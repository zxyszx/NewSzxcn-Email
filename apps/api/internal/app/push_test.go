package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

type testPushSender struct {
	calls   int
	invalid bool
	failure error
}

func (s *testPushSender) Send(_ context.Context, token, event, binding string) (bool, error) {
	s.calls++
	if !pushTokenPattern.MatchString(token) || len(event) != 32 || len(binding) != 64 {
		return false, errors.New("invalid payload")
	}
	return s.invalid, s.failure
}

func pushTestSetup(t *testing.T) (*App, *testClient, *testPushSender, *Mailbox, map[string]string) {
	t.Helper()
	a := newTestApp(t)
	stopTestWorkers(a)
	a.updateConfig(func(cfg *Config) { cfg.ExternalIMAPSecretKey = "isolated-test-encryption-secret" })
	sender := &testPushSender{}
	a.pushSender = sender
	server := httptest.NewServer(a.Router())
	t.Cleanup(server.Close)
	client := &testClient{t: t, server: server}
	if status := client.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, nil); status != 200 {
		t.Fatalf("login status %d", status)
	}
	_, mailbox := defaultAdminUserAndMailbox(t, a)
	device := map[string]string{"id": strings.Repeat("i", 43), "secret": strings.Repeat("s", 43), "token": strings.Repeat("t", 100)}
	return a, client, sender, mailbox, device
}

func pushInsertTestMessage(t *testing.T, a *App, mailbox *Mailbox, eligible bool) string {
	t.Helper()
	folder, err := a.ensureFolder(context.Background(), mailbox.ID, "Inbox")
	if err != nil {
		t.Fatal(err)
	}
	now := a.now().UTC()
	id, err := a.insertMessage(context.Background(), storedMessage{MailboxID: mailbox.ID, FolderID: folder, NotifyPush: eligible, MessageUID: newID("uid"), MessageID: newID("mime"), From: "sender@example.test", Subject: "must-not-enter-push", BodyText: "private body", SentAt: now, ReceivedAt: now}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func pushCount(t *testing.T, a *App, table string) int {
	t.Helper()
	var count int
	if table != "push_devices" && table != "push_outbox" {
		t.Fatal("invalid test table")
	}
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestPushRegistrationAndLogout(t *testing.T) {
	a, client, _, _, device := pushTestSetup(t)
	anonymous := &testClient{t: t, server: client.server}
	if got := anonymous.do("POST", "/api/me/push-devices", device, nil); got != 401 {
		t.Fatalf("anonymous=%d", got)
	}
	if got := client.doWithHeaders("POST", "/api/me/push-devices", device, map[string]string{"Origin": "https://evil.example"}, nil); got != 403 {
		t.Fatalf("origin=%d", got)
	}
	if got := client.do("POST", "/api/me/push-devices", device, nil); got != 200 {
		t.Fatalf("register=%d", got)
	}
	var cipher string
	if err := a.db.QueryRow(`SELECT token_cipher FROM push_devices`).Scan(&cipher); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cipher, device["token"]) {
		t.Fatal("token stored in plaintext")
	}
	plain, err := a.decryptBackupPassword(cipher)
	if err != nil || plain != device["token"] {
		t.Fatal("token encryption failed")
	}
	device["secret"] = strings.Repeat("x", 43)
	if got := client.do("POST", "/api/me/push-devices", device, nil); got != 409 {
		t.Fatalf("proof bypass=%d", got)
	}
	if got := client.do("POST", "/api/auth/logout", nil, nil); got != 200 {
		t.Fatal(got)
	}
	if pushCount(t, a, "push_devices") != 0 {
		t.Fatal("logout retained device")
	}
}

func TestPushAtomicEnqueueAndDedup(t *testing.T) {
	a, client, sender, mailbox, device := pushTestSetup(t)
	if got := client.do("POST", "/api/me/push-devices", device, nil); got != 200 {
		t.Fatal(got)
	}
	pushInsertTestMessage(t, a, mailbox, false)
	if pushCount(t, a, "push_outbox") != 0 {
		t.Fatal("import triggered notification")
	}
	id := pushInsertTestMessage(t, a, mailbox, true)
	if pushCount(t, a, "push_outbox") != 1 {
		t.Fatal("incoming message did not enqueue")
	}
	var queuedID string
	_ = a.db.QueryRow(`SELECT id FROM push_outbox`).Scan(&queuedID)
	if _, err := a.db.Exec(`INSERT OR IGNORE INTO push_outbox(id,device_id,message_id,next_attempt,created_at) VALUES(?,?,?,?,?)`, newID("p"), device["id"], id, 0, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if pushCount(t, a, "push_outbox") != 1 {
		t.Fatal("duplicate job")
	}
	_, _ = a.db.Exec(`UPDATE push_outbox SET next_attempt=0`)
	a.deliverPushBatch(context.Background())
	a.deliverPushBatch(context.Background())
	if sender.calls != 1 {
		t.Fatalf("deliveries=%d", sender.calls)
	}
	var finished int
	_ = a.db.QueryRow(`SELECT finished FROM push_outbox WHERE id=?`, queuedID).Scan(&finished)
	if finished != 1 {
		t.Fatal("not marked delivered")
	}
}

func TestPushAttachmentFailureRollsBackMessage(t *testing.T) {
	a, client, _, mailbox, device := pushTestSetup(t)
	if got := client.do("POST", "/api/me/push-devices", device, nil); got != 200 {
		t.Fatal(got)
	}
	folder, err := a.ensureFolder(context.Background(), mailbox.ID, "Inbox")
	if err != nil {
		t.Fatal(err)
	}
	uid := newID("uid")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = a.insertMessage(ctx, storedMessage{MailboxID: mailbox.ID, FolderID: folder, NotifyPush: true, MessageUID: uid, SentAt: time.Now(), ReceivedAt: time.Now()}, []AttachmentInput{
		{Filename: "valid.txt", ContentBase64: base64.StdEncoding.EncodeToString([]byte("test"))},
		{Filename: "invalid.txt", ContentBase64: "!invalid-base64"},
	})
	if err == nil || ctx.Err() != nil {
		t.Fatal("attachment failure must return promptly")
	}
	var count int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE message_uid=?`, uid).Scan(&count); err != nil || count != 0 || pushCount(t, a, "push_outbox") != 0 {
		t.Fatal("partial message or push survived rollback")
	}
	entries, err := os.ReadDir(filepath.Join(a.config().DataDir, "attachments"))
	if err != nil || len(entries) != 0 {
		t.Fatal("partial attachment files survived rollback")
	}
}

func TestPushRetryRevocationAndInvalidToken(t *testing.T) {
	for _, mode := range []string{"retry", "expired", "read", "moved", "disabled", "invalid", "rollback", "rebind"} {
		t.Run(mode, func(t *testing.T) {
			a, client, sender, mailbox, device := pushTestSetup(t)
			if got := client.do("POST", "/api/me/push-devices", device, nil); got != 200 {
				t.Fatal(got)
			}
			id := pushInsertTestMessage(t, a, mailbox, true)
			_, _ = a.db.Exec(`UPDATE push_outbox SET next_attempt=0`)
			switch mode {
			case "retry":
				sender.failure = errors.New("private provider response must not be persisted")
			case "expired":
				_, _ = a.db.Exec(`UPDATE sessions SET expires_at='2000-01-01T00:00:00Z'`)
			case "read":
				_, _ = a.db.Exec(`UPDATE messages SET is_read=1 WHERE id=?`, id)
			case "moved":
				folder, err := a.ensureFolder(context.Background(), mailbox.ID, "Trash")
				if err != nil {
					t.Fatal(err)
				}
				_, _ = a.db.Exec(`UPDATE messages SET folder_id=? WHERE id=?`, folder, id)
			case "disabled":
				_, _ = a.db.Exec(`UPDATE users SET disabled=1 WHERE id=?`, mailbox.UserID)
			case "invalid":
				sender.invalid = true
			case "rollback":
				folder, err := a.ensureFolder(context.Background(), mailbox.ID, "Inbox")
				if err != nil {
					t.Fatal(err)
				}
				tx, err := a.db.Begin()
				if err != nil {
					t.Fatal(err)
				}
				_, err = a.insertMessageWithDB(context.Background(), tx, storedMessage{MailboxID: mailbox.ID, FolderID: folder, NotifyPush: true, MessageUID: newID("uid"), SentAt: time.Now(), ReceivedAt: time.Now()}, nil)
				if err != nil {
					tx.Rollback()
					t.Fatal(err)
				}
				_ = tx.Rollback()
				if pushCount(t, a, "push_outbox") != 1 {
					t.Fatal("rolled-back message retained notification")
				}
			case "rebind":
				device["token"] = strings.Repeat("u", 100)
				if got := client.do("POST", "/api/me/push-devices", device, nil); got != 200 {
					t.Fatal(got)
				}
			}
			a.deliverPushBatch(context.Background())
			if mode == "retry" {
				var last string
				var next int64
				_ = a.db.QueryRow(`SELECT last_error,next_attempt FROM push_outbox`).Scan(&last, &next)
				if last != "delivery unavailable" || next <= time.Now().Unix() {
					t.Fatal("unsafe retry state")
				}
				sender.failure = nil
				_, _ = a.db.Exec(`UPDATE push_outbox SET next_attempt=0`)
				a.deliverPushBatch(context.Background())
				if sender.calls != 2 {
					t.Fatal("retry did not deliver")
				}
			} else if mode == "invalid" {
				if pushCount(t, a, "push_devices") != 0 || pushCount(t, a, "push_outbox") != 0 {
					t.Fatal("unregistered device retained")
				}
			} else if mode != "rollback" && sender.calls != 0 {
				t.Fatal("ineligible notification delivered")
			}
		})
	}
}

type pushRoundTripper func(*http.Request) (*http.Response, error)

func (f pushRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFCMDataOnlyPayload(t *testing.T) {
	sender := &fcmSender{project: "test-project", tokens: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "test-access"}), client: &http.Client{Transport: pushRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://fcm.googleapis.com/v1/projects/test-project/messages:send" {
			t.Fatal("unexpected endpoint")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]map[string]any
		if json.Unmarshal(body, &payload) != nil {
			t.Fatal("bad JSON")
		}
		message := payload["message"]
		if len(message) != 3 || message["notification"] != nil {
			t.Fatal("notification content leaked")
		}
		data := message["data"].(map[string]any)
		if len(data) != 3 || data["type"] != "new_mail" || data["binding_id"] != strings.Repeat("b", 64) {
			t.Fatal("unexpected message metadata")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
	})}}
	if invalid, err := sender.Send(context.Background(), strings.Repeat("t", 100), strings.Repeat("a", 32), strings.Repeat("b", 64)); invalid || err != nil {
		t.Fatal("send failed")
	}
}

func TestPushRetryAfterAndExhaustedLease(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, test := range []struct {
		value string
		want  time.Duration
	}{
		{"120", 120 * time.Second},
		{"-1", 0},
		{"999999999999999", 24 * time.Hour},
		{now.Add(5 * time.Minute).Format(http.TimeFormat), 5 * time.Minute},
		{"invalid", 0},
	} {
		if got := pushRetryAfter(test.value, now); got != test.want {
			t.Fatalf("Retry-After %q: %s, want %s", test.value, got, test.want)
		}
	}
	a, client, sender, mailbox, device := pushTestSetup(t)
	if got := client.do("POST", "/api/me/push-devices", device, nil); got != 200 {
		t.Fatal(got)
	}
	pushInsertTestMessage(t, a, mailbox, true)
	sender.failure = &pushRetryError{status: 429, after: 300 * time.Second}
	_, _ = a.db.Exec(`UPDATE push_outbox SET next_attempt=0`)
	a.deliverPushBatch(context.Background())
	var next int64
	if err := a.db.QueryRow(`SELECT next_attempt FROM push_outbox`).Scan(&next); err != nil || next < a.now().Unix()+299 {
		t.Fatal("provider retry delay not honored")
	}
	_, _ = a.db.Exec(`UPDATE push_outbox SET attempts=8,lease_until=0`)
	a.deliverPushBatch(context.Background())
	var finished int
	if err := a.db.QueryRow(`SELECT finished FROM push_outbox`).Scan(&finished); err != nil || finished != 1 || sender.calls != 1 {
		t.Fatal("exhausted lease was not finalized")
	}
}
