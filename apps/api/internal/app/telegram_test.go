package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTelegramSettingsDiscoveryTestAndMailQueue(t *testing.T) {
	type sentMessage struct {
		ChatID string `json:"chat_id"`
		Text   string `json:"text"`
	}
	var sent []sentMessage
	telegramServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/bottest-token/getUpdates":
			_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":7,"message":{"chat":{"id":123456789,"type":"private","first_name":"Zhenxi","last_name":"Shen"}}}]}`))
		case "/bottest-token/sendMessage":
			var message sentMessage
			if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
				t.Fatalf("decode Telegram message: %v", err)
			}
			sent = append(sent, message)
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":8}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer telegramServer.Close()

	a := newTestApp(t)
	stopTestWorkers(a)
	a.telegramURL = telegramServer.URL
	server := httptest.NewServer(a.Router())
	defer server.Close()
	admin := &testClient{t: t, server: server}
	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d body=%v", code, login)
	}

	var settings SystemSettings
	if code := admin.do("GET", "/api/admin/settings", nil, &settings); code != http.StatusOK {
		t.Fatalf("get settings code=%d", code)
	}
	payload := systemSettingsPayload(settings)
	payload["telegramMailEnabled"] = true
	payload["telegramBotToken"] = "test-token"
	payload["telegramPrivateChatId"] = "123456789"
	payload["telegramBodyMode"] = "full"
	if code := admin.do("POST", "/api/admin/settings", payload, &settings); code != http.StatusOK {
		t.Fatalf("save Telegram settings code=%d settings=%+v", code, settings)
	}
	if !settings.TelegramMailEnabled || !settings.TelegramBotTokenSet || settings.TelegramPrivateChatID != "123456789" || settings.TelegramBodyMode != "full" {
		t.Fatalf("unexpected Telegram settings: %+v", settings)
	}
	if a.config().TelegramBotToken != "test-token" {
		t.Fatal("Telegram token was not persisted in runtime config")
	}

	var discovered map[string]string
	if code := admin.do("POST", "/api/admin/settings/telegram/discover", map[string]string{"botToken": ""}, &discovered); code != http.StatusOK {
		t.Fatalf("discover chat code=%d response=%v", code, discovered)
	}
	if discovered["chatId"] != "123456789" || discovered["displayName"] != "Zhenxi Shen" {
		t.Fatalf("unexpected discovered chat: %v", discovered)
	}
	var testResult map[string]any
	if code := admin.do("POST", "/api/admin/settings/telegram/test", map[string]string{"botToken": "", "chatId": ""}, &testResult); code != http.StatusOK {
		t.Fatalf("test Telegram code=%d response=%v", code, testResult)
	}
	if len(sent) != 1 || sent[0].ChatID != "123456789" || !strings.Contains(sent[0].Text, "通知测试") {
		t.Fatalf("unexpected Telegram test message: %+v", sent)
	}

	sent = nil
	receivedAt := time.Date(2026, 8, 6, 9, 30, 0, 0, time.UTC)
	a.enqueueTelegramMailNotification(context.Background(), "mail_test_telegram", storedMessage{
		RecipientAddr: "admin@example.com",
		Subject:       "账单 <已生成>",
		From:          "billing@example.net",
		FromName:      "Billing & Support",
		ReceivedAt:    receivedAt,
		BodyText:      "这是邮件正文，包含 <VIP> & 续费信息。",
	}, []AttachmentInput{{Filename: "账单-2026.pdf"}})
	if err := a.processDueTelegramMailNotifications(context.Background()); err != nil {
		t.Fatalf("process Telegram mail queue: %v", err)
	}
	if len(sent) != 1 {
		t.Fatalf("expected one queued Telegram message, got %d", len(sent))
	}
	text := sent[0].Text
	for _, expected := range []string{"收到新邮件", "Billing &amp; Support", "账单 &lt;已生成&gt;", "admin@example.com", "邮件正文", "账单-2026.pdf", "&lt;VIP&gt; &amp; 续费信息"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("Telegram mail message missing %q: %s", expected, text)
		}
	}
	var delivered string
	if err := a.db.QueryRow(`SELECT COALESCE(delivered_at,'') FROM telegram_mail_outbox WHERE message_id=?`, "mail_test_telegram").Scan(&delivered); err != nil || delivered == "" {
		t.Fatalf("Telegram queue was not marked delivered: delivered=%q err=%v", delivered, err)
	}
}

func TestTelegramSettingsRejectEnabledWithoutCredentials(t *testing.T) {
	a := newTestApp(t)
	server := httptest.NewServer(a.Router())
	defer server.Close()
	admin := &testClient{t: t, server: server}
	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d", code)
	}
	var settings SystemSettings
	if code := admin.do("GET", "/api/admin/settings", nil, &settings); code != http.StatusOK {
		t.Fatalf("get settings code=%d", code)
	}
	payload := systemSettingsPayload(settings)
	payload["telegramMailEnabled"] = true
	var body map[string]any
	if code := admin.do("POST", "/api/admin/settings", payload, &body); code != http.StatusBadRequest {
		t.Fatalf("expected missing Telegram credentials to fail, code=%d body=%v", code, body)
	}
}

func TestTelegramNetworkErrorDoesNotExposeToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	serverURL := server.URL
	server.Close()

	a := newTestApp(t)
	stopTestWorkers(a)
	a.telegramURL = serverURL
	const token = "123456:secret-token-value"
	err := a.sendTelegramMessage(context.Background(), token, "123456789", "test")
	if err == nil {
		t.Fatal("expected Telegram network request to fail")
	}
	if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "secret-token-value") {
		t.Fatalf("Telegram error exposed Bot Token: %v", err)
	}
}
