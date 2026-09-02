package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeDocker struct {
	mu        sync.Mutex
	current   string
	pulls     []string
	latest    string
	rollback  string
	images    map[string]bool
	removed   []string
	commands  []string
	updaterID string
}

func newFakeDocker(initial string, pulls ...string) *fakeDocker {
	images := map[string]bool{initial: true, "sha256:updater": true}
	return &fakeDocker{current: initial, latest: initial, pulls: pulls, images: images, updaterID: "sha256:updater"}
}

func (f *fakeDocker) Run(_ context.Context, env map[string]string, name string, args ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	command := name + " " + strings.Join(args, " ")
	f.commands = append(f.commands, command)
	if name != "docker" {
		return "", fmt.Errorf("unexpected command %s", command)
	}
	joined := strings.Join(args, " ")
	switch {
	case strings.Contains(joined, "system df"):
		return "TYPE TOTAL ACTIVE SIZE RECLAIMABLE\nImages 3 2 2GB 1GB", nil
	case strings.Contains(joined, "compose") && strings.HasSuffix(joined, "ps -q lanqin-email"):
		return "mail-container\n", nil
	case joined == "inspect --format {{.Image}} mail-container":
		return f.current + "\n", nil
	case joined == "inspect --format {{.Image}} updater-container":
		return f.updaterID + "\n", nil
	case joined == "ps -aq":
		return "mail-container\nupdater-container\n", nil
	case strings.HasPrefix(joined, "image tag "):
		fields := strings.Fields(joined)
		f.rollback = fields[2]
		return "", nil
	case strings.HasPrefix(joined, "image pull "):
		if len(f.pulls) > 0 {
			f.latest = f.pulls[0]
			f.pulls = f.pulls[1:]
			f.images[f.latest] = true
		}
		return "pulled", nil
	case strings.HasPrefix(joined, "image inspect --format {{.Id}} "):
		ref := strings.TrimPrefix(joined, "image inspect --format {{.Id}} ")
		if strings.HasSuffix(ref, ":rollback-previous") {
			return f.rollback + "\n", nil
		}
		return f.latest + "\n", nil
	case strings.Contains(joined, "compose") && strings.Contains(joined, " up -d --no-deps --force-recreate lanqin-email"):
		image := env["LANQIN_IMAGE"]
		if strings.HasSuffix(image, ":rollback-previous") {
			f.current = f.rollback
		} else {
			f.current = f.latest
		}
		return "started", nil
	case joined == "image ls -q --no-trunc --filter label="+projectImageLabels[0]:
		var values []string
		for image := range f.images {
			if image != f.updaterID {
				values = append(values, image)
			}
		}
		return strings.Join(values, "\n"), nil
	case joined == "image ls -q --no-trunc --filter label="+projectImageLabels[1]:
		return f.updaterID + "\n", nil
	case strings.HasPrefix(joined, "image inspect --format {{json .RepoTags}} "):
		image := strings.TrimPrefix(joined, "image inspect --format {{json .RepoTags}} ")
		if image == f.latest || image == f.rollback {
			return "[\"project:tag\"]", nil
		}
		return "null", nil
	case strings.HasPrefix(joined, "image inspect --format {{.Size}} "):
		return "1000", nil
	case strings.HasPrefix(joined, "image rm "):
		image := strings.TrimPrefix(joined, "image rm ")
		if image == f.current || image == f.rollback || image == f.updaterID {
			return "", fmt.Errorf("attempted to remove protected image %s", image)
		}
		delete(f.images, image)
		f.removed = append(f.removed, image)
		return "Deleted: " + image, nil
	default:
		return "", fmt.Errorf("unexpected docker command: %s", joined)
	}
}

func (f *fakeDocker) currentImage() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.current
}

func testUpdater(t *testing.T, fake *fakeDocker, healthURL string) *updater {
	t.Helper()
	return &updater{
		cfg: config{
			InstallDir:  t.TempDir(),
			TargetImage: "ghcr.io/zxyszx/newszxcn-email:latest",
			HealthURL:   healthURL,
			Service:     "lanqin-email",
			HealthWait:  1500 * time.Millisecond,
		},
		runner: fake,
		log:    log.New(os.Stderr, "", 0),
	}
}

func TestThreeUpdatesKeepOnlyCurrentAndPreviousProjectImages(t *testing.T) {
	fake := newFakeDocker("sha256:v1", "sha256:v2", "sha256:v3", "sha256:v4")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	u := testUpdater(t, fake, server.URL)

	for i := 0; i < 3; i++ {
		result, err := u.update(context.Background())
		if err != nil {
			t.Fatalf("update %d failed: %v", i+1, err)
		}
		if !result.Updated || result.RolledBack {
			t.Fatalf("update %d result=%+v", i+1, result)
		}
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.current != "sha256:v4" || fake.rollback != "sha256:v3" {
		t.Fatalf("current=%s rollback=%s", fake.current, fake.rollback)
	}
	for _, stale := range []string{"sha256:v1", "sha256:v2"} {
		if fake.images[stale] {
			t.Fatalf("stale image %s was retained", stale)
		}
	}
	if !fake.images["sha256:v3"] || !fake.images["sha256:v4"] || !fake.images[fake.updaterID] {
		t.Fatalf("required images were removed: %#v", fake.images)
	}
	for _, command := range fake.commands {
		if strings.Contains(command, "system prune") || strings.Contains(command, "volume rm") || strings.Contains(command, "container rm") {
			t.Fatalf("destructive global command executed: %s", command)
		}
	}
}

func TestFailedHealthCheckRestoresPreviousWithoutCleanup(t *testing.T) {
	fake := newFakeDocker("sha256:good", "sha256:bad")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fake.currentImage() == "sha256:good" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	u := testUpdater(t, fake, server.URL)

	result, err := u.update(context.Background())
	if err == nil {
		t.Fatal("expected failed target health check")
	}
	if !result.RolledBack || fake.currentImage() != "sha256:good" {
		t.Fatalf("rollback result=%+v current=%s", result, fake.currentImage())
	}
	if len(result.DeletedImages) != 0 || len(fake.removed) != 0 {
		t.Fatalf("images were cleaned before successful health check: %+v", result.DeletedImages)
	}
}

func TestRollbackRefPreservesRegistryPort(t *testing.T) {
	if got := rollbackRef("registry.example.test:5000/team/mail:v1"); got != "registry.example.test:5000/team/mail:rollback-previous" {
		t.Fatalf("rollbackRef=%q", got)
	}
}

func TestMergedEnvOverridesExistingValues(t *testing.T) {
	got := mergedEnv([]string{"PATH=/bin", "LANQIN_IMAGE=old", "EMPTY=old"}, map[string]string{
		"LANQIN_IMAGE": "rollback",
		"EMPTY":        "",
	})
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "LANQIN_IMAGE=old") || !strings.Contains(joined, "LANQIN_IMAGE=rollback") {
		t.Fatalf("environment was not replaced: %v", got)
	}
	if strings.Contains(joined, "EMPTY=old") || !strings.Contains(joined, "EMPTY=") {
		t.Fatalf("empty override was not preserved: %v", got)
	}
}

func TestSecondUpdaterCannotAcquireLock(t *testing.T) {
	path := t.TempDir() + "/.update.lock"
	first, err := acquireFileLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseFileLock(first)
	second, err := acquireFileLock(path)
	if !errors.Is(err, errUpdateLocked) || second != nil {
		t.Fatalf("second lock=%v err=%v", second, err)
	}
}

func TestHTTPUpdateIsAcceptedBeforeContainerReplacement(t *testing.T) {
	fake := newFakeDocker("sha256:v1", "sha256:v2")
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer health.Close()
	u := testUpdater(t, fake, health.URL)
	u.cfg.Token = "test-secret"

	req := httptest.NewRequest(http.MethodPost, "/v1/update", nil)
	req.Header.Set("Authorization", "Bearer test-secret")
	recorder := httptest.NewRecorder()
	u.handleUpdate(recorder, req)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	deadline := time.Now().Add(2 * time.Second)
	for fake.currentImage() != "sha256:v2" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if fake.currentImage() != "sha256:v2" {
		t.Fatal("accepted update did not complete in the background")
	}
}
