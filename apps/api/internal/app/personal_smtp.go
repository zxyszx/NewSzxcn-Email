package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	netmail "net/mail"
	"sort"
	"strings"
)

type personalSMTPRoute struct {
	Enabled            bool   `json:"enabled"`
	GmailAddress       string `json:"gmailAddress"`
	PasswordSet        bool   `json:"passwordSet"`
	Microsoft          bool   `json:"microsoft"`
	Yahoo              bool   `json:"yahoo"`
	PasswordCiphertext string `json:"-"`
}

func (a *App) personalSMTPRoute(ctx context.Context, userID string) (personalSMTPRoute, error) {
	var route personalSMTPRoute
	err := a.db.QueryRowContext(ctx, `SELECT enabled,gmail_address,password_ciphertext,microsoft,yahoo FROM personal_smtp_routes WHERE user_id=?`, userID).Scan(&route.Enabled, &route.GmailAddress, &route.PasswordCiphertext, &route.Microsoft, &route.Yahoo)
	if errors.Is(err, sql.ErrNoRows) {
		return route, nil
	}
	route.PasswordSet = route.PasswordCiphertext != ""
	return route, err
}
func (a *App) handlePersonalSMTPRoute(w http.ResponseWriter, r *http.Request) {
	route, err := a.personalSMTPRoute(r.Context(), currentUser(r).ID)
	if err != nil {
		respondError(w, 500, "无法读取发信路由")
		return
	}
	respondJSON(w, 200, route)
}
func (a *App) handleSavePersonalSMTPRoute(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled      bool   `json:"enabled"`
		GmailAddress string `json:"gmailAddress"`
		AppPassword  string `json:"appPassword"`
		Microsoft    bool   `json:"microsoft"`
		Yahoo        bool   `json:"yahoo"`
	}
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	route, err := a.personalSMTPRoute(r.Context(), currentUser(r).ID)
	if err != nil {
		respondError(w, 500, "无法读取发信路由")
		return
	}
	address := normalizeEmail(req.GmailAddress)
	parsed, parseErr := netmail.ParseAddress(address)
	if address != "" && (parseErr != nil || parsed.Address != address || domainPart(address) != "gmail.com") {
		badRequest(w, errors.New("请输入个人 Gmail 地址（@gmail.com）"))
		return
	}
	password := strings.ReplaceAll(strings.TrimSpace(req.AppPassword), " ", "")
	if password != "" {
		if len(password) != 16 || strings.ContainsAny(password, "\r\n\t") {
			badRequest(w, errors.New("请填写 16 位 Google 应用专用密码"))
			return
		}
		route.PasswordCiphertext, err = a.encryptExternalIMAPPassword(password)
		if err != nil {
			respondError(w, 400, "凭据加密未配置，请联系管理员")
			return
		}
	} else if route.GmailAddress != address {
		route.PasswordCiphertext = ""
	}
	if req.Enabled && (address == "" || route.PasswordCiphertext == "" || (!req.Microsoft && !req.Yahoo)) {
		badRequest(w, errors.New("启用前请填写 Gmail 和应用专用密码，并至少选择一个收件厂商"))
		return
	}
	_, err = a.db.ExecContext(r.Context(), `INSERT INTO personal_smtp_routes(user_id,enabled,gmail_address,password_ciphertext,microsoft,yahoo) VALUES(?,?,?,?,?,?) ON CONFLICT(user_id) DO UPDATE SET enabled=excluded.enabled,gmail_address=excluded.gmail_address,password_ciphertext=excluded.password_ciphertext,microsoft=excluded.microsoft,yahoo=excluded.yahoo`, currentUser(r).ID, boolInt(req.Enabled), address, route.PasswordCiphertext, boolInt(req.Microsoft), boolInt(req.Yahoo))
	if err != nil {
		respondError(w, 500, "无法保存发信路由")
		return
	}
	a.handlePersonalSMTPRoute(w, r)
}

// Exact public mailbox domains only: custom Microsoft 365 domains are not
// inferred from arbitrary suffixes or recipient-controlled hostnames.
func personalSMTPProvider(address string) string {
	switch domainPart(normalizeEmail(address)) {
	case "outlook.com", "hotmail.com", "live.com", "msn.com", "outlook.cn", "hotmail.co.uk", "hotmail.fr", "hotmail.de", "hotmail.it", "live.cn", "live.co.uk":
		return "microsoft"
	case "yahoo.com", "ymail.com", "rocketmail.com", "yahoo.co.uk", "yahoo.ca", "yahoo.com.au", "yahoo.fr", "yahoo.de", "yahoo.it", "yahoo.es", "yahoo.com.hk", "yahoo.com.tw", "yahoo.co.jp":
		return "yahoo"
	}
	return ""
}
func (route personalSMTPRoute) matches(address string) bool {
	provider := personalSMTPProvider(address)
	return route.Enabled && ((provider == "microsoft" && route.Microsoft) || (provider == "yahoo" && route.Yahoo))
}

// Use the actual Gmail identity for Gmail SMTP. Keep the original reply target,
// body and attachments; discard signatures invalidated by replacing From.
func personalGmailMIME(raw []byte, gmailAddress string) ([]byte, error) {
	msg, err := netmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, errors.New("无法解析中继邮件")
	}
	from, err := msg.Header.AddressList("From")
	if err != nil || len(from) != 1 {
		return nil, errors.New("中继邮件的发件人无效")
	}
	reply := msg.Header.Get("Reply-To")
	if reply == "" {
		reply = from[0].String()
	}
	if _, err = netmail.ParseAddressList(reply); err != nil {
		return nil, errors.New("中继邮件的回复地址无效")
	}
	for key := range msg.Header {
		lower := strings.ToLower(key)
		if lower == "from" || lower == "sender" || lower == "reply-to" || lower == "bcc" || lower == "return-path" || lower == "dkim-signature" || lower == "authentication-results" || strings.HasPrefix(lower, "arc-") {
			delete(msg.Header, key)
		}
	}
	msg.Header["From"] = []string{(&netmail.Address{Name: from[0].Address + " via NewSzxcn", Address: gmailAddress}).String()}
	msg.Header["Reply-To"] = []string{reply}
	var out bytes.Buffer
	keys := make([]string, 0, len(msg.Header))
	for key := range msg.Header {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		for _, value := range msg.Header[key] {
			if strings.ContainsAny(value, "\r\n") {
				return nil, errors.New("中继邮件头无效")
			}
			fmt.Fprintf(&out, "%s: %s\r\n", key, value)
		}
	}
	out.WriteString("\r\n")
	if _, err = io.Copy(&out, msg.Body); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func personalSubmissionMarker(queueID string, recipients []string) string {
	values := append([]string(nil), recipients...)
	sort.Strings(values)
	sum := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return queueID + ".rcpt." + hex.EncodeToString(sum[:])
}

// Each accepted recipient group has a durable marker, so a failed Gmail group
// cannot cause the already-submitted direct group to be resent on retry.
func (a *App) sendQueueWithPersonalRoute(ctx context.Context, item sendQueueItem, raw []byte) error {
	if handled, err := a.sendQueueWithSMTPPool(ctx, item, raw); handled {
		return err
	}

	// Successful recipients stay excluded even if routing settings change
	// while a failed item is waiting for its next attempt.
	remaining := make([]string, 0, len(item.Recipients))
	for _, rcpt := range item.Recipients {
		done, err := a.hasSendQueueDeliveredMarker(personalSubmissionMarker(item.ID, []string{rcpt}))
		if err != nil {
			return err
		}
		if !done {
			remaining = append(remaining, rcpt)
		}
	}
	if len(remaining) == 0 {
		return nil
	}
	item.Recipients = remaining
	route, err := a.personalSMTPRoute(ctx, item.UserID)
	if err != nil {
		return errors.New("无法读取个人发信路由")
	}
	if !route.Enabled {
		return a.sendSMTP(item.MailFrom, item.Recipients, raw)
	}
	var direct, relay []string
	for _, rcpt := range item.Recipients {
		if route.matches(rcpt) {
			relay = append(relay, rcpt)
		} else {
			direct = append(direct, rcpt)
		}
	}
	if len(relay) == 0 {
		return a.sendSMTP(item.MailFrom, item.Recipients, raw)
	}
	if !a.externalIMAPAllowedByID(ctx, item.UserID) {
		return errors.New("外部邮箱使用权限已关闭，无法使用个人 Gmail 发信")
	}
	password, err := a.decryptExternalIMAPPassword(route.PasswordCiphertext)
	if err != nil {
		return errors.New("无法解密个人 Gmail 凭据")
	}
	gmailMIME, err := personalGmailMIME(raw, route.GmailAddress)
	if err != nil {
		return err
	}
	cfg := a.config()
	cfg.SMTPHost = "smtp.gmail.com"
	cfg.SMTPPort = "465"
	cfg.SMTPUsername = route.GmailAddress
	cfg.SMTPPassword = password
	cfg.SMTPRequireTLS = true
	type group struct {
		recipients []string
		gmail      bool
	}
	groups := []group{}
	if len(direct) > 0 {
		groups = append(groups, group{direct, false})
	}
	// Send Gmail recipients separately to make SMTP RCPT rejection and retries
	// independent, including Bcc recipients (which never appear in MIME headers).
	for _, rcpt := range relay {
		groups = append(groups, group{[]string{rcpt}, true})
	}
	sender := sendSMTPWithConfig
	if a.personalSMTPSender != nil {
		sender = a.personalSMTPSender
	}
	var submissionErrors []error
	for _, g := range groups {
		var err error
		if g.gmail {
			err = sender(cfg, route.GmailAddress, g.recipients, gmailMIME)
		} else {
			err = sender(a.config(), item.MailFrom, g.recipients, raw)
		}
		if err != nil {
			// Upstream text may contain account details. Keep credentials and raw SMTP
			// responses out of user-visible errors and the audit log.
			if g.gmail {
				submissionErrors = append(submissionErrors, errors.New("Gmail 中继提交失败，请检查应用专用密码、配额和连接"))
			} else {
				submissionErrors = append(submissionErrors, err)
			}
			continue
		}
		for _, rcpt := range g.recipients {
			if err = a.writeSendQueueDeliveredMarker(personalSubmissionMarker(item.ID, []string{rcpt})); err != nil {
				return errors.New("邮件已提交，但提交标记保存失败，需要管理员检查")
			}
		}
	}
	return errors.Join(submissionErrors...)
}
