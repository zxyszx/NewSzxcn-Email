package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	netmail "net/mail"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBuildMIMEWithoutAttachmentsUsesAlternative(t *testing.T) {
	raw, err := BuildMIME(MIMEMessage{
		From: "noreply@example.test", FromName: systemSenderDisplayName,
		To: []string{"target@example.net"}, Subject: "测试", Text: "纯文本", HTML: "<p>纯文本</p>",
		MessageID: "<test@example.test>", Date: time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	msg, parts := parseMIMEParts(t, raw)
	mediaType, _, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/alternative" {
		t.Fatalf("content type=%q err=%v", msg.Header.Get("Content-Type"), err)
	}
	if strings.Contains(string(raw), "multipart/mixed") {
		t.Fatalf("message without attachments contains multipart/mixed: %s", raw)
	}
	if string(parts["text/plain"]) != "纯文本" || string(parts["text/html"]) != "<p>纯文本</p>" {
		t.Fatalf("parts=%q", parts)
	}
}

func TestBuildMIMEWithAttachmentKeepsMixedWrapper(t *testing.T) {
	raw, err := BuildMIME(MIMEMessage{
		From: "sender@example.test", To: []string{"target@example.net"}, Subject: "附件",
		Text: "正文", HTML: "<p>正文</p>", MessageID: "<attachment@example.test>", Date: time.Now().UTC(),
		Attachments: []AttachmentInput{{Filename: "test.txt", ContentType: "text/plain", ContentBase64: base64.StdEncoding.EncodeToString([]byte("file"))}},
	})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := netmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	mediaType, _, _ := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if mediaType != "multipart/mixed" {
		t.Fatalf("content type=%q", msg.Header.Get("Content-Type"))
	}
}

func TestForwardingVerificationMessageMetadataAndContent(t *testing.T) {
	a := newTestApp(t)
	stopTestWorkers(a)
	a.updateConfig(func(cfg *Config) { cfg.SMTPHost = "postfix" })
	user, _ := defaultAdminUserAndMailbox(t, a)
	now := time.Date(2026, 8, 23, 14, 0, 0, 0, time.UTC)
	queueID, err := a.sendForwardingVerificationEmail(context.Background(), user.ID, "kellisonwreede@example.net", "secret-token", now)
	if err != nil {
		t.Fatal(err)
	}
	raw := queuedMIMEForTest(t, a, queueID)
	msg, parts := parseMIMEParts(t, raw)
	from, err := netmail.ParseAddress(msg.Header.Get("From"))
	if err != nil {
		t.Fatal(err)
	}
	if from.Name != systemSenderDisplayName || from.Address != "noreply@lanqin.local" {
		t.Fatalf("from=%+v", from)
	}
	subject, err := (&mime.WordDecoder{}).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil || subject != "请确认您的邮箱转发设置" {
		t.Fatalf("subject=%q err=%v", subject, err)
	}
	for contentType, body := range parts {
		content := string(body)
		for _, expected := range []string{"NewSzxcn Email Service", "请确认您的邮箱转发设置", "k***e@example.net", "24 小时", "您的邮箱设置不会被更改", "请勿直接回复", "/mail/forwarding/verification/confirm?token=secret-token"} {
			if !strings.Contains(content, expected) {
				t.Fatalf("%s missing %q: %s", contentType, expected, content)
			}
		}
		if strings.Contains(content, "kellisonwreede@example.net") {
			t.Fatalf("%s exposes full target address: %s", contentType, content)
		}
	}
	htmlBody := string(parts["text/html"])
	for _, expected := range []string{`role="presentation"`, `max-width:600px`, `background:#2563eb`, "安全验证"} {
		if !strings.Contains(htmlBody, expected) {
			t.Fatalf("html template missing %q: %s", expected, htmlBody)
		}
	}
	if strings.Contains(htmlBody, "<img") || strings.Contains(htmlBody, "linear-gradient") {
		t.Fatalf("verification email should not depend on external images or gradients: %s", htmlBody)
	}
}

func TestMaskEmailAddress(t *testing.T) {
	tests := map[string]string{
		"kellisonwreede@gmail.com": "k***e@gmail.com",
		"abc@example.test":         "a***c@example.test",
		"ab@example.test":          "a*@example.test",
		"a@example.test":           "*@example.test",
		"missing-at":               "***",
		"@example.test":            "***",
		"a@":                       "***",
	}
	for input, expected := range tests {
		if actual := maskEmailAddress(input); actual != expected {
			t.Errorf("maskEmailAddress(%q)=%q want %q", input, actual, expected)
		}
	}
}

func TestForwardingVerificationHTMLContentIsEscaped(t *testing.T) {
	a := newTestApp(t)
	stopTestWorkers(a)
	a.updateConfig(func(cfg *Config) { cfg.SMTPHost = "postfix" })
	user, _ := defaultAdminUserAndMailbox(t, a)
	queueID, err := a.sendForwardingVerificationEmail(context.Background(), user.ID, "abc@example.test<script>", "token", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	_, parts := parseMIMEParts(t, queuedMIMEForTest(t, a, queueID))
	htmlBody := string(parts["text/html"])
	if strings.Contains(htmlBody, "<script>") || !strings.Contains(htmlBody, "example.test&lt;script&gt;") {
		t.Fatalf("dynamic content was not escaped: %s", htmlBody)
	}
}

func TestForwardingVerificationRateLimitAndTokenRotation(t *testing.T) {
	a := newTestApp(t)
	stopTestWorkers(a)
	a.updateConfig(func(cfg *Config) { cfg.SMTPHost = "postfix" })
	user, _ := defaultAdminUserAndMailbox(t, a)
	ctx := context.Background()
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	id := newID("fwd")
	email := "rotate@example.net"
	if err := a.issueForwardingVerification(ctx, user.ID, id, email, true); err != nil {
		t.Fatal(err)
	}
	firstToken := latestForwardingTokenForTest(t, a, user.ID)
	var firstHash, expiresRaw string
	if err := a.db.QueryRow("SELECT verification_token_hash,verification_expires_at FROM forwarding_verified_emails WHERE id=?", id).Scan(&firstHash, &expiresRaw); err != nil {
		t.Fatal(err)
	}
	if firstHash != hashToken(firstToken) || !parseTime(expiresRaw).Equal(now.Add(24*time.Hour)) {
		t.Fatalf("hash=%q expires=%q", firstHash, expiresRaw)
	}

	if err := a.issueForwardingVerification(ctx, user.ID, id, email, false); err == nil {
		t.Fatal("expected 60 second rate limit")
	} else {
		var rateErr *forwardingVerificationRateLimitError
		if !errors.As(err, &rateErr) || !strings.Contains(err.Error(), "1分钟") {
			t.Fatalf("rate error=%v", err)
		}
	}

	now = now.Add(61 * time.Second)
	if err := a.issueForwardingVerification(ctx, user.ID, id, email, false); err != nil {
		t.Fatal(err)
	}
	secondToken := latestForwardingTokenForTest(t, a, user.ID)
	if secondToken == firstToken {
		t.Fatal("resend reused the verification token")
	}
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	if status := getStatusForTest(t, ts.URL+"/api/verify-email?token="+firstToken); status != http.StatusBadRequest {
		t.Fatalf("old token status=%d", status)
	}
	if status := getStatusForTest(t, ts.URL+"/api/verify-email?token="+secondToken); status != http.StatusOK {
		t.Fatalf("new token status=%d", status)
	}
	if status := getStatusForTest(t, ts.URL+"/api/verify-email?token="+secondToken); status != http.StatusOK {
		t.Fatalf("repeated verified token status=%d", status)
	}
}

func TestForwardingVerificationAutomaticallyActivatesPendingMailboxBinding(t *testing.T) {
	a := newTestApp(t)
	stopTestWorkers(a)
	a.updateConfig(func(cfg *Config) { cfg.SMTPHost = "postfix" })
	user, mailbox := defaultAdminUserAndMailbox(t, a)
	ctx := context.Background()
	now := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	email := "friend@example.net"
	verifiedID := newID("fwd")
	if err := a.issueForwardingVerification(ctx, user.ID, verifiedID, email, true); err != nil {
		t.Fatal(err)
	}
	bindingID := newID("fbind")
	if _, err := a.db.Exec(`INSERT INTO forwarding_pending_bindings(id,user_id,verified_email_id,target_email,scope,mailbox_id,status,failure_reason,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, bindingID, user.ID, verifiedID, email, forwardingBindingScopeMailbox, mailbox.ID, forwardingBindingPending, "", now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	token := latestForwardingTokenForTest(t, a, user.ID)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/verify-email?format=json&token=" + token)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var result struct {
		OK          bool                   `json:"ok"`
		Activations []ForwardingActivation `json:"activations"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || !result.OK || len(result.Activations) != 1 {
		t.Fatalf("status=%d result=%+v", resp.StatusCode, result)
	}
	if result.Activations[0].SourceEmail != mailbox.Address || result.Activations[0].TargetEmail != email {
		t.Fatalf("activation=%+v", result.Activations[0])
	}
	var target, targetsJSON, bindingStatus, tokenHash string
	if err := a.db.QueryRow(`SELECT target_email,target_emails FROM mailbox_forwarding_settings WHERE mailbox_id=?`, mailbox.ID).Scan(&target, &targetsJSON); err != nil {
		t.Fatal(err)
	}
	if target != email || len(forwardingTargetsFromStored(target, targetsJSON)) != 1 {
		t.Fatalf("target=%q targets=%q", target, targetsJSON)
	}
	if err := a.db.QueryRow(`SELECT status FROM forwarding_pending_bindings WHERE id=?`, bindingID).Scan(&bindingStatus); err != nil || bindingStatus != forwardingBindingActive {
		t.Fatalf("binding status=%q err=%v", bindingStatus, err)
	}
	if err := a.db.QueryRow(`SELECT verification_token_hash FROM forwarding_verified_emails WHERE id=?`, verifiedID).Scan(&tokenHash); err != nil || tokenHash != hashToken(token) {
		t.Fatalf("token hash=%q err=%v", tokenHash, err)
	}
	if status := getStatusForTest(t, ts.URL+"/api/verify-email?token="+token); status != http.StatusOK {
		t.Fatalf("repeated verified token status=%d", status)
	}
	var auditCount int
	if err := a.db.QueryRow(`SELECT COUNT(1) FROM forwarding_audit_events WHERE binding_id=? AND event='binding.activated'`, bindingID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("audit count=%d err=%v", auditCount, err)
	}
}

func TestForwardingVerificationTargetDailyLimit(t *testing.T) {
	a := newTestApp(t)
	stopTestWorkers(a)
	a.updateConfig(func(cfg *Config) { cfg.SMTPHost = "postfix" })
	user, _ := defaultAdminUserAndMailbox(t, a)
	ctx := context.Background()
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	now := base
	a.now = func() time.Time { return now }
	id := newID("fwd")
	for i := 0; i < forwardingVerificationTargetLimit; i++ {
		if i > 0 {
			now = base.Add(time.Duration(i) * 61 * time.Second)
		}
		if err := a.issueForwardingVerification(ctx, user.ID, id, "daily-limit@example.net", i == 0); err != nil {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
	}
	now = base.Add(time.Duration(forwardingVerificationTargetLimit) * 61 * time.Second)
	err := a.issueForwardingVerification(ctx, user.ID, id, "daily-limit@example.net", false)
	var rateErr *forwardingVerificationRateLimitError
	if !errors.As(err, &rateErr) || !strings.Contains(err.Error(), "24小时内") {
		t.Fatalf("daily limit error=%v", err)
	}
	var count int
	if err := a.db.QueryRow("SELECT COUNT(1) FROM forwarding_verification_attempts WHERE email=?", "daily-limit@example.net").Scan(&count); err != nil || count != forwardingVerificationTargetLimit {
		t.Fatalf("attempt count=%d err=%v", count, err)
	}
}

func TestForwardingVerificationRateLimitIsConcurrent(t *testing.T) {
	a := newTestApp(t)
	stopTestWorkers(a)
	a.updateConfig(func(cfg *Config) { cfg.SMTPHost = "postfix" })
	user, _ := defaultAdminUserAndMailbox(t, a)
	a.now = func() time.Time { return time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC) }
	ctx := context.Background()
	id := newID("fwd")
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- a.issueForwardingVerification(ctx, user.ID, id, "concurrent@example.net", true)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	var successes, limited int
	for err := range errs {
		if err == nil {
			successes++
			continue
		}
		var rateErr *forwardingVerificationRateLimitError
		if errors.As(err, &rateErr) {
			limited++
		} else {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if successes != 1 || limited != 1 {
		t.Fatalf("successes=%d limited=%d", successes, limited)
	}
}

func TestForwardingVerificationRateLimitResponse(t *testing.T) {
	recorder := httptest.NewRecorder()
	err := &forwardingVerificationRateLimitError{message: "操作过于频繁，请在17秒后重试", retryAfter: 17 * time.Second}
	if !respondForwardingVerificationRateLimit(recorder, err) {
		t.Fatal("rate limit error was not handled")
	}
	if recorder.Code != http.StatusTooManyRequests || recorder.Header().Get("Retry-After") != "17" {
		t.Fatalf("status=%d retry-after=%q", recorder.Code, recorder.Header().Get("Retry-After"))
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || !strings.Contains(body["error"], "17秒") {
		t.Fatalf("body=%q err=%v", recorder.Body.String(), err)
	}
}

func parseMIMEParts(t *testing.T, raw []byte) (*netmail.Message, map[string][]byte) {
	t.Helper()
	msg, err := netmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/alternative" {
		t.Fatalf("content type=%q err=%v", msg.Header.Get("Content-Type"), err)
	}
	reader := multipart.NewReader(msg.Body, params["boundary"])
	parts := map[string][]byte{}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		partType, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		data, err := io.ReadAll(part)
		if err != nil {
			t.Fatal(err)
		}
		if strings.EqualFold(part.Header.Get("Content-Transfer-Encoding"), "base64") {
			data, err = base64.StdEncoding.DecodeString(strings.Join(strings.Fields(string(data)), ""))
			if err != nil {
				t.Fatal(err)
			}
		}
		parts[partType] = data
	}
	return msg, parts
}

func queuedMIMEForTest(t *testing.T, a *App, queueID string) []byte {
	t.Helper()
	var encoded string
	if err := a.db.QueryRow("SELECT mime_base64 FROM send_queue WHERE id=?", queueID).Scan(&encoded); err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func latestForwardingTokenForTest(t *testing.T, a *App, userID string) string {
	t.Helper()
	var encoded string
	if err := a.db.QueryRow("SELECT mime_base64 FROM send_queue WHERE user_id=? AND source=? ORDER BY created_at DESC,id DESC LIMIT 1", userID, sendSourceForwardingVerification).Scan(&encoded); err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return extractForwardingVerificationToken(t, string(raw))
}

func getStatusForTest(t *testing.T, target string) int {
	t.Helper()
	response, err := http.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode
}
