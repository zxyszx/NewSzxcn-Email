package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

const forwardingVerificationTTL = 24 * time.Hour

type ForwardingVerifiedEmail struct {
	ID                    string     `json:"id"`
	Email                 string     `json:"email"`
	Verified              bool       `json:"verified"`
	CreatedAt             time.Time  `json:"createdAt"`
	VerifiedAt            *time.Time `json:"verifiedAt,omitempty"`
	VerificationSentAt    *time.Time `json:"verificationSentAt,omitempty"`
	VerificationExpiresAt *time.Time `json:"verificationExpiresAt,omitempty"`
	DeliveryStatus        string     `json:"deliveryStatus,omitempty"`
	DeliveryError         string     `json:"deliveryError,omitempty"`
}

type MailboxForwardingRule struct {
	MailboxID    string   `json:"mailboxId"`
	TargetEmail  string   `json:"targetEmail"`
	TargetEmails []string `json:"targetEmails"`
}

type ForwardingSettings struct {
	VerifiedEmails      []ForwardingVerifiedEmail `json:"verifiedEmails"`
	AccountTargetEmail  string                    `json:"accountTargetEmail"`
	AccountTargetEmails []string                  `json:"accountTargetEmails"`
	MailboxRules        []MailboxForwardingRule   `json:"mailboxRules"`
}

func (a *App) handleForwardingSettings(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	settings, err := a.forwardingSettings(r.Context(), user.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load forwarding settings")
		return
	}
	respondJSON(w, http.StatusOK, settings)
}

func (a *App) handleAddForwardingVerifiedEmail(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	var req struct {
		Email string `json:"email"`
	}
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	email, ok := a.cleanForwardingVerificationEmail(w, r, user.ID, req.Email)
	if !ok {
		return
	}
	id, verified, err := a.forwardingVerifiedEmailState(r.Context(), user.ID, email)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		respondError(w, http.StatusInternalServerError, "failed to load verified email")
		return
	}
	if verified {
		settings, err := a.forwardingSettings(r.Context(), user.ID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to load forwarding settings")
			return
		}
		respondJSON(w, http.StatusOK, settings)
		return
	}
	if id == "" {
		id = newID("fwd")
	}
	if err := a.issueForwardingVerification(r.Context(), user.ID, id, email, errors.Is(err, sql.ErrNoRows)); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save verified email")
		return
	}
	settings, err := a.forwardingSettings(r.Context(), user.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load forwarding settings")
		return
	}
	respondJSON(w, http.StatusCreated, settings)
}

func (a *App) handleResendForwardingVerifiedEmail(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		respondError(w, http.StatusNotFound, "verified email not found")
		return
	}
	var email string
	var verified int
	err := a.db.QueryRowContext(r.Context(), `SELECT email,verified FROM forwarding_verified_emails WHERE id=? AND user_id=?`, id, user.ID).Scan(&email, &verified)
	if errors.Is(err, sql.ErrNoRows) {
		respondError(w, http.StatusNotFound, "verified email not found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load verified email")
		return
	}
	if intBool(verified) {
		settings, err := a.forwardingSettings(r.Context(), user.ID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to load forwarding settings")
			return
		}
		respondJSON(w, http.StatusOK, settings)
		return
	}
	if err := a.issueForwardingVerification(r.Context(), user.ID, id, email, false); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to resend verification email")
		return
	}
	settings, err := a.forwardingSettings(r.Context(), user.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load forwarding settings")
		return
	}
	respondJSON(w, http.StatusOK, settings)
}

func (a *App) handleVerifyForwardingEmail(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		a.renderForwardingVerificationPage(w, http.StatusBadRequest, false, "", "验证链接无效")
		return
	}
	var id, email string
	var verified int
	var expiresRaw sql.NullString
	err := a.db.QueryRowContext(r.Context(), `SELECT id,email,verified,verification_expires_at FROM forwarding_verified_emails WHERE verification_token_hash=?`, hashToken(token)).Scan(&id, &email, &verified, &expiresRaw)
	if errors.Is(err, sql.ErrNoRows) {
		a.renderForwardingVerificationPage(w, http.StatusBadRequest, false, "", "验证链接无效或已使用")
		return
	}
	if err != nil {
		a.renderForwardingVerificationPage(w, http.StatusInternalServerError, false, "", "验证失败，请稍后重试")
		return
	}
	if intBool(verified) {
		a.renderForwardingVerificationPage(w, http.StatusOK, true, email, "该邮箱已经验证完成")
		return
	}
	if expiresRaw.Valid && expiresRaw.String != "" && parseTime(expiresRaw.String).Before(a.now().UTC()) {
		a.renderForwardingVerificationPage(w, http.StatusBadRequest, false, email, "验证链接已过期，请回到设置页重新发送")
		return
	}
	now := a.now().UTC().Format(time.RFC3339Nano)
	_, err = a.db.ExecContext(r.Context(), `UPDATE forwarding_verified_emails
		SET verified=1,verified_at=?,delivery_status='verified',delivery_error='',updated_at=?
		WHERE id=?`, now, now, id)
	if err != nil {
		a.renderForwardingVerificationPage(w, http.StatusInternalServerError, false, email, "验证失败，请稍后重试")
		return
	}
	a.renderForwardingVerificationPage(w, http.StatusOK, true, email, "验证完成，可以回到设置页选择此转发目标")
}

func (a *App) handleDeleteForwardingVerifiedEmail(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		respondError(w, http.StatusNotFound, "verified email not found")
		return
	}
	var email string
	if err := a.db.QueryRowContext(r.Context(), `SELECT email FROM forwarding_verified_emails WHERE id=? AND user_id=?`, id, user.ID).Scan(&email); err != nil {
		respondError(w, http.StatusNotFound, "verified email not found")
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback()
	now := a.now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM forwarding_verified_emails WHERE id=? AND user_id=?`, id, user.ID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete verified email")
		return
	}
	if err := a.removeForwardingTargetFromSettings(r.Context(), tx, user.ID, email, now); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update mailbox forwarding")
		return
	}
	if err := tx.Commit(); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save forwarding settings")
		return
	}
	settings, err := a.forwardingSettings(r.Context(), user.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load forwarding settings")
		return
	}
	respondJSON(w, http.StatusOK, settings)
}

func (a *App) handleUpdateAccountForwarding(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	var req struct {
		TargetEmail  string   `json:"targetEmail"`
		TargetEmails []string `json:"targetEmails"`
	}
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	targets, err := a.cleanForwardingTargets(r.Context(), user.ID, forwardingTargetsFromRequest(req.TargetEmail, req.TargetEmails))
	if err != nil {
		badRequest(w, err)
		return
	}
	target := firstForwardingTarget(targets)
	now := a.now().UTC().Format(time.RFC3339Nano)
	_, err = a.db.ExecContext(r.Context(), `INSERT INTO account_forwarding_settings(user_id,target_email,target_emails,updated_at)
		VALUES(?,?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET target_email=excluded.target_email,target_emails=excluded.target_emails,updated_at=excluded.updated_at`,
		user.ID, target, jsonEncode(targets), now)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save account forwarding")
		return
	}
	settings, err := a.forwardingSettings(r.Context(), user.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load forwarding settings")
		return
	}
	respondJSON(w, http.StatusOK, settings)
}

func (a *App) handleUpdateMailboxForwarding(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	mailboxID := strings.TrimSpace(chi.URLParam(r, "id"))
	if mailboxID == "" {
		respondError(w, http.StatusNotFound, "mailbox not found")
		return
	}
	if ok, err := a.userOwnsMailboxID(r.Context(), user.ID, mailboxID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to check mailbox")
		return
	} else if !ok {
		respondError(w, http.StatusNotFound, "mailbox not found")
		return
	}
	var req struct {
		TargetEmail  string   `json:"targetEmail"`
		TargetEmails []string `json:"targetEmails"`
	}
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	targets, err := a.cleanForwardingTargets(r.Context(), user.ID, forwardingTargetsFromRequest(req.TargetEmail, req.TargetEmails))
	if err != nil {
		badRequest(w, err)
		return
	}
	target := firstForwardingTarget(targets)
	if len(targets) == 0 {
		if _, err := a.db.ExecContext(r.Context(), `DELETE FROM mailbox_forwarding_settings WHERE mailbox_id=?`, mailboxID); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to save mailbox forwarding")
			return
		}
	} else {
		now := a.now().UTC().Format(time.RFC3339Nano)
		if _, err := a.db.ExecContext(r.Context(), `INSERT INTO mailbox_forwarding_settings(mailbox_id,target_email,target_emails,updated_at)
			VALUES(?,?,?,?)
			ON CONFLICT(mailbox_id) DO UPDATE SET target_email=excluded.target_email,target_emails=excluded.target_emails,updated_at=excluded.updated_at`,
			mailboxID, target, jsonEncode(targets), now); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to save mailbox forwarding")
			return
		}
	}
	settings, err := a.forwardingSettings(r.Context(), user.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load forwarding settings")
		return
	}
	respondJSON(w, http.StatusOK, settings)
}

func (a *App) forwardingSettings(ctx context.Context, userID string) (ForwardingSettings, error) {
	settings := ForwardingSettings{
		VerifiedEmails: []ForwardingVerifiedEmail{},
		MailboxRules:   []MailboxForwardingRule{},
	}
	rows, err := a.db.QueryContext(ctx, `SELECT fve.id,fve.email,fve.verified,fve.created_at,
			fve.verified_at,fve.verification_sent_at,fve.verification_expires_at,
			COALESCE(NULLIF(sq.status,''), fve.delivery_status),
			COALESCE(NULLIF(sq.last_error,''), fve.delivery_error)
		FROM forwarding_verified_emails fve
		LEFT JOIN send_queue sq ON sq.id=fve.delivery_queue_id
		WHERE fve.user_id=?
		ORDER BY fve.created_at DESC,fve.email`, userID)
	if err != nil {
		return settings, err
	}
	defer rows.Close()
	for rows.Next() {
		var item ForwardingVerifiedEmail
		var verified int
		var created string
		var verifiedAt, sentAt, expiresAt sql.NullString
		if err := rows.Scan(&item.ID, &item.Email, &verified, &created, &verifiedAt, &sentAt, &expiresAt, &item.DeliveryStatus, &item.DeliveryError); err != nil {
			return settings, err
		}
		item.Verified = intBool(verified)
		item.CreatedAt = parseTime(created)
		item.VerifiedAt = nullableTime(verifiedAt)
		item.VerificationSentAt = nullableTime(sentAt)
		item.VerificationExpiresAt = nullableTime(expiresAt)
		settings.VerifiedEmails = append(settings.VerifiedEmails, item)
	}
	if err := rows.Err(); err != nil {
		return settings, err
	}
	var accountTarget, accountTargetsJSON string
	err = a.db.QueryRowContext(ctx, `SELECT target_email,target_emails FROM account_forwarding_settings WHERE user_id=?`, userID).Scan(&accountTarget, &accountTargetsJSON)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return settings, err
	}
	settings.AccountTargetEmails = forwardingTargetsFromStored(accountTarget, accountTargetsJSON)
	settings.AccountTargetEmail = firstForwardingTarget(settings.AccountTargetEmails)
	rows, err = a.db.QueryContext(ctx, `SELECT mfs.mailbox_id,mfs.target_email,mfs.target_emails
		FROM mailbox_forwarding_settings mfs
		JOIN mailboxes mb ON mb.id=mfs.mailbox_id
		WHERE mb.user_id=? AND (mfs.target_email<>'' OR mfs.target_emails<>'[]')
		ORDER BY mb.address`, userID)
	if err != nil {
		return settings, err
	}
	defer rows.Close()
	for rows.Next() {
		var item MailboxForwardingRule
		var target, targetsJSON string
		if err := rows.Scan(&item.MailboxID, &target, &targetsJSON); err != nil {
			return settings, err
		}
		item.TargetEmails = forwardingTargetsFromStored(target, targetsJSON)
		item.TargetEmail = firstForwardingTarget(item.TargetEmails)
		if len(item.TargetEmails) > 0 {
			settings.MailboxRules = append(settings.MailboxRules, item)
		}
	}
	return settings, rows.Err()
}

func (a *App) issueForwardingVerification(ctx context.Context, userID, id, email string, insert bool) error {
	now := a.now().UTC()
	expires := now.Add(forwardingVerificationTTL)
	token := randomToken()
	nowRaw := now.Format(time.RFC3339Nano)
	expiresRaw := expires.Format(time.RFC3339Nano)
	if insert {
		if _, err := a.db.ExecContext(ctx, `INSERT INTO forwarding_verified_emails(id,user_id,email,verified,verified_at,verification_token_hash,verification_sent_at,verification_expires_at,delivery_queue_id,delivery_status,delivery_error,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			id, userID, email, 0, nil, hashToken(token), nowRaw, expiresRaw, "", sendQueueStatusQueued, "", nowRaw, nowRaw); err != nil {
			return err
		}
	} else {
		if _, err := a.db.ExecContext(ctx, `UPDATE forwarding_verified_emails
			SET verified=0,verified_at=NULL,verification_token_hash=?,verification_sent_at=?,verification_expires_at=?,delivery_queue_id='',delivery_status=?,delivery_error='',updated_at=?
			WHERE id=? AND user_id=?`,
			hashToken(token), nowRaw, expiresRaw, sendQueueStatusQueued, nowRaw, id, userID); err != nil {
			return err
		}
	}
	queueID, err := a.sendForwardingVerificationEmail(ctx, userID, email, token, now)
	if err != nil {
		_, _ = a.db.ExecContext(ctx, `UPDATE forwarding_verified_emails SET delivery_status=?,delivery_error=?,updated_at=? WHERE id=? AND user_id=?`, sendQueueStatusFailed, err.Error(), nowRaw, id, userID)
		return nil
	}
	if queueID != "" {
		_, _ = a.db.ExecContext(ctx, `UPDATE forwarding_verified_emails SET delivery_queue_id=?,delivery_status=?,delivery_error='',updated_at=? WHERE id=? AND user_id=?`, queueID, sendQueueStatusQueued, nowRaw, id, userID)
	}
	return nil
}

func (a *App) sendForwardingVerificationEmail(ctx context.Context, userID, targetEmail, token string, now time.Time) (string, error) {
	if strings.TrimSpace(a.config().SMTPHost) == "" {
		return "", errors.New("SMTP 未配置，无法发送验证邮件")
	}
	mb, err := a.primaryMailboxForUser(ctx, userID)
	if err != nil {
		return "", err
	}
	fromDomain := domainPart(mb.Address)
	from := "noreply@" + fromDomain
	link := a.forwardingVerificationURL(token)
	text := "邮箱转发验证\n\n您正在将此邮箱添加为邮件转发目标地址。请打开以下链接完成验证：\n" + link + "\n\n此链接 24 小时内有效。如果您没有发起此操作，请忽略此邮件。"
	html := `<div style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Arial,sans-serif;color:#111827;line-height:1.6;padding:32px 24px">
<div style="max-width:640px;margin:0 auto">
<h1 style="font-size:28px;line-height:1.25;margin:0 0 28px;font-weight:700">邮箱转发验证</h1>
<p style="font-size:17px;margin:0 0 28px">您正在将此邮箱添加为邮件转发目标地址。请点击下方按钮完成验证：</p>
<p style="text-align:center;margin:0 0 34px"><a href="` + htmlEscape(link) + `" style="display:inline-block;background:#2563eb;color:#ffffff;text-decoration:none;border-radius:8px;padding:14px 38px;font-size:18px;font-weight:700">确认验证</a></p>
<p style="font-size:15px;color:#6b7280;margin:0 0 12px">如果按钮无法点击，请复制以下链接到浏览器：</p>
<p style="font-size:15px;color:#6b7280;word-break:break-all;margin:0 0 28px">` + htmlEscape(link) + `</p>
<p style="font-size:15px;color:#9ca3af;margin:0">此链接 24 小时内有效。如果您没有发起此操作，请忽略此邮件。</p>
</div></div>`
	messageID := fmt.Sprintf("<%s@%s>", newID("fwdverify"), fromDomain)
	mimeBytes, err := BuildMIME(MIMEMessage{From: from, FromName: "noreply", To: []string{targetEmail}, Subject: "邮箱转发验证", Text: text, HTML: html, MessageID: messageID, Date: now})
	if err != nil {
		return "", err
	}
	return a.enqueueSend(ctx, sendQueueInput{
		UserID:     userID,
		MailboxID:  mb.ID,
		MessageID:  messageID,
		Source:     sendSourceForwardingVerification,
		MailFrom:   from,
		HeaderFrom: from,
		Recipients: []string{targetEmail},
		MIMEBytes:  mimeBytes,
		Now:        now,
	})
}

func (a *App) forwardingVerificationURL(token string) string {
	base := strings.TrimRight(strings.TrimSpace(a.config().PublicBaseURL), "/")
	if base == "" {
		base = "https://" + strings.Trim(strings.TrimSpace(a.config().PublicHostname), "/")
	}
	return base + "/api/verify-email?token=" + url.QueryEscape(token)
}

func (a *App) renderForwardingVerificationPage(w http.ResponseWriter, status int, ok bool, email, message string) {
	title := "邮箱转发验证"
	heading := "验证失败"
	color := "#dc2626"
	if ok {
		heading = "验证完成"
		color = "#2563eb"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>%s</title></head><body style="margin:0;background:#f8fafc;color:#0f172a;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Arial,sans-serif"><main style="min-height:100vh;display:grid;place-items:center;padding:24px"><section style="width:min(100%%,520px);background:white;border:1px solid #e2e8f0;border-radius:14px;padding:34px 30px;box-shadow:0 18px 45px rgba(15,23,42,.08)"><h1 style="margin:0 0 14px;font-size:28px">%s</h1><p style="margin:0 0 10px;font-size:17px;color:#475569">%s</p><p style="margin:0 0 26px;font-size:15px;color:#64748b">%s</p><a href="/" style="display:inline-block;border-radius:8px;background:%s;color:white;text-decoration:none;padding:12px 18px;font-weight:700">返回邮箱</a></section></main></body></html>`,
		title, heading, htmlEscape(message), htmlEscape(email), color)
}

func (a *App) cleanForwardingVerificationEmail(w http.ResponseWriter, r *http.Request, userID, value string) (string, bool) {
	email := normalizeEmail(value)
	if email == "" || !strings.Contains(email, "@") {
		badRequest(w, errors.New("邮箱地址无效"))
		return "", false
	}
	if owns, err := a.userOwnsMailboxAddress(r.Context(), userID, email); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to check mailbox")
		return "", false
	} else if owns {
		badRequest(w, errors.New("不能把当前账号邮箱作为转发验证邮箱"))
		return "", false
	}
	return email, true
}

func (a *App) forwardingVerifiedEmailState(ctx context.Context, userID, email string) (id string, verified bool, err error) {
	var verifiedInt int
	err = a.db.QueryRowContext(ctx, `SELECT id,verified FROM forwarding_verified_emails WHERE user_id=? AND email=?`, userID, normalizeEmail(email)).Scan(&id, &verifiedInt)
	return id, intBool(verifiedInt), err
}

func (a *App) primaryMailboxForUser(ctx context.Context, userID string) (Mailbox, error) {
	var mb Mailbox
	var created string
	err := a.db.QueryRowContext(ctx, `SELECT id,user_id,domain_id,local_part,address,display_name,quota_mb,status,created_at
		FROM mailboxes WHERE user_id=? AND status='active' ORDER BY created_at,id LIMIT 1`, userID).
		Scan(&mb.ID, &mb.UserID, &mb.DomainID, &mb.LocalPart, &mb.Address, &mb.DisplayName, &mb.QuotaMB, &mb.Status, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return mb, errors.New("当前账号没有可用于发送验证邮件的邮箱")
	}
	mb.CreatedAt = parseTime(created)
	return mb, err
}

func (a *App) forwardingEmailVerified(ctx context.Context, userID, email string) (bool, error) {
	var count int
	err := a.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM forwarding_verified_emails WHERE user_id=? AND email=? AND verified=1`, userID, normalizeEmail(email)).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func forwardingTargetsFromRequest(targetEmail string, targetEmails []string) []string {
	if len(targetEmails) > 0 {
		return targetEmails
	}
	if strings.TrimSpace(targetEmail) == "" {
		return nil
	}
	return []string{targetEmail}
}

func (a *App) cleanForwardingTargets(ctx context.Context, userID string, values []string) ([]string, error) {
	targets := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || strings.EqualFold(value, "none") {
			continue
		}
		target := normalizeEmail(value)
		if target == "" || !strings.Contains(target, "@") {
			return nil, errors.New("转发邮箱无效")
		}
		if seen[target] {
			continue
		}
		ok, err := a.forwardingEmailVerified(ctx, userID, target)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errors.New("请先完成邮箱验证")
		}
		seen[target] = true
		targets = append(targets, target)
	}
	return targets, nil
}

func forwardingTargetsFromStored(targetEmail, targetsJSON string) []string {
	targets := dedupeEmails(jsonDecodeSlice(targetsJSON))
	if len(targets) > 0 {
		return targets
	}
	target := normalizeEmail(targetEmail)
	if target == "" {
		return nil
	}
	return []string{target}
}

func firstForwardingTarget(targets []string) string {
	if len(targets) == 0 {
		return ""
	}
	return targets[0]
}

func removeForwardingTarget(targets []string, email string) []string {
	email = normalizeEmail(email)
	next := make([]string, 0, len(targets))
	for _, target := range targets {
		if normalizeEmail(target) == email {
			continue
		}
		next = append(next, normalizeEmail(target))
	}
	return dedupeEmails(next)
}

func (a *App) removeForwardingTargetFromSettings(ctx context.Context, tx *sql.Tx, userID, email, now string) error {
	var accountTarget, accountTargetsJSON string
	if err := tx.QueryRowContext(ctx, `SELECT target_email,target_emails FROM account_forwarding_settings WHERE user_id=?`, userID).Scan(&accountTarget, &accountTargetsJSON); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	} else if err == nil {
		targets := removeForwardingTarget(forwardingTargetsFromStored(accountTarget, accountTargetsJSON), email)
		if _, err := tx.ExecContext(ctx, `UPDATE account_forwarding_settings SET target_email=?,target_emails=?,updated_at=? WHERE user_id=?`, firstForwardingTarget(targets), jsonEncode(targets), now, userID); err != nil {
			return err
		}
	}

	rows, err := tx.QueryContext(ctx, `SELECT mfs.mailbox_id,mfs.target_email,mfs.target_emails
		FROM mailbox_forwarding_settings mfs
		JOIN mailboxes mb ON mb.id=mfs.mailbox_id
		WHERE mb.user_id=?`, userID)
	if err != nil {
		return err
	}
	type mailboxRow struct {
		id          string
		target      string
		targetsJSON string
	}
	var items []mailboxRow
	for rows.Next() {
		var item mailboxRow
		if err := rows.Scan(&item.id, &item.target, &item.targetsJSON); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range items {
		targets := removeForwardingTarget(forwardingTargetsFromStored(item.target, item.targetsJSON), email)
		if len(targets) == 0 {
			if _, err := tx.ExecContext(ctx, `DELETE FROM mailbox_forwarding_settings WHERE mailbox_id=?`, item.id); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE mailbox_forwarding_settings SET target_email=?,target_emails=?,updated_at=? WHERE mailbox_id=?`, firstForwardingTarget(targets), jsonEncode(targets), now, item.id); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) userOwnsMailboxID(ctx context.Context, userID, mailboxID string) (bool, error) {
	var count int
	err := a.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM mailboxes WHERE id=? AND user_id=? AND status='active'`, mailboxID, userID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (a *App) userOwnsMailboxAddress(ctx context.Context, userID, address string) (bool, error) {
	var count int
	err := a.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM mailboxes WHERE user_id=? AND address=? AND status='active'`, userID, normalizeEmail(address)).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
