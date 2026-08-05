package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const telegramMailMaxAttempts = 8

type telegramMailPayload struct {
	From            string   `json:"from"`
	FromName        string   `json:"fromName,omitempty"`
	Recipient       string   `json:"recipient"`
	Subject         string   `json:"subject"`
	ReceivedAt      string   `json:"receivedAt"`
	Body            string   `json:"body"`
	BodyMode        string   `json:"bodyMode"`
	AttachmentNames []string `json:"attachmentNames,omitempty"`
}

type telegramCredentialsRequest struct {
	BotToken string `json:"botToken"`
	ChatID   string `json:"chatId"`
}

type telegramAPIResponse struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description"`
	Result      json.RawMessage `json:"result"`
}

type telegramUpdate struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		Chat struct {
			ID        int64  `json:"id"`
			Type      string `json:"type"`
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
			Username  string `json:"username"`
		} `json:"chat"`
	} `json:"message"`
}

func normalizeTelegramBodyMode(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "full") {
		return "full"
	}
	return "summary"
}

func validTelegramPrivateChatID(value string) bool {
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return err == nil && id > 0
}

func (a *App) handleDiscoverTelegramChat(w http.ResponseWriter, r *http.Request) {
	var req telegramCredentialsRequest
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	token := strings.TrimSpace(req.BotToken)
	if token == "" {
		token = strings.TrimSpace(a.config().TelegramBotToken)
	}
	if token == "" {
		badRequest(w, errors.New("请先填写 Telegram Bot Token"))
		return
	}
	chatID, displayName, err := a.discoverTelegramPrivateChat(r.Context(), token)
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"chatId": chatID, "displayName": displayName})
}

func (a *App) handleTestTelegram(w http.ResponseWriter, r *http.Request) {
	var req telegramCredentialsRequest
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	token, chatID := a.telegramCredentials(req)
	if token == "" {
		badRequest(w, errors.New("请先填写 Telegram Bot Token"))
		return
	}
	if !validTelegramPrivateChatID(chatID) {
		badRequest(w, errors.New("请先获取或填写有效的私聊 Chat ID"))
		return
	}
	now := a.now().Local().Format("2006-01-02 15:04:05 MST")
	text := "<b>NewSzxcn 邮箱通知测试</b>\n\nTelegram 私聊邮件通知连接正常。\n\n<b>测试时间：</b>" + html.EscapeString(now)
	if err := a.sendTelegramMessage(r.Context(), token, chatID, text); err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) telegramCredentials(req telegramCredentialsRequest) (string, string) {
	cfg := a.config()
	token := strings.TrimSpace(req.BotToken)
	if token == "" {
		token = strings.TrimSpace(cfg.TelegramBotToken)
	}
	chatID := strings.TrimSpace(req.ChatID)
	if chatID == "" {
		chatID = strings.TrimSpace(cfg.TelegramPrivateChatID)
	}
	return token, chatID
}

func (a *App) discoverTelegramPrivateChat(ctx context.Context, token string) (string, string, error) {
	var updates []telegramUpdate
	if err := a.callTelegram(ctx, token, "getUpdates", map[string]any{
		"limit":           100,
		"timeout":         0,
		"allowed_updates": []string{"message"},
	}, &updates); err != nil {
		return "", "", err
	}
	for i := len(updates) - 1; i >= 0; i-- {
		message := updates[i].Message
		if message == nil || message.Chat.Type != "private" || message.Chat.ID <= 0 {
			continue
		}
		name := strings.TrimSpace(strings.Join([]string{message.Chat.FirstName, message.Chat.LastName}, " "))
		if name == "" && message.Chat.Username != "" {
			name = "@" + message.Chat.Username
		}
		return strconv.FormatInt(message.Chat.ID, 10), name, nil
	}
	return "", "", errors.New("未找到私聊会话，请先在 Telegram 中打开机器人并发送 /start，然后重试")
}

func (a *App) enqueueTelegramMailNotification(ctx context.Context, messageID string, msg storedMessage, attachments []AttachmentInput) {
	cfg := a.config()
	if !cfg.TelegramMailEnabled || strings.TrimSpace(cfg.TelegramBotToken) == "" || !validTelegramPrivateChatID(cfg.TelegramPrivateChatID) {
		return
	}
	recipient := normalizeEmail(msg.RecipientAddr)
	if recipient == "" && len(msg.To) > 0 {
		recipient = normalizeEmail(msg.To[0])
	}
	body := strings.TrimSpace(msg.BodyText)
	if body == "" {
		body = strings.TrimSpace(stripTags(msg.BodyHTML))
	}
	mode := normalizeTelegramBodyMode(cfg.TelegramBodyMode)
	limit := 800
	if mode == "full" {
		limit = 2600
	}
	body, truncated := truncateRunes(strings.Join(strings.Fields(body), " "), limit)
	if truncated {
		body += "..."
	}
	if body == "" {
		body = strings.TrimSpace(msg.Snippet)
	}
	names := make([]string, 0, len(attachments))
	for _, attachment := range attachments {
		name := strings.TrimSpace(attachment.Filename)
		if name != "" {
			names = append(names, name)
		}
		if len(names) >= 10 {
			break
		}
	}
	payload := telegramMailPayload{
		From:            msg.From,
		FromName:        msg.FromName,
		Recipient:       recipient,
		Subject:         msg.Subject,
		ReceivedAt:      msg.ReceivedAt.Format(time.RFC3339Nano),
		Body:            body,
		BodyMode:        mode,
		AttachmentNames: names,
	}
	now := a.now().UTC().Format(time.RFC3339Nano)
	if _, err := a.db.ExecContext(ctx, `INSERT OR IGNORE INTO telegram_mail_outbox(id,message_id,payload_json,next_attempt_at,created_at,updated_at) VALUES(?,?,?,?,?,?)`, newID("tgm"), messageID, jsonEncode(payload), now, now, now); err != nil {
		a.log.Warn("failed to enqueue Telegram mail notification", "messageId", messageID, "error", err)
	}
}

func (a *App) telegramMailWorker(ctx context.Context) {
	a.log.Info("Telegram mail notification worker started")
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if err := a.processDueTelegramMailNotifications(ctx); err != nil && !errors.Is(err, context.Canceled) {
			a.log.Warn("Telegram mail notification worker failed", "error", err)
		}
		select {
		case <-ctx.Done():
			a.log.Info("Telegram mail notification worker stopped")
			return
		case <-ticker.C:
		}
	}
}

func (a *App) processDueTelegramMailNotifications(ctx context.Context) error {
	cfg := a.config()
	if !cfg.TelegramMailEnabled || strings.TrimSpace(cfg.TelegramBotToken) == "" || !validTelegramPrivateChatID(cfg.TelegramPrivateChatID) {
		return nil
	}
	_, _ = a.db.ExecContext(ctx, `DELETE FROM telegram_mail_outbox WHERE updated_at<? AND (delivered_at IS NOT NULL OR attempt_count>=?)`, a.now().UTC().Add(-30*24*time.Hour).Format(time.RFC3339Nano), telegramMailMaxAttempts)
	rows, err := a.db.QueryContext(ctx, `SELECT id,payload_json,attempt_count FROM telegram_mail_outbox WHERE delivered_at IS NULL AND attempt_count<? AND next_attempt_at<=? ORDER BY next_attempt_at,created_at LIMIT 20`, telegramMailMaxAttempts, a.now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	type queueItem struct {
		id      string
		payload telegramMailPayload
		attempt int
	}
	items := []queueItem{}
	for rows.Next() {
		var item queueItem
		var raw string
		if err := rows.Scan(&item.id, &raw, &item.attempt); err != nil {
			rows.Close()
			return err
		}
		if err := json.Unmarshal([]byte(raw), &item.payload); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range items {
		err := a.sendTelegramMessage(ctx, cfg.TelegramBotToken, cfg.TelegramPrivateChatID, formatTelegramMailMessage(item.payload))
		now := a.now().UTC()
		if err != nil {
			next := now.Add(sendRetryDelay(item.attempt + 1))
			_, _ = a.db.ExecContext(ctx, `UPDATE telegram_mail_outbox SET attempt_count=attempt_count+1,next_attempt_at=?,last_error=?,updated_at=? WHERE id=? AND delivered_at IS NULL`, next.Format(time.RFC3339Nano), truncateWebhookError(err.Error()), now.Format(time.RFC3339Nano), item.id)
			continue
		}
		stamp := now.Format(time.RFC3339Nano)
		_, _ = a.db.ExecContext(ctx, `UPDATE telegram_mail_outbox SET attempt_count=attempt_count+1,last_error='',updated_at=?,delivered_at=? WHERE id=? AND delivered_at IS NULL`, stamp, stamp, item.id)
	}
	return nil
}

func formatTelegramMailMessage(payload telegramMailPayload) string {
	subject := strings.TrimSpace(payload.Subject)
	if subject == "" || subject == "(no subject)" {
		subject = "（无主题）"
	}
	from := strings.TrimSpace(payload.From)
	if name := strings.TrimSpace(payload.FromName); name != "" {
		from = name + " <" + from + ">"
	}
	receivedAt := parseTime(payload.ReceivedAt)
	timeText := strings.TrimSpace(payload.ReceivedAt)
	if !receivedAt.IsZero() {
		timeText = receivedAt.Local().Format("2006-01-02 15:04:05 MST")
	}
	lines := []string{
		"<b>收到新邮件</b>",
		"",
		"<b>发件人：</b>" + html.EscapeString(from),
		"<b>收件邮箱：</b>" + html.EscapeString(payload.Recipient),
		"<b>主题：</b>" + html.EscapeString(subject),
		"<b>收件时间：</b>" + html.EscapeString(timeText),
	}
	if len(payload.AttachmentNames) > 0 {
		names := make([]string, 0, len(payload.AttachmentNames))
		for _, name := range payload.AttachmentNames {
			names = append(names, html.EscapeString(name))
		}
		lines = append(lines, "<b>附件：</b>"+strings.Join(names, "、"))
	}
	body := strings.TrimSpace(payload.Body)
	if body != "" {
		label := "正文摘要"
		if normalizeTelegramBodyMode(payload.BodyMode) == "full" {
			label = "邮件正文"
		}
		lines = append(lines, "", "<b>"+label+"：</b>", "<blockquote>"+html.EscapeString(body)+"</blockquote>")
	}
	return strings.Join(lines, "\n")
}

func (a *App) sendTelegramMessage(ctx context.Context, token, chatID, text string) error {
	return a.callTelegram(ctx, token, "sendMessage", map[string]any{
		"chat_id":                  chatID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}, nil)
}

func (a *App) callTelegram(ctx context.Context, token, method string, payload any, result any) error {
	token = strings.TrimSpace(token)
	if token == "" || strings.ContainsAny(token, "/\\\r\n") {
		return errors.New("Telegram Bot Token 无效")
	}
	base := strings.TrimRight(strings.TrimSpace(a.telegramURL), "/")
	endpoint := base + "/bot" + url.PathEscape(token) + "/" + method
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "NewSzxcn-Email-Telegram/1.0")
	client := &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("Telegram 请求失败，请检查网络连接和机器人配置")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	var apiResponse telegramAPIResponse
	if err := json.Unmarshal(raw, &apiResponse); err != nil {
		return fmt.Errorf("Telegram 返回了无效响应（HTTP %d）", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !apiResponse.OK {
		description := strings.TrimSpace(apiResponse.Description)
		if description == "" {
			description = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("Telegram 发送失败: %s", description)
	}
	if result != nil && len(apiResponse.Result) > 0 {
		if err := json.Unmarshal(apiResponse.Result, result); err != nil {
			return err
		}
	}
	return nil
}
