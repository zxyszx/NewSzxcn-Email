package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	nethtml "golang.org/x/net/html"
)

const (
	telegramMailMaxAttempts = 8
	telegramMessageBudget   = 3800
	telegramPairingTTL      = 10 * time.Minute
)

type telegramPairing struct {
	TokenFingerprint string
	ExpiresAt        time.Time
}

type telegramMailPayload struct {
	From            string   `json:"from"`
	FromName        string   `json:"fromName,omitempty"`
	Recipient       string   `json:"recipient"`
	Subject         string   `json:"subject"`
	ReceivedAt      string   `json:"receivedAt"`
	Body            string   `json:"body"`
	BodyMode        string   `json:"bodyMode"`
	OTP             string   `json:"otp,omitempty"`
	AttachmentNames []string `json:"attachmentNames,omitempty"`
	AttachmentCount int      `json:"attachmentCount,omitempty"`
}

type telegramCredentialsRequest struct {
	BotToken    string `json:"botToken"`
	ChatID      string `json:"chatId"`
	PairingCode string `json:"pairingCode"`
}

type telegramAPIResponse struct {
	OK          bool            `json:"ok"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Result      json.RawMessage `json:"result"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

type telegramUpdate struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		Text string `json:"text"`
		Chat struct {
			ID        int64  `json:"id"`
			Type      string `json:"type"`
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
			Username  string `json:"username"`
		} `json:"chat"`
	} `json:"message"`
}

type telegramAPIError struct {
	HTTPStatus  int
	ErrorCode   int
	Description string
	RetryAfter  time.Duration
}

func (e *telegramAPIError) Error() string {
	description := strings.TrimSpace(e.Description)
	if description == "" {
		description = fmt.Sprintf("HTTP %d", e.HTTPStatus)
	}
	return "Telegram 发送失败: " + description
}

type telegramSentMessage struct {
	MessageID int64 `json:"message_id"`
}

type telegramFormattedMessage struct {
	HTML      string
	PlainText string
	OTP       string
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

func (a *App) handleCreateTelegramPairing(w http.ResponseWriter, r *http.Request) {
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
	var bot struct {
		Username string `json:"username"`
	}
	if err := a.callTelegram(r.Context(), token, "getMe", map[string]any{}, &bot); err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}
	if strings.TrimSpace(bot.Username) == "" {
		respondError(w, http.StatusBadGateway, "Telegram 机器人没有可用的用户名")
		return
	}
	code, err := newTelegramPairingCode()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "无法生成 Telegram 绑定码")
		return
	}
	expiresAt := a.now().UTC().Add(telegramPairingTTL)
	a.telegramPairMu.Lock()
	for value, pairing := range a.telegramPairs {
		if !pairing.ExpiresAt.After(a.now().UTC()) {
			delete(a.telegramPairs, value)
		}
	}
	a.telegramPairs[code] = telegramPairing{TokenFingerprint: telegramTokenFingerprint(token), ExpiresAt: expiresAt}
	a.telegramPairMu.Unlock()
	respondJSON(w, http.StatusOK, map[string]string{
		"code":        code,
		"botUsername": bot.Username,
		"deepLink":    "https://t.me/" + url.PathEscape(bot.Username) + "?start=" + url.QueryEscape(code),
		"expiresAt":   expiresAt.Format(time.RFC3339Nano),
	})
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
	code := strings.ToUpper(strings.TrimSpace(req.PairingCode))
	if token == "" || code == "" {
		badRequest(w, errors.New("请先生成 Telegram 一次性绑定码"))
		return
	}
	a.telegramPairMu.Lock()
	pairing, ok := a.telegramPairs[code]
	a.telegramPairMu.Unlock()
	if !ok || !pairing.ExpiresAt.After(a.now().UTC()) || pairing.TokenFingerprint != telegramTokenFingerprint(token) {
		badRequest(w, errors.New("Telegram 绑定码无效或已过期，请重新生成"))
		return
	}
	chatID, displayName, err := a.discoverTelegramPrivateChat(r.Context(), token, code)
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}
	a.telegramPairMu.Lock()
	delete(a.telegramPairs, code)
	a.telegramPairMu.Unlock()
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

func (a *App) discoverTelegramPrivateChat(ctx context.Context, token, pairingCode string) (string, string, error) {
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
		text := strings.TrimSpace(message.Text)
		if text != pairingCode && text != "/start "+pairingCode {
			continue
		}
		name := strings.TrimSpace(strings.Join([]string{message.Chat.FirstName, message.Chat.LastName}, " "))
		if name == "" && message.Chat.Username != "" {
			name = "@" + message.Chat.Username
		}
		return strconv.FormatInt(message.Chat.ID, 10), name, nil
	}
	return "", "", errors.New("未找到匹配的私聊，请打开机器人发送绑定码后重试")
}

func newTelegramPairingCode() (string, error) {
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(raw)), nil
}

func telegramTokenFingerprint(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func telegramMailboxAllowed(cfg Config, mailboxID string) bool {
	mailboxID = strings.TrimSpace(mailboxID)
	if mailboxID == "" {
		return cfg.TelegramIncludeUnregistered
	}
	for _, id := range cleanIDList(strings.Split(cfg.TelegramMailboxIDs, ",")) {
		if id == mailboxID {
			return true
		}
	}
	return false
}

func (a *App) activeTelegramMailboxIDs(ctx context.Context, values []string) []string {
	ids := cleanIDList(values)
	active := make([]string, 0, len(ids))
	for _, id := range ids {
		var exists int
		if err := a.db.QueryRowContext(ctx, `SELECT 1 FROM mailboxes WHERE id=? AND status='active'`, id).Scan(&exists); err == nil && exists == 1 {
			active = append(active, id)
		}
	}
	return active
}

func (a *App) enqueueTelegramMailNotification(ctx context.Context, messageID string, msg storedMessage, attachments []AttachmentInput) {
	cfg := a.config()
	if !cfg.TelegramMailEnabled || strings.TrimSpace(cfg.TelegramBotToken) == "" || !validTelegramPrivateChatID(cfg.TelegramPrivateChatID) || !telegramMailboxAllowed(cfg, msg.MailboxID) {
		return
	}
	recipient := normalizeEmail(msg.RecipientAddr)
	if recipient == "" && len(msg.To) > 0 {
		recipient = normalizeEmail(msg.To[0])
	}
	body := telegramMessageBody(msg)
	otp := detectTelegramOTP(msg.Subject, body)
	mode := normalizeTelegramBodyMode(cfg.TelegramBodyMode)
	limit := 800
	if mode == "full" {
		limit = 2600
	}
	body, truncated := truncateRunes(body, limit)
	if truncated {
		body += "..."
	}
	if body == "" {
		body = normalizeTelegramText(msg.Snippet)
	}
	from, _ := truncateRunes(strings.TrimSpace(msg.From), 254)
	fromName, _ := truncateRunes(strings.TrimSpace(msg.FromName), 160)
	subject, _ := truncateRunes(strings.TrimSpace(msg.Subject), 240)
	names := make([]string, 0, min(len(attachments), 5))
	for _, attachment := range attachments {
		name := sanitizeTelegramAttachmentName(attachment.Filename)
		if name != "" {
			names = append(names, name)
		}
		if len(names) >= 5 {
			break
		}
	}
	payload := telegramMailPayload{
		From:            from,
		FromName:        fromName,
		Recipient:       recipient,
		Subject:         subject,
		ReceivedAt:      a.now().UTC().Format(time.RFC3339Nano),
		Body:            body,
		BodyMode:        mode,
		OTP:             otp,
		AttachmentNames: names,
		AttachmentCount: len(attachments),
	}
	now := a.now().UTC().Format(time.RFC3339Nano)
	if _, err := a.db.ExecContext(ctx, `INSERT OR IGNORE INTO telegram_mail_outbox(id,message_id,payload_json,next_attempt_at,created_at,updated_at) VALUES(?,?,?,?,?,?)`, newID("tgm"), messageID, jsonEncode(payload), now, now, now); err != nil {
		a.log.Warn("failed to enqueue Telegram mail notification", "messageId", messageID, "error", err)
	}
}

func telegramMessageBody(msg storedMessage) string {
	text := strings.TrimSpace(msg.BodyText)
	text, _ = truncateRunes(text, 128*1024)
	if text != "" && looksLikeHTMLDocument(text) {
		text = telegramHTMLToText(text)
	}
	if strings.TrimSpace(text) == "" {
		text = telegramHTMLToText(msg.BodyHTML)
	}
	return stripTelegramQuotedContent(normalizeTelegramText(text))
}

func looksLikeHTMLDocument(value string) bool {
	value, _ = truncateRunes(value, 128*1024)
	value = strings.ToLower(strings.TrimSpace(value))
	if strings.HasPrefix(value, "<!doctype html") || strings.HasPrefix(value, "<html") || strings.HasPrefix(value, "<head") || strings.HasPrefix(value, "<body") || strings.HasPrefix(value, "<style") {
		return true
	}
	matches := telegramHTMLTagRe.FindAllStringIndex(value, 4)
	return len(matches) >= 3
}

func telegramHTMLToText(value string) string {
	value = strings.ToValidUTF8(value, "�")
	value, _ = truncateRunes(value, 128*1024)
	doc, err := nethtml.Parse(strings.NewReader(value))
	if err != nil {
		return stripTags(value)
	}
	var out strings.Builder
	var walk func(*nethtml.Node, bool)
	walk = func(node *nethtml.Node, skipped bool) {
		if node.Type == nethtml.ElementNode {
			switch strings.ToLower(node.Data) {
			case "script", "style", "head", "noscript", "svg":
				skipped = true
			case "br":
				if !skipped {
					out.WriteByte('\n')
				}
			}
		}
		if node.Type == nethtml.TextNode && !skipped {
			out.WriteString(node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child, skipped)
		}
		if node.Type == nethtml.ElementNode && !skipped {
			switch strings.ToLower(node.Data) {
			case "p", "div", "li", "tr", "table", "section", "article", "header", "footer", "h1", "h2", "h3", "h4", "h5", "h6":
				out.WriteByte('\n')
			}
		}
	}
	walk(doc, false)
	return normalizeTelegramText(out.String())
}

func normalizeTelegramText(value string) string {
	value = strings.ReplaceAll(strings.ToValidUTF8(value, "�"), "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	lines := strings.Split(value, "\n")
	out := make([]string, 0, len(lines))
	empty := false
	for _, line := range lines {
		line = strings.TrimSpace(strings.Map(func(r rune) rune {
			if r == '\t' {
				return ' '
			}
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, line))
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			if !empty && len(out) > 0 {
				out = append(out, "")
			}
			empty = true
			continue
		}
		empty = false
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

var telegramQuoteBoundaryRe = regexp.MustCompile(`(?i)^(?:-{2,}\s*(?:original message|原始邮件)\s*-*|on .+ wrote:|发件人[：:]|from[：:].+|_{5,})$`)
var telegramHTMLTagRe = regexp.MustCompile(`(?i)</?(?:div|p|table|tr|td|br|span|a|img)(?:\s[^>]*)?>`)

func stripTelegramQuotedContent(value string) string {
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if i > 0 && (trimmed == "--" || telegramQuoteBoundaryRe.MatchString(trimmed)) {
			lines = lines[:i]
			break
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func sanitizeTelegramAttachmentName(value string) string {
	value = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(value, "�")))
	value = strings.Join(strings.Fields(value), " ")
	value, truncated := truncateRunes(value, 100)
	if truncated {
		value += "..."
	}
	return value
}

var (
	telegramOTPKeywordRe   = regexp.MustCompile(`(?i)(验证码|校验码|动态码|登录码|安全码|一次性密码|otp|verification[ -]?code|security[ -]?code|login[ -]?code|passcode|one[ -]?time[ -]?(?:password|code))`)
	telegramOTPCandidateRe = regexp.MustCompile(`(?i)[a-z0-9]{4,10}`)
	telegramEmailRe        = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)
	telegramURLRe          = regexp.MustCompile(`(?i)https?://[^\s<>"']+`)
)

func detectTelegramOTP(subject, body string) string {
	text := normalizeTelegramText(strings.TrimSpace(subject) + "\n" + body)
	keywords := telegramOTPKeywordRe.FindAllStringIndex(text, -1)
	if len(keywords) == 0 {
		return ""
	}
	type candidateScore struct {
		value string
		score int
		count int
	}
	scores := map[string]candidateScore{}
	subjectEnd := len(strings.TrimSpace(subject))
	excludedRanges := append(telegramEmailRe.FindAllStringIndex(text, -1), telegramURLRe.FindAllStringIndex(text, -1)...)
	for _, match := range telegramOTPCandidateRe.FindAllStringIndex(text, -1) {
		if telegramRangeOverlaps(match, excludedRanges) {
			continue
		}
		if match[0] > 0 && isTelegramOTPAlphaNumeric(rune(text[match[0]-1])) {
			continue
		}
		if match[1] < len(text) && isTelegramOTPAlphaNumeric(rune(text[match[1]])) {
			continue
		}
		value := strings.ToUpper(text[match[0]:match[1]])
		hasDigit := false
		for _, r := range value {
			if unicode.IsDigit(r) {
				hasDigit = true
				break
			}
		}
		if !hasDigit || telegramOTPKeywordRe.MatchString(value) {
			continue
		}
		if isTelegramOTPNonCode(value) {
			continue
		}
		best := 0
		for _, keyword := range keywords {
			distance := match[0] - keyword[1]
			if distance < 0 {
				distance = keyword[0] - match[1]
			}
			if distance < 0 {
				distance = 0
			}
			score := 0
			switch {
			case distance <= 16:
				score = 100
			case distance <= 48:
				score = 80
			case distance <= 100:
				score = 55
			}
			if match[0] <= subjectEnd {
				score += 15
			}
			if score > best {
				best = score
			}
		}
		if best == 0 {
			continue
		}
		current := scores[value]
		current.value = value
		current.count++
		if best > current.score {
			current.score = best
		}
		scores[value] = current
	}
	items := make([]candidateScore, 0, len(scores))
	for _, item := range scores {
		item.score += min(item.count-1, 2) * 5
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].score > items[j].score })
	if len(items) == 0 || items[0].score < 55 {
		return ""
	}
	if len(items) > 1 && items[1].score >= items[0].score-25 {
		return ""
	}
	return items[0].value
}

func telegramRangeOverlaps(candidate []int, ranges [][]int) bool {
	for _, item := range ranges {
		if len(item) == 2 && candidate[0] < item[1] && candidate[1] > item[0] {
			return true
		}
	}
	return false
}

func isTelegramOTPNonCode(value string) bool {
	if len(value) == 4 {
		if year, err := strconv.Atoi(value); err == nil && year >= 1900 && year <= 2099 {
			return true
		}
	}
	if len(value) == 8 {
		if _, err := time.Parse("20060102", value); err == nil {
			return true
		}
	}
	return false
}

func isTelegramOTPAlphaNumeric(r rune) bool {
	return r <= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r))
}

func (a *App) shouldNotifyTelegramMessage(ctx context.Context, messageID string) bool {
	var folder string
	if err := a.db.QueryRowContext(ctx, `SELECT lower(COALESCE(NULLIF(f.role,''),f.name,'')) FROM messages m LEFT JOIN folders f ON f.id=m.folder_id WHERE m.id=?`, messageID).Scan(&folder); err != nil {
		return false
	}
	switch strings.TrimSpace(folder) {
	case "spam", "junk", "trash", "deleted":
		return false
	default:
		return true
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
	a.telegramDeliveryMu.Lock()
	defer a.telegramDeliveryMu.Unlock()
	_, _ = a.db.ExecContext(ctx, `DELETE FROM telegram_mail_outbox WHERE updated_at<? AND (delivered_at IS NOT NULL OR attempt_count>=?)`, a.now().UTC().Add(-30*24*time.Hour).Format(time.RFC3339Nano), telegramMailMaxAttempts)
	cfg := a.config()
	if !cfg.TelegramMailEnabled || strings.TrimSpace(cfg.TelegramBotToken) == "" || !validTelegramPrivateChatID(cfg.TelegramPrivateChatID) {
		return nil
	}
	nowText := a.now().UTC().Format(time.RFC3339Nano)
	rows, err := a.db.QueryContext(ctx, `SELECT id,payload_json,attempt_count FROM telegram_mail_outbox WHERE delivered_at IS NULL AND attempt_count<? AND next_attempt_at<=? AND (lease_until='' OR lease_until<=?) ORDER BY next_attempt_at,created_at LIMIT 20`, telegramMailMaxAttempts, nowText, nowText)
	if err != nil {
		return err
	}
	type queueItem struct {
		id      string
		payload telegramMailPayload
		attempt int
		invalid bool
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
			item.invalid = true
		}
		items = append(items, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range items {
		if item.invalid {
			now := a.now().UTC().Format(time.RFC3339Nano)
			if _, err := a.db.ExecContext(ctx, `UPDATE telegram_mail_outbox SET attempt_count=?,last_error='通知数据损坏',updated_at=?,lease_until='',payload_json='{}' WHERE id=?`, telegramMailMaxAttempts, now, item.id); err != nil {
				return err
			}
			continue
		}
		now := a.now().UTC()
		leaseUntil := now.Add(2 * time.Minute).Format(time.RFC3339Nano)
		result, err := a.db.ExecContext(ctx, `UPDATE telegram_mail_outbox SET lease_until=?,updated_at=? WHERE id=? AND delivered_at IS NULL AND (lease_until='' OR lease_until<=?)`, leaseUntil, now.Format(time.RFC3339Nano), item.id, now.Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			continue
		}
		formatted := formatTelegramMailMessage(item.payload)
		telegramMessageID, err := a.deliverTelegramMailMessage(ctx, cfg.TelegramBotToken, cfg.TelegramPrivateChatID, formatted)
		now = a.now().UTC()
		if err != nil {
			attempts := item.attempt + 1
			delay := sendRetryDelay(attempts)
			var apiErr *telegramAPIError
			if errors.As(err, &apiErr) {
				if apiErr.RetryAfter > 0 {
					delay = apiErr.RetryAfter
				}
				code := apiErr.ErrorCode
				if code == 0 {
					code = apiErr.HTTPStatus
				}
				if code == http.StatusUnauthorized || code == http.StatusForbidden || (code >= 400 && code < 500 && code != http.StatusTooManyRequests) {
					attempts = telegramMailMaxAttempts
				}
			}
			next := now.Add(delay)
			if _, updateErr := a.db.ExecContext(ctx, `UPDATE telegram_mail_outbox SET attempt_count=?,next_attempt_at=?,last_error=?,updated_at=?,lease_until='',payload_json=CASE WHEN ?>=? THEN '{}' ELSE payload_json END WHERE id=? AND delivered_at IS NULL`, attempts, next.Format(time.RFC3339Nano), truncateWebhookError(err.Error()), now.Format(time.RFC3339Nano), attempts, telegramMailMaxAttempts, item.id); updateErr != nil {
				return updateErr
			}
			continue
		}
		stamp := now.Format(time.RFC3339Nano)
		if _, err := a.db.ExecContext(ctx, `UPDATE telegram_mail_outbox SET attempt_count=attempt_count+1,last_error='',updated_at=?,delivered_at=?,lease_until='',telegram_message_id=?,payload_json='{}' WHERE id=? AND delivered_at IS NULL`, stamp, stamp, telegramMessageID, item.id); err != nil {
			return err
		}
	}
	return nil
}

func formatTelegramMailMessage(payload telegramMailPayload) telegramFormattedMessage {
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
	subject, _ = truncateRunes(subject, 180)
	from, _ = truncateRunes(from, 220)
	recipient, _ := truncateRunes(strings.TrimSpace(payload.Recipient), 160)
	lines := []string{
		"📩 <b>新邮件通知</b>",
		"",
		"<b>主题：</b>" + escapeTelegramWithinBudget(subject, 420),
		"<b>发件人：</b>" + escapeTelegramWithinBudget(from, 500),
		"<b>收件邮箱：</b><code>" + escapeTelegramWithinBudget(recipient, 320) + "</code>",
		"<b>收件时间：</b>" + html.EscapeString(timeText),
	}
	if payload.OTP != "" {
		lines = append(lines, "", "🔐 <b>验证码</b>", "<code>"+html.EscapeString(payload.OTP)+"</code>")
	}
	if len(payload.AttachmentNames) > 0 {
		names := make([]string, 0, len(payload.AttachmentNames))
		for _, name := range payload.AttachmentNames {
			names = append(names, escapeTelegramWithinBudget(name, 180))
		}
		attachmentText := strings.Join(names, "、")
		if payload.AttachmentCount > len(payload.AttachmentNames) {
			attachmentText += fmt.Sprintf("，其余 %d 个未显示", payload.AttachmentCount-len(payload.AttachmentNames))
		}
		lines = append(lines, "", fmt.Sprintf("📎 <b>附件：%d 个</b>", max(payload.AttachmentCount, len(payload.AttachmentNames))), attachmentText)
	}
	body := strings.TrimSpace(payload.Body)
	if body != "" {
		label := "正文摘要"
		if normalizeTelegramBodyMode(payload.BodyMode) == "full" {
			label = "邮件正文"
		}
		prefix := strings.Join(lines, "\n") + "\n\n<b>" + label + "</b>\n<blockquote>"
		suffix := "</blockquote>"
		body = formatTelegramBodyHTML(body, telegramMessageBudget-utf8.RuneCountInString(prefix)-utf8.RuneCountInString(suffix))
		lines = []string{prefix + body + suffix}
	}
	htmlText := strings.Join(lines, "\n")
	plain := formatTelegramMailPlainText(payload)
	return telegramFormattedMessage{HTML: htmlText, PlainText: plain, OTP: payload.OTP}
}

func formatTelegramBodyHTML(value string, budget int) string {
	if budget <= 3 {
		return ""
	}
	var out strings.Builder
	used := 0
	truncated := false
	appendEscaped := func(text string) bool {
		for _, r := range text {
			escaped := html.EscapeString(string(r))
			length := utf8.RuneCountInString(escaped)
			if used+length > budget-3 {
				return false
			}
			out.WriteString(escaped)
			used += length
		}
		return true
	}
	last := 0
	for _, match := range telegramURLRe.FindAllStringIndex(value, -1) {
		if !appendEscaped(value[last:match[0]]) {
			truncated = true
			break
		}
		rawURL, trailing := trimTelegramURL(value[match[0]:match[1]])
		parsed, err := url.Parse(rawURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			if !appendEscaped(value[match[0]:match[1]]) {
				truncated = true
				break
			}
			last = match[1]
			continue
		}
		display := rawURL
		if utf8.RuneCountInString(display) > 72 {
			display = "🔗 " + parsed.Hostname() + " 链接"
		}
		anchor := `<a href="` + html.EscapeString(rawURL) + `">` + html.EscapeString(display) + `</a>`
		length := utf8.RuneCountInString(anchor)
		if used+length > budget-3 {
			truncated = true
			break
		}
		out.WriteString(anchor)
		used += length
		if !appendEscaped(trailing) {
			truncated = true
			break
		}
		last = match[1]
	}
	if !truncated && last < len(value) && !appendEscaped(value[last:]) {
		truncated = true
	}
	if truncated {
		out.WriteString("...")
	}
	return out.String()
}

func trimTelegramURL(value string) (string, string) {
	trimmed := strings.TrimRight(value, ".,;:!?)]}，。；：！？）》】")
	return trimmed, value[len(trimmed):]
}

func (a *App) sendTelegramMessage(ctx context.Context, token, chatID, text string) error {
	_, err := a.sendTelegramPayload(ctx, token, map[string]any{
		"chat_id":                  chatID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	})
	return err
}

func (a *App) deliverTelegramMailMessage(ctx context.Context, token, chatID string, message telegramFormattedMessage) (int64, error) {
	payload := map[string]any{
		"chat_id":                  chatID,
		"text":                     message.HTML,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	if markup := telegramCopyMarkup(message.OTP); markup != nil {
		payload["reply_markup"] = markup
	}
	result, err := a.sendTelegramPayload(ctx, token, payload)
	if err == nil {
		return result.MessageID, nil
	}
	var apiErr *telegramAPIError
	if !errors.As(err, &apiErr) || apiErr.ErrorCode != http.StatusBadRequest {
		return 0, err
	}
	fallback := map[string]any{
		"chat_id":                  chatID,
		"text":                     message.PlainText,
		"disable_web_page_preview": true,
	}
	if markup := telegramCopyMarkup(message.OTP); markup != nil {
		fallback["reply_markup"] = markup
	}
	result, err = a.sendTelegramPayload(ctx, token, fallback)
	if err != nil {
		return 0, err
	}
	return result.MessageID, nil
}

func (a *App) sendTelegramPayload(ctx context.Context, token string, payload map[string]any) (telegramSentMessage, error) {
	var result telegramSentMessage
	err := a.callTelegram(ctx, token, "sendMessage", payload, &result)
	return result, err
}

func telegramCopyMarkup(otp string) map[string]any {
	otp = strings.TrimSpace(otp)
	if otp == "" || utf8.RuneCountInString(otp) > 256 {
		return nil
	}
	return map[string]any{"inline_keyboard": [][]map[string]any{{{
		"text":      "复制验证码",
		"copy_text": map[string]string{"text": otp},
	}}}}
}

func escapeTelegramWithinBudget(value string, budget int) string {
	if budget <= 3 {
		return ""
	}
	var out strings.Builder
	used := 0
	truncated := false
	for _, r := range value {
		escaped := html.EscapeString(string(r))
		length := utf8.RuneCountInString(escaped)
		if used+length > budget-3 {
			truncated = true
			break
		}
		out.WriteString(escaped)
		used += length
	}
	if truncated {
		out.WriteString("...")
	}
	return out.String()
}

func formatTelegramMailPlainText(payload telegramMailPayload) string {
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
	parts := []string{"新邮件通知", "", "主题：" + subject, "发件人：" + from, "收件邮箱：" + payload.Recipient, "收件时间：" + timeText}
	if payload.OTP != "" {
		parts = append(parts, "", "验证码", payload.OTP)
	}
	if payload.AttachmentCount > 0 {
		parts = append(parts, "", fmt.Sprintf("附件：%d 个", payload.AttachmentCount))
	}
	if body := strings.TrimSpace(payload.Body); body != "" {
		parts = append(parts, "", "正文摘要", body)
	}
	text := normalizeTelegramText(strings.Join(parts, "\n"))
	text, truncated := truncateRunes(text, telegramMessageBudget-3)
	if truncated {
		text += "..."
	}
	return text
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
		return &telegramAPIError{HTTPStatus: resp.StatusCode, ErrorCode: apiResponse.ErrorCode, Description: description, RetryAfter: time.Duration(apiResponse.Parameters.RetryAfter) * time.Second}
	}
	if result != nil && len(apiResponse.Result) > 0 {
		if err := json.Unmarshal(apiResponse.Result, result); err != nil {
			return err
		}
	}
	return nil
}
