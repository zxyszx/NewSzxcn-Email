package app

import (
	"context"
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
	var adminMailboxID string
	if err := a.db.QueryRow(`SELECT id FROM mailboxes WHERE address='admin@lanqin.local'`).Scan(&adminMailboxID); err != nil {
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
	for _, expected := range []string{"新邮件通知", "Billing &amp; Support", "账单 &lt;已生成&gt;", "admin@example.com", "邮件正文", "账单-2026.pdf", "846981", "&lt;VIP&gt; &amp; 续费信息"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("Telegram mail message missing %q: %s", expected, text)
		}
	}
	if sent[0].ReplyMarkup == nil {
		t.Fatal("Telegram OTP copy button was not included")
	}
	var delivered, storedPayload string
	var telegramMessageID int64
	if err := a.db.QueryRow(`SELECT COALESCE(delivered_at,''),payload_json,telegram_message_id FROM telegram_mail_outbox WHERE message_id=?`, "mail_test_telegram").Scan(&delivered, &storedPayload, &telegramMessageID); err != nil || delivered == "" {
		t.Fatalf("Telegram queue was not marked delivered: delivered=%q err=%v", delivered, err)
	}
	if storedPayload != "{}" || telegramMessageID != 8 {
		t.Fatalf("delivered payload was not cleared safely: payload=%q telegramMessageId=%d", storedPayload, telegramMessageID)
	}

	a.enqueueTelegramMailNotification(context.Background(), "mail_pending_before_disable", storedMessage{MailboxID: adminMailboxID, RecipientAddr: "admin@lanqin.local", Subject: "pending", From: "sender@example.com", ReceivedAt: time.Now(), BodyText: "pending"}, nil)
	disablePayload := systemSettingsPayload(settings)
	disablePayload["telegramMailEnabled"] = false
	if code := admin.do("POST", "/api/admin/settings", disablePayload, &settings); code != http.StatusOK {
		t.Fatalf("disable Telegram settings code=%d", code)
	}
	var pending int
	if err := a.db.QueryRow(`SELECT COUNT(1) FROM telegram_mail_outbox WHERE delivered_at IS NULL`).Scan(&pending); err != nil || pending != 0 {
		t.Fatalf("pending Telegram queue was not cleared: count=%d err=%v", pending, err)
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

func TestTelegramOTPDetectionAndMessageBudget(t *testing.T) {
	body := "本次登录验证码为 846981，请在十分钟内完成验证。\n\nOn yesterday wrote:\n旧验证码是 112233"
	cleaned := stripTelegramQuotedContent(body)
	if otp := detectTelegramOTP("登录验证", cleaned); otp != "846981" {
		t.Fatalf("unexpected OTP %q", otp)
	}
	if otp := detectTelegramOTP("验证码", "验证码可能是 123456 或 654321，请联系客服确认"); otp != "" {
		t.Fatalf("ambiguous OTP should not be selected: %q", otp)
	}
	message := formatTelegramMailMessage(telegramMailPayload{
		From:       strings.Repeat("R&D <team@example.com> ", 30),
		Recipient:  "admin@example.com",
		Subject:    strings.Repeat("超长主题 & <test> ", 50),
		ReceivedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Body:       strings.Repeat("正文内容 & <重要> ", 1000),
		BodyMode:   "full",
		OTP:        "846981",
		AttachmentNames: []string{
			strings.Repeat("附件&", 80), strings.Repeat("报价<", 80), strings.Repeat("说明", 80),
		},
		AttachmentCount: 12,
	})
	if got := utf8.RuneCountInString(message.HTML); got > telegramMessageBudget {
		t.Fatalf("Telegram HTML exceeds budget: %d", got)
	}
	if !strings.Contains(message.HTML, "&amp;") || !strings.Contains(message.HTML, "&lt;") || !strings.Contains(message.HTML, "<code>846981</code>") {
		t.Fatalf("message escaping or OTP formatting missing: %s", message.HTML)
	}
	if markup := telegramCopyMarkup(message.OTP); markup == nil {
		t.Fatal("copy_text markup missing")
	}
}

func TestTelegramIQiyiOTPDetection(t *testing.T) {
	subject := "825534 是您的动态安全验证码"
	body := "哈喽 iqiyi02@newszxcn.com 您正在进行爱奇艺账号的安全验证，以下是您的动态验证码：825534 如果这不是您的邮件，请忽略此邮件，请勿回复 手机·电视 其他 APP 在 LG, Samsung 等应用商店搜索 iQiyi 即可获得 Copyright © 2021 iQiyi All Rights Reserved"
	otp := detectTelegramOTP(subject, body)
	if otp != "825534" {
		t.Fatalf("iQiyi OTP not detected: %q", otp)
	}
	message := formatTelegramMailMessage(telegramMailPayload{Subject: subject, From: "no_reply_intl@iq.com", Recipient: "iqiyi02@newszxcn.com", ReceivedAt: time.Now().UTC().Format(time.RFC3339Nano), Body: body, OTP: otp})
	if !strings.Contains(message.HTML, "<code>825534</code>") || telegramCopyMarkup(message.OTP) == nil {
		t.Fatalf("iQiyi OTP section or copy button missing: %+v", message)
	}
}

func TestTelegramForwardedGateOTPAndLinks(t *testing.T) {
	body := `---------- Forwarded message ---------
Date: 2026年8月6日周四 17:59
Subject: 登录验证码 (https://www.gate.com)

Gate 检测到您的账号正试图从此 IP 获得登录验证码：
IP: 87.83.105.229
如为您本人登录，请输入如下验证码完成操作：
311665
如非本人操作，请点击此处禁用账户 <https://data.gate.com/track/click?token=abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123456789>`
	if otp := detectTelegramOTP("Fwd: 登录验证码 (https://www.gate.com)", body); otp != "311665" {
		t.Fatalf("forwarded Gate OTP not detected: %q", otp)
	}
	if otp := detectTelegramOTP("登录验证码", "日期 2026-08-06，验证码将在稍后发送"); otp != "" {
		t.Fatalf("year was incorrectly detected as OTP: %q", otp)
	}
	message := formatTelegramMailMessage(telegramMailPayload{
		From: "no-reply@alert.gate.com", Recipient: "admin@example.com", Subject: "登录验证码",
		ReceivedAt: time.Now().UTC().Format(time.RFC3339Nano), Body: body, BodyMode: "full", OTP: "311665",
	})
	if !strings.Contains(message.HTML, `<a href="https://www.gate.com">https://www.gate.com</a>`) {
		t.Fatalf("normal URL was not linkified: %s", message.HTML)
	}
	if !strings.Contains(message.HTML, `>🔗 data.gate.com 链接</a>`) {
		t.Fatalf("long tracking URL was not shortened: %s", message.HTML)
	}
	if strings.Contains(message.HTML, "&lt;a href=") || utf8.RuneCountInString(message.HTML) > telegramMessageBudget {
		t.Fatalf("generated Telegram HTML is invalid or too long: %s", message.HTML)
	}
	markup := telegramCopyMarkup(message.OTP)
	buttons, ok := markup["inline_keyboard"].([][]map[string]any)
	if !ok || len(buttons) != 1 || len(buttons[0]) != 1 || buttons[0][0]["text"] != "复制验证码" {
		t.Fatalf("copy OTP button missing: %#v", markup)
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
	var mailboxID string
	if err := a.db.QueryRow(`SELECT id FROM mailboxes WHERE address='admin@lanqin.local'`).Scan(&mailboxID); err != nil {
		t.Fatal(err)
	}
	a.updateConfig(func(cfg *Config) {
		cfg.TelegramMailEnabled = true
		cfg.TelegramBotToken = "test-token"
		cfg.TelegramPrivateChatID = "123456"
		cfg.TelegramMailboxIDs = mailboxID
	})
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
	a.telegramURL = server.URL
	messageID, err := a.deliverTelegramMailMessage(context.Background(), "test-token", "123456", telegramFormattedMessage{HTML: "<b>broken", PlainText: "safe fallback", OTP: "123456"})
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
	a.telegramURL = server.URL
	a.updateConfig(func(cfg *Config) {
		cfg.TelegramMailEnabled = true
		cfg.TelegramBotToken = "test-token"
		cfg.TelegramPrivateChatID = "123456"
	})
	now := a.now().UTC().Format(time.RFC3339Nano)
	if _, err := a.db.Exec(`INSERT INTO telegram_mail_outbox(id,message_id,payload_json,next_attempt_at,created_at,updated_at) VALUES('bad','bad','{',?,?,?),('good','good',?, ?, ?, ?)`, now, now, now, jsonEncode(telegramMailPayload{Subject: "good", From: "sender@example.com", Recipient: "admin@example.com", ReceivedAt: now, Body: "body"}), now, now, now); err != nil {
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
