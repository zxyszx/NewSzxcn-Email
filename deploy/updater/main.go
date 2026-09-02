package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var projectImageLabels = []string{
	"org.opencontainers.image.title=NewSzxcn Email all-in-one",
	"org.opencontainers.image.title=NewSzxcn Email updater",
}

type commandRunner interface {
	Run(context.Context, map[string]string, string, ...string) (string, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, extraEnv map[string]string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = mergedEnv(os.Environ(), extraEnv)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s failed: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func mergedEnv(base []string, extra map[string]string) []string {
	if len(extra) == 0 {
		return base
	}
	result := make([]string, 0, len(base)+len(extra))
	for _, entry := range base {
		key, _, found := strings.Cut(entry, "=")
		if !found || extra[key] == "" {
			if _, replaced := extra[key]; !replaced {
				result = append(result, entry)
			}
		}
	}
	for key, value := range extra {
		result = append(result, key+"="+value)
	}
	return result
}

type config struct {
	Token       string
	InstallDir  string
	TargetImage string
	HealthURL   string
	ListenAddr  string
	Service     string
	HealthWait  time.Duration
}

type updater struct {
	cfg    config
	runner commandRunner
	log    *log.Logger
}

type updateResult struct {
	Updated          bool     `json:"updated"`
	RolledBack       bool     `json:"rolledBack"`
	PreviousImage    string   `json:"previousImage,omitempty"`
	CurrentImage     string   `json:"currentImage,omitempty"`
	DeletedImages    []string `json:"deletedImages,omitempty"`
	EstimatedFreed   int64    `json:"estimatedFreedBytes"`
	DiskBefore       string   `json:"diskBefore,omitempty"`
	DiskAfter        string   `json:"diskAfter,omitempty"`
	RollbackImageRef string   `json:"rollbackImageRef,omitempty"`
}

func main() {
	cfg := loadConfig()
	logger := log.New(os.Stdout, "newszxcn-updater ", log.LstdFlags|log.LUTC)
	u := &updater{cfg: cfg, runner: execRunner{}, log: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("/v1/update", u.handleUpdate)
	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      12 * time.Minute,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	logger.Printf("listening on %s", cfg.ListenAddr)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		logger.Fatal(err)
	}
}

func loadConfig() config {
	waitSeconds := envInt("LANQIN_UPDATE_HEALTH_SECONDS", 90)
	return config{
		Token:       strings.TrimSpace(os.Getenv("WATCHTOWER_HTTP_API_TOKEN")),
		InstallDir:  envString("LANQIN_INSTALL_DIR", "/opt/newszxcn-email"),
		TargetImage: envString("LANQIN_TARGET_IMAGE", "ghcr.io/zxyszx/newszxcn-email:latest"),
		HealthURL:   envString("LANQIN_UPDATE_HEALTH_URL", "http://lanqin-email/healthz"),
		ListenAddr:  envString("LANQIN_UPDATE_LISTEN_ADDR", ":8080"),
		Service:     envString("LANQIN_UPDATE_SERVICE", "lanqin-email"),
		HealthWait:  time.Duration(waitSeconds) * time.Second,
	}
}

func envString(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func (u *updater) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	providedToken := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if u.cfg.Token == "" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || len(providedToken) != len(u.cfg.Token) || subtle.ConstantTimeCompare([]byte(providedToken), []byte(u.cfg.Token)) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	err := u.startUpdate()
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errUpdateLocked) {
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"accepted": true})
}

var errUpdateLocked = errors.New("another update or rollback is already running")

func (u *updater) startUpdate() error {
	lock, err := acquireFileLock(filepath.Join(u.cfg.InstallDir, ".update.lock"))
	if err != nil {
		return err
	}
	go func() {
		defer releaseFileLock(lock)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		result, updateErr := u.updateLocked(ctx)
		if updateErr != nil {
			u.log.Printf("update failed: %v (rolled_back=%t current=%s)", updateErr, result.RolledBack, shortID(result.CurrentImage))
			return
		}
		u.log.Printf("update completed (updated=%t current=%s deleted=%v)", result.Updated, shortID(result.CurrentImage), result.DeletedImages)
	}()
	return nil
}

func (u *updater) update(ctx context.Context) (updateResult, error) {
	lock, err := acquireFileLock(filepath.Join(u.cfg.InstallDir, ".update.lock"))
	if err != nil {
		return updateResult{}, err
	}
	defer releaseFileLock(lock)
	return u.updateLocked(ctx)
}

func (u *updater) updateLocked(ctx context.Context) (updateResult, error) {
	result := updateResult{RollbackImageRef: rollbackRef(u.cfg.TargetImage)}
	result.DiskBefore = u.systemDF(ctx)
	u.log.Printf("disk usage before cleanup:\n%s", result.DiskBefore)

	previous, err := u.serviceImageID(ctx)
	if err != nil {
		return result, err
	}
	result.PreviousImage = previous
	if _, err := u.runner.Run(ctx, nil, "docker", "image", "tag", previous, result.RollbackImageRef); err != nil {
		return result, fmt.Errorf("preserve rollback image: %w", err)
	}
	u.log.Printf("preserved current image %s as %s", shortID(previous), result.RollbackImageRef)

	if _, err := u.runner.Run(ctx, nil, "docker", "image", "pull", u.cfg.TargetImage); err != nil {
		return result, fmt.Errorf("pull target image: %w", err)
	}
	targetID, err := u.imageID(ctx, u.cfg.TargetImage)
	if err != nil {
		return result, err
	}
	if targetID == previous {
		result.CurrentImage = previous
		result.DiskAfter = u.systemDF(ctx)
		u.log.Printf("target image is already running")
		return result, nil
	}

	if err := u.composeUp(ctx, u.cfg.TargetImage); err != nil {
		return u.rollback(ctx, result, fmt.Errorf("start target image: %w", err))
	}
	if err := u.waitHealthy(ctx); err != nil {
		return u.rollback(ctx, result, fmt.Errorf("target health check: %w", err))
	}
	current, err := u.serviceImageID(ctx)
	if err != nil {
		return u.rollback(ctx, result, fmt.Errorf("inspect healthy target: %w", err))
	}
	if current != targetID {
		return u.rollback(ctx, result, fmt.Errorf("running image %s does not match pulled target %s", shortID(current), shortID(targetID)))
	}
	result.Updated = true
	result.CurrentImage = current
	deleted, estimated := u.cleanupDanglingProjectImages(ctx, current, previous)
	result.DeletedImages = deleted
	result.EstimatedFreed = estimated
	result.DiskAfter = u.systemDF(ctx)
	u.log.Printf("deleted project images: %v", deleted)
	u.log.Printf("estimated logical image space released: %d bytes", estimated)
	u.log.Printf("disk usage after cleanup:\n%s", result.DiskAfter)
	return result, nil
}

func (u *updater) rollback(ctx context.Context, result updateResult, cause error) (updateResult, error) {
	u.log.Printf("new image failed; restoring %s", result.RollbackImageRef)
	result.RolledBack = true
	if err := u.composeUp(ctx, result.RollbackImageRef); err != nil {
		return result, fmt.Errorf("%v; rollback start failed: %w", cause, err)
	}
	if err := u.waitHealthy(ctx); err != nil {
		return result, fmt.Errorf("%v; rollback health check failed: %w", cause, err)
	}
	current, err := u.serviceImageID(ctx)
	if err != nil {
		return result, fmt.Errorf("%v; inspect rollback image: %w", cause, err)
	}
	result.CurrentImage = current
	result.DiskAfter = u.systemDF(ctx)
	return result, cause
}

func (u *updater) composeArgs(args ...string) []string {
	base := []string{"compose", "--project-directory", u.cfg.InstallDir, "--env-file", filepath.Join(u.cfg.InstallDir, ".env"), "-f", filepath.Join(u.cfg.InstallDir, "docker-compose.yml")}
	return append(base, args...)
}

func (u *updater) composeUp(ctx context.Context, image string) error {
	env := map[string]string{"LANQIN_IMAGE": image, "LANQIN_INSTALL_DIR": u.cfg.InstallDir}
	_, err := u.runner.Run(ctx, env, "docker", u.composeArgs("up", "-d", "--no-deps", "--force-recreate", u.cfg.Service)...)
	return err
}

func (u *updater) serviceImageID(ctx context.Context) (string, error) {
	out, err := u.runner.Run(ctx, nil, "docker", u.composeArgs("ps", "-q", u.cfg.Service)...)
	if err != nil {
		return "", fmt.Errorf("find running service container: %w", err)
	}
	if strings.TrimSpace(out) == "" {
		return "", errors.New("find running service container: service is not running")
	}
	containerID := strings.Fields(out)[0]
	out, err = u.runner.Run(ctx, nil, "docker", "inspect", "--format", "{{.Image}}", containerID)
	if err != nil {
		return "", fmt.Errorf("inspect service image: %w", err)
	}
	return strings.TrimSpace(out), nil
}

func (u *updater) imageID(ctx context.Context, ref string) (string, error) {
	out, err := u.runner.Run(ctx, nil, "docker", "image", "inspect", "--format", "{{.Id}}", ref)
	if err != nil {
		return "", fmt.Errorf("inspect image %s: %w", ref, err)
	}
	imageID := strings.TrimSpace(out)
	if imageID == "" {
		return "", fmt.Errorf("inspect image %s: empty image ID", ref)
	}
	return imageID, nil
}

func (u *updater) waitHealthy(ctx context.Context) error {
	deadline := time.Now().Add(u.cfg.HealthWait)
	client := &http.Client{Timeout: 3 * time.Second}
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.cfg.HealthURL, nil)
		if err == nil {
			resp, requestErr := client.Do(req)
			if requestErr == nil {
				_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
				_ = resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					return nil
				}
			}
		}
		if time.Now().After(deadline) {
			return errors.New("service did not become healthy before timeout")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func (u *updater) cleanupDanglingProjectImages(ctx context.Context, current, rollback string) ([]string, int64) {
	protected := map[string]bool{current: true, rollback: true}
	if out, err := u.runner.Run(ctx, nil, "docker", "ps", "-aq"); err == nil {
		for _, containerID := range strings.Fields(out) {
			if image, inspectErr := u.runner.Run(ctx, nil, "docker", "inspect", "--format", "{{.Image}}", containerID); inspectErr == nil {
				protected[strings.TrimSpace(image)] = true
			}
		}
	}
	candidates := make([]string, 0)
	for _, label := range projectImageLabels {
		out, err := u.runner.Run(ctx, nil, "docker", "image", "ls", "-q", "--no-trunc", "--filter", "label="+label)
		if err != nil {
			u.log.Printf("list cleanup candidates for %q: %v", label, err)
			continue
		}
		candidates = append(candidates, strings.Fields(out)...)
	}
	seen := map[string]bool{}
	var deleted []string
	var estimated int64
	for _, imageID := range candidates {
		if seen[imageID] || protected[imageID] {
			continue
		}
		seen[imageID] = true
		tags, inspectErr := u.runner.Run(ctx, nil, "docker", "image", "inspect", "--format", "{{json .RepoTags}}", imageID)
		if inspectErr != nil || !isDanglingTags(tags) {
			continue
		}
		size := int64(0)
		if rawSize, sizeErr := u.runner.Run(ctx, nil, "docker", "image", "inspect", "--format", "{{.Size}}", imageID); sizeErr == nil {
			size, _ = strconv.ParseInt(strings.TrimSpace(rawSize), 10, 64)
		}
		if _, removeErr := u.runner.Run(ctx, nil, "docker", "image", "rm", imageID); removeErr != nil {
			u.log.Printf("skip image %s: %v", shortID(imageID), removeErr)
			continue
		}
		deleted = append(deleted, imageID)
		estimated += size
	}
	return deleted, estimated
}

func isDanglingTags(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || value == "null" || value == "[]" || value == "[\"<none>:<none>\"]"
}

func rollbackRef(image string) string {
	image = strings.TrimSpace(strings.SplitN(image, "@", 2)[0])
	lastSlash := strings.LastIndex(image, "/")
	if colon := strings.LastIndex(image, ":"); colon > lastSlash {
		image = image[:colon]
	}
	return image + ":rollback-previous"
}

func acquireFileLock(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, errUpdateLocked
	}
	return file, nil
}

func releaseFileLock(file *os.File) {
	if file == nil {
		return
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	_ = file.Close()
}

func (u *updater) systemDF(ctx context.Context) string {
	out, err := u.runner.Run(ctx, nil, "docker", "system", "df")
	if err != nil {
		return "unavailable: " + err.Error()
	}
	return strings.TrimSpace(out)
}

func shortID(value string) string {
	value = strings.TrimPrefix(strings.TrimSpace(value), "sha256:")
	if len(value) > 12 {
		return value[:12]
	}
	return value
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
