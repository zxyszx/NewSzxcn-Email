package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestTelegramSettingsDiscoveryTestAndMailQueue(t *testing.T) {
	type sentMessage struct {
		ChatID      string         `json:"chat_id"`
		Text        string         `json:"text"`
		ReplyMarkup map[string]any `json:"reply_markup"`
	}
	var sent []sentMessage
	var pairingCode atomic.Value
	pairingCode.Store("")
	telegramServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/bottest-token/getMe":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1,"is_bot":true,"username":"newszxcn_test_bot"}}`))
		case "/bottest-token/getUpdates":
			code, _ := pairingCode.Load().(string)
			_, _ = fmt.Fprintf(w, `{"ok":true,"result":[{"update_id":6,"message":{"text":"/start wrong-code","chat":{"id":987654321,"type":"private","first_name":"Other"}}},{"update_id":7,"message":{"text":"/start %s","chat":{"id":123456789,"type":"private","first_name":"Zhenxi","last_name":"Shen"}}}]}`, code)
		case "/bottest-token/sendMessage":
			var message sentMessage
			if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
				t.Fatalf("decode Telegram message: %v", err)
			}
			sent = append(sent, message)
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":8}}`))
		case "/botpersonal-token/sendMessage":
			var message sentMessage
			if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
				t.Fatalf("decode personal Telegram message: %v", err)
			}
			sent = append(sent, message)
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":9}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer telegramServer.Close()

	a := newTestApp(t)
	stopTestWorkers(a)
	a.updateConfig(func(cfg *Config) { cfg.UpdateServiceToken = "telegram-user-test-secret" })
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
	var adminMailboxID string
	if err := a.db.QueryRow(`SELECT id FROM mailboxes WHERE address='admin@lanqin.local'`).Scan(&adminMailboxID); err != nil {
		t.Fatal(err)
	}
	var adminUserID string
	if err := a.db.QueryRow(`SELECT user_id FROM mailboxes WHERE id=?`, adminMailboxID).Scan(&adminUserID); err != nil {
		t.Fatal(err)
	}
	now := a.now().UTC().Format(time.RFC3339Nano)
	userTokenCipher, err := a.encryptBackupPassword("personal-token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO user_telegram_settings(user_id,enabled,bot_token_cipher,private_chat_id,created_at,updated_at) VALUES(?,1,?,'123456789',?,?)`, adminUserID, userTokenCipher, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO user_telegram_mailboxes(user_id,mailbox_id) VALUES(?,?)`, adminUserID, adminMailboxID); err != nil {
		t.Fatal(err)
	}
	payload["telegramMailboxIds"] = []string{adminMailboxID}
	if code := admin.do("POST", "/api/admin/settings", payload, &settings); code != http.StatusOK {
		t.Fatalf("save Telegram settings code=%d settings=%+v", code, settings)
	}
	if !settings.TelegramMailEnabled || !settings.TelegramBotTokenSet || settings.TelegramPrivateChatID != "123456789" || settings.TelegramBodyMode != "full" {
		t.Fatalf("unexpected Telegram settings: %+v", settings)
	}
	if a.config().TelegramBotToken != "test-token" {
		t.Fatal("Telegram token was not persisted in runtime config")
	}

	var pairing struct {
		Code     string `json:"code"`
		DeepLink string `json:"deepLink"`
	}
	if code := admin.do("POST", "/api/admin/settings/telegram/pair", map[string]string{"botToken": ""}, &pairing); code != http.StatusOK || pairing.Code == "" || !strings.Contains(pairing.DeepLink, pairing.Code) {
		t.Fatalf("create pairing code=%d response=%+v", code, pairing)
	}
	pairingCode.Store(pairing.Code)
	var discovered map[string]string
	if code := admin.do("POST", "/api/admin/settings/telegram/discover", map[string]string{"botToken": "", "pairingCode": pairing.Code}, &discovered); code != http.StatusOK {
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
		MailboxID:     adminMailboxID,
		RecipientAddr: "admin@example.com",
		Subject:       "账单 <已生成>",
		From:          "billing@example.net",
		FromName:      "Billing & Support",
		ReceivedAt:    receivedAt,
		BodyText:      "这是邮件正文，验证码是 846981，包含 <VIP> & 续费信息。",
	}, []AttachmentInput{{Filename: "账单-2026.pdf"}})
	if err := a.processDueTelegramMailNotifications(context.Background()); err != nil {
		t.Fatalf("process Telegram mail queue: %v", err)
	}
	if len(sent) != 1 {
		t.Fatalf("expected one queued Telegram message, got %d", len(sent))
	}
	text := sent[0].Text
	for _, expected := range []string{"新邮件通知", "Billing &amp; Support", "账单 &lt;已生成&gt;", "admin@example.com", "收件时间"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("Telegram mail message missing %q: %s", expected, text)
		}
	}
	for _, forbidden := range []string{"邮件正文", "账单-2026.pdf", "846981", "VIP", "点击查看"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("Telegram mail message unexpectedly included %q: %s", forbidden, text)
		}
	}
	markupJSON, _ := json.Marshal(sent[0].ReplyMarkup)
	if !strings.Contains(string(markupJSON), "查看邮件内容") || !strings.Contains(string(markupJSON), "message=mail_test_telegram") {
		t.Fatalf("Telegram view button was not included: %s", markupJSON)
	}
	var delivered, storedPayload string
	var telegramMessageID int64
	if err := a.db.QueryRow(`SELECT COALESCE(delivered_at,''),payload_json,telegram_message_id FROM telegram_mail_outbox WHERE message_id=?`, "mail_test_telegram").Scan(&delivered, &storedPayload, &telegramMessageID); err != nil || delivered == "" {
		t.Fatalf("Telegram queue was not marked delivered: delivered=%q err=%v", delivered, err)
	}
	if storedPayload != "{}" || telegramMessageID != 9 {
		t.Fatalf("delivered payload was not cleared safely: payload=%q telegramMessageId=%d", storedPayload, telegramMessageID)
	}

	a.enqueueTelegramMailNotification(context.Background(), "mail_pending_before_disable", storedMessage{MailboxID: adminMailboxID, RecipientAddr: "admin@lanqin.local", Subject: "pending", From: "sender@example.com", ReceivedAt: time.Now(), BodyText: "pending"}, nil)
	disablePayload := systemSettingsPayload(settings)
	disablePayload["telegramMailEnabled"] = false
	if code := admin.do("POST", "/api/admin/settings", disablePayload, &settings); code != http.StatusOK {
		t.Fatalf("disable Telegram settings code=%d", code)
	}
	var pending int
	if err := a.db.QueryRow(`SELECT COUNT(1) FROM telegram_mail_outbox WHERE delivered_at IS NULL`).Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("admin backup bot settings changed a personal queue: count=%d err=%v", pending, err)
	}
}

func TestTelegramSettingsRejectEnabledWithoutCredentials(t *testing.T) {
	a := newTestApp(t)
	a.updateConfig(func(cfg *Config) { cfg.UpdateServiceToken = "telegram-user-test-secret" })
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

func TestUserTelegramSettingsAreScopedToOwnedMailboxes(t *testing.T) {
	a := newTestApp(t)
	a.updateConfig(func(cfg *Config) { cfg.UpdateServiceToken = "telegram-user-test-secret" })
	server := httptest.NewServer(a.Router())
	defer server.Close()
	client := &testClient{t: t, server: server}
	var login map[string]any
	if code := client.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d", code)
	}
	var mailboxID string
	if err := a.db.QueryRow(`SELECT id FROM mailboxes WHERE address='admin@lanqin.local'`).Scan(&mailboxID); err != nil {
		t.Fatal(err)
	}
	var settings userTelegramSettings
	if code := client.do("POST", "/api/me/telegram", userTelegramSettingsRequest{Enabled: true, BotToken: "personal-test-token", PrivateChatID: "123456", MailboxIDs: []string{mailboxID}}, &settings); code != http.StatusOK {
		t.Fatalf("save personal Telegram settings code=%d response=%+v", code, settings)
	}
	if !settings.Enabled || settings.PrivateChatID != "123456" || len(settings.MailboxIDs) != 1 || settings.MailboxIDs[0] != mailboxID || !settings.BotConfigured {
		t.Fatalf("unexpected personal Telegram settings: %+v", settings)
	}
	var body map[string]any
	if code := client.do("POST", "/api/me/telegram", userTelegramSettingsRequest{Enabled: true, PrivateChatID: "123456", MailboxIDs: []string{"foreign-mailbox"}}, &body); code != http.StatusBadRequest {
		t.Fatalf("foreign mailbox should be rejected, code=%d body=%v", code, body)
	}
}

func TestTelegramNetworkErrorDoesNotExposeToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	serverURL := server.URL
	server.Close()

	a := newTestApp(t)
	stopTestWorkers(a)
	a.updateConfig(func(cfg *Config) { cfg.UpdateServiceToken = "telegram-user-test-secret" })
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

func TestTelegramNotificationMessageBudget(t *testing.T) {
	message := formatTelegramMailMessage(telegramMailPayload{
		From:       strings.Repeat("R&D <team@example.com> ", 30),
		Recipient:  "admin@example.com",
		Subject:    strings.Repeat("超长主题 & <test> ", 50),
		ReceivedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Body:       "正文验证码 846981 不应出现在通知中",
		OTP:        "846981",
		ViewURL:    "https://mail.example.com/?message=mail-1",
	})
	if got := utf8.RuneCountInString(message.HTML); got > telegramMessageBudget {
		t.Fatalf("Telegram HTML exceeds budget: %d", got)
	}
	if !strings.Contains(message.HTML, "&amp;") || !strings.Contains(message.HTML, "&lt;") {
		t.Fatalf("message escaping missing: %s", message.HTML)
	}
	if strings.Contains(message.HTML, "846981") || strings.Contains(message.PlainText, "846981") {
		t.Fatalf("notification exposed body or OTP: %+v", message)
	}
	if markup := telegramViewMarkup(message.ViewURL); markup == nil {
		t.Fatal("view button markup missing")
	}
}

func TestTelegramIQiyiNotificationOmitsOTPAndBody(t *testing.T) {
	subject := "825534 是您的动态安全验证码"
	body := "哈喽 iqiyi02@newszxcn.com 您正在进行爱奇艺账号的安全验证，以下是您的动态验证码：825534 如果这不是您的邮件，请忽略此邮件，请勿回复 手机·电视 其他 APP 在 LG, Samsung 等应用商店搜索 iQiyi 即可获得 Copyright © 2021 iQiyi All Rights Reserved"
	message := formatTelegramMailMessage(telegramMailPayload{Subject: subject, From: "no_reply_intl@iq.com", Recipient: "iqiyi02@newszxcn.com", ReceivedAt: time.Now().UTC().Format(time.RFC3339Nano), Body: body, OTP: "825534", ViewURL: "https://mail.example.com/?message=iqiyi"})
	if strings.Contains(message.HTML, body) || strings.Contains(message.HTML, "<code>") {
		t.Fatalf("iQiyi body or extracted OTP was included: %+v", message)
	}
}

func TestTelegramViewButton(t *testing.T) {
	const viewURL = "https://mail.example.com/?message=mail-123"
	markup := telegramViewMarkup(viewURL)
	buttons, ok := markup["inline_keyboard"].([][]map[string]any)
	if !ok || len(buttons) != 1 || len(buttons[0]) != 1 || buttons[0][0]["text"] != "查看邮件内容" || buttons[0][0]["url"] != viewURL {
		t.Fatalf("view button is invalid: %#v", markup)
	}
	if telegramViewMarkup("javascript:alert(1)") != nil {
		t.Fatal("unsafe view URL was accepted")
	}
}

func TestTelegramPseudoHTMLAndBodyCharset(t *testing.T) {
	pseudo := `<html><head><style>.hidden{display:none}</style></head><body><p>验证码：778899</p><div>欢迎登录</div></body></html>`
	text := telegramMessageBody(storedMessage{BodyText: pseudo})
	if strings.Contains(text, "display:none") || strings.Contains(text, "<p>") || !strings.Contains(text, "778899") {
		t.Fatalf("pseudo HTML was not cleaned: %q", text)
	}

	encoded, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("您的验证码是 445566"))
	if err != nil {
		t.Fatal(err)
	}
	raw := append([]byte("From: sender@example.com\r\nTo: admin@example.com\r\nSubject: GBK\r\nContent-Type: text/plain; charset=gbk\r\n\r\n"), encoded...)
	a := newTestApp(t)
	stopTestWorkers(a)
	msg, _, err := a.parseMaildirMessage(raw, "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg.BodyText, "445566") || !strings.Contains(msg.BodyText, "验证码") {
		t.Fatalf("GBK body was not decoded: %q", msg.BodyText)
	}
}

func TestTelegramRetryAfterAndPermanentErrors(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		response   string
		retryAfter time.Duration
	}{
		{name: "rate limit", status: http.StatusTooManyRequests, response: `{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":17}}`, retryAfter: 17 * time.Second},
		{name: "unauthorized", status: http.StatusUnauthorized, response: `{"ok":false,"error_code":401,"description":"Unauthorized"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.response))
			}))
			defer server.Close()
			a := newTestApp(t)
			stopTestWorkers(a)
			a.telegramURL = server.URL
			err := a.sendTelegramMessage(context.Background(), "test-token", "123456", "test")
			var apiErr *telegramAPIError
			if !errors.As(err, &apiErr) || apiErr.ErrorCode != tc.status || apiErr.RetryAfter != tc.retryAfter {
				t.Fatalf("unexpected Telegram error: %#v", err)
			}
		})
	}
}

func TestTelegramMailboxScopeAndOriginalRecipient(t *testing.T) {
	a := newTestApp(t)
	stopTestWorkers(a)
	a.updateConfig(func(cfg *Config) { cfg.UpdateServiceToken = "telegram-user-test-secret" })
	var mailboxID string
	if err := a.db.QueryRow(`SELECT id FROM mailboxes WHERE address='admin@lanqin.local'`).Scan(&mailboxID); err != nil {
		t.Fatal(err)
	}
	var userID string
	if err := a.db.QueryRow(`SELECT user_id FROM mailboxes WHERE id=?`, mailboxID).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	now := a.now().UTC().Format(time.RFC3339Nano)
	tokenCipher, err := a.encryptBackupPassword("test-token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO user_telegram_settings(user_id,enabled,bot_token_cipher,private_chat_id,created_at,updated_at) VALUES(?,1,?,'123456',?,?)`, userID, tokenCipher, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO user_telegram_mailboxes(user_id,mailbox_id) VALUES(?,?)`, userID, mailboxID); err != nil {
		t.Fatal(err)
	}
	a.enqueueTelegramMailNotification(context.Background(), "scope-denied", storedMessage{MailboxID: "another-mailbox", RecipientAddr: "other@example.com", Subject: "denied"}, nil)
	a.enqueueTelegramMailNotification(context.Background(), "scope-allowed", storedMessage{MailboxID: mailboxID, RecipientAddr: "admin@lanqin.local", Subject: "allowed"}, nil)
	var count int
	if err := a.db.QueryRow(`SELECT COUNT(1) FROM telegram_mail_outbox`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("unexpected scoped queue count=%d err=%v", count, err)
	}

	raw := []byte("From: sender@example.com\r\nTo: hidden-list@example.net\r\nDelivered-To: admin@lanqin.local\r\nSubject: recipient\r\n\r\nbody")
	msg, _, err := a.parseMaildirMessage(raw, "admin@lanqin.local")
	if err != nil {
		t.Fatal(err)
	}
	if msg.RecipientAddr != "admin@lanqin.local" {
		t.Fatalf("wrong original recipient: %q", msg.RecipientAddr)
	}
}

func TestParseMaildirMessageDecodesAppleGB2312(t *testing.T) {
	subject := "验证 Apple 账户电子邮件地址"
	body := "你的 Apple 验证码是 978534"
	encodedSubject, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(subject))
	if err != nil {
		t.Fatal(err)
	}
	encodedBody, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("From: Apple <appleid@id.apple.com>\r\n" +
		"To: admin@example.com\r\n" +
		"Subject: =?gb2312?B?" + base64.StdEncoding.EncodeToString(encodedSubject) + "?=\r\n" +
		"Content-Type: text/plain; charset=gb2312\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\n" +
		base64.StdEncoding.EncodeToString(encodedBody))
	a := newTestApp(t)
	stopTestWorkers(a)
	msg, _, err := a.parseMaildirMessage(raw, "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject != subject {
		t.Fatalf("GB2312 subject was not decoded: %q", msg.Subject)
	}
	if msg.BodyText != body {
		t.Fatalf("GB2312 body was not decoded: %q", msg.BodyText)
	}
}

func TestTelegramBadRequestFallsBackToPlainText(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: can't parse entities"}`))
			return
		}
		if _, exists := payload["parse_mode"]; exists {
			t.Fatal("plain-text fallback still included parse_mode")
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":99}}`))
	}))
	defer server.Close()
	a := newTestApp(t)
	stopTestWorkers(a)
	a.updateConfig(func(cfg *Config) { cfg.UpdateServiceToken = "telegram-user-test-secret" })
	a.telegramURL = server.URL
	messageID, err := a.deliverTelegramMailMessage(context.Background(), "test-token", "123456", telegramFormattedMessage{HTML: "<b>broken", PlainText: "safe fallback", ViewURL: "https://mail.example.com/?message=mail-1"})
	if err != nil || messageID != 99 || calls.Load() != 2 {
		t.Fatalf("fallback failed: messageId=%d calls=%d err=%v", messageID, calls.Load(), err)
	}
}

func TestTelegramMalformedQueueItemDoesNotBlockLaterMail(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":7}}`))
	}))
	defer server.Close()
	a := newTestApp(t)
	stopTestWorkers(a)
	a.updateConfig(func(cfg *Config) { cfg.UpdateServiceToken = "telegram-user-test-secret" })
	a.telegramURL = server.URL
	var userID string
	if err := a.db.QueryRow(`SELECT id FROM users ORDER BY created_at LIMIT 1`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	tokenCipher, err := a.encryptBackupPassword("test-token")
	if err != nil {
		t.Fatal(err)
	}
	now := a.now().UTC().Format(time.RFC3339Nano)
	if _, err := a.db.Exec(`INSERT INTO user_telegram_settings(user_id,enabled,bot_token_cipher,private_chat_id,created_at,updated_at) VALUES(?,1,?,'123456',?,?)`, userID, tokenCipher, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO telegram_mail_outbox(id,message_id,payload_json,next_attempt_at,created_at,updated_at) VALUES('bad','bad','{',?,?,?),('good','good',?, ?, ?, ?)`, now, now, now, jsonEncode(telegramMailPayload{UserID: userID, ChatID: "123456", Subject: "good", From: "sender@example.com", Recipient: "admin@example.com", ReceivedAt: now, Body: "body"}), now, now, now); err != nil {
		t.Fatal(err)
	}
	if err := a.processDueTelegramMailNotifications(context.Background()); err != nil {
		t.Fatal(err)
	}
	var badAttempts int
	var delivered string
	if err := a.db.QueryRow(`SELECT attempt_count FROM telegram_mail_outbox WHERE id='bad'`).Scan(&badAttempts); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow(`SELECT COALESCE(delivered_at,'') FROM telegram_mail_outbox WHERE id='good'`).Scan(&delivered); err != nil {
		t.Fatal(err)
	}
	if badAttempts != telegramMailMaxAttempts || delivered == "" || calls.Load() != 1 {
		t.Fatalf("malformed queue handling failed: attempts=%d delivered=%q calls=%d", badAttempts, delivered, calls.Load())
	}
}
