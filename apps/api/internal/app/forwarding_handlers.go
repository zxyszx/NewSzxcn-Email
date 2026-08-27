package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

const (
	forwardingVerificationTTL          = 24 * time.Hour
	forwardingVerificationUserCooldown = 60 * time.Second
	forwardingVerificationTargetLimit  = 5
	forwardingBindingScopeAccount      = "account"
	forwardingBindingScopeMailbox      = "mailbox"
	forwardingBindingPending           = "pending_verification"
	forwardingBindingActive            = "active"
	forwardingBindingFailed            = "activation_failed"
	forwardingBindingCancelled         = "cancelled"
	forwardingBindingExpired           = "expired"
)

var forwardingVerificationHTMLTemplate = template.Must(template.New("forwarding-verification").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>请确认您的邮箱转发设置</title>
</head>
<body style="margin:0;padding:0;background:#f4f6f8;color:#0f172a;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI','Microsoft YaHei',Arial,sans-serif;-webkit-text-size-adjust:100%">
  <div style="display:none;max-height:0;overflow:hidden;opacity:0">请在 24 小时内确认此邮箱转发地址。</div>
  <table role="presentation" width="100%" cellspacing="0" cellpadding="0" border="0" bgcolor="#f4f6f8" style="width:100%;background:#f4f6f8">
    <tr>
      <td align="center" style="padding:36px 16px">
        <table role="presentation" width="600" cellspacing="0" cellpadding="0" border="0" style="width:100%;max-width:600px;background:#ffffff;border:1px solid #e2e8f0;border-radius:10px;overflow:hidden">
          <tr>
            <td style="padding:18px 28px;border-bottom:1px solid #e2e8f0">
              <table role="presentation" width="100%" cellspacing="0" cellpadding="0" border="0">
                <tr>
                  <td style="vertical-align:middle">
                    <table role="presentation" cellspacing="0" cellpadding="0" border="0">
                      <tr>
                        <td width="36" height="36" align="center" bgcolor="#2563eb" style="width:36px;height:36px;border-radius:8px;color:#ffffff;font-size:17px;font-weight:700;line-height:36px">N</td>
                        <td style="padding-left:12px;font-size:15px;font-weight:700;color:#0f172a">NewSzxcn Email Service</td>
                      </tr>
                    </table>
                  </td>
                  <td align="right" style="font-size:12px;font-weight:600;color:#64748b">安全验证</td>
                </tr>
              </table>
            </td>
          </tr>
          <tr>
            <td style="padding:32px 28px 28px">
              <h1 style="margin:0 0 16px;font-size:24px;line-height:1.35;font-weight:700;color:#0f172a">请确认您的邮箱转发设置</h1>
              <p style="margin:0 0 24px;font-size:15px;line-height:1.75;color:#334155">您正在 NewSzxcn 邮箱中添加此地址作为邮件转发目标，请确认这是您本人的操作。</p>

              <table role="presentation" width="100%" cellspacing="0" cellpadding="0" border="0" bgcolor="#f8fafc" style="width:100%;background:#f8fafc;border:1px solid #e2e8f0;border-radius:8px">
                <tr>
                  <td style="padding:16px 18px 10px;font-size:13px;color:#64748b">转发地址</td>
                  <td align="right" style="padding:16px 18px 10px;font-size:14px;font-weight:600;color:#0f172a;word-break:break-all">{{.MaskedEmail}}</td>
                </tr>
                <tr>
                  <td style="padding:10px 18px 16px;border-top:1px solid #e8edf3;font-size:13px;color:#64748b">有效时间</td>
                  <td align="right" style="padding:10px 18px 16px;border-top:1px solid #e8edf3;font-size:14px;font-weight:600;color:#0f172a">24 小时</td>
                </tr>
              </table>

              <table role="presentation" width="100%" cellspacing="0" cellpadding="0" border="0" style="width:100%;margin:26px 0 24px">
                <tr>
                  <td align="center">
                    <table role="presentation" cellspacing="0" cellpadding="0" border="0">
                      <tr>
                        <td align="center" bgcolor="#2563eb" style="border-radius:7px;background:#2563eb">
                          <a href="{{.Link}}" style="display:inline-block;min-width:168px;padding:13px 24px;color:#ffffff;font-size:15px;font-weight:700;line-height:20px;text-align:center;text-decoration:none">确认转发地址</a>
                        </td>
                      </tr>
                    </table>
                  </td>
                </tr>
              </table>

              <p style="margin:0 0 10px;font-size:13px;line-height:1.6;color:#64748b">如果按钮无法点击，请复制下方完整链接到浏览器中打开：</p>
              <p style="margin:0 0 24px;padding:12px 14px;border-radius:6px;background:#f1f5f9;font-size:12px;line-height:1.6;color:#475569;word-break:break-all">{{.Link}}</p>

              <table role="presentation" width="100%" cellspacing="0" cellpadding="0" border="0" style="width:100%;border-left:3px solid #2563eb">
                <tr>
                  <td style="padding:3px 0 3px 14px;font-size:13px;line-height:1.65;color:#64748b">如果这不是您的操作，可以直接忽略此邮件。您的邮箱设置不会被更改。</td>
                </tr>
              </table>
            </td>
          </tr>
          <tr>
            <td align="center" style="padding:16px 28px;border-top:1px solid #e2e8f0;background:#f8fafc;font-size:12px;line-height:1.6;color:#94a3b8">此邮件由 NewSzxcn Email Service 自动发送，请勿直接回复。</td>
          </tr>
        </table>
      </td>
    </tr>
  </table>
</body>
</html>`))

type forwardingVerificationTemplateData struct {
	MaskedEmail string
	Link        string
}

type forwardingVerificationRateLimitError struct {
	message    string
	retryAfter time.Duration
}

func (e *forwardingVerificationRateLimitError) Error() string { return e.message }

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
	GlobalBinding         bool       `json:"globalBinding"`
	MailboxBindings       []string   `json:"mailboxBindings"`
}

type MailboxForwardingRule struct {
	MailboxID      string   `json:"mailboxId"`
	MailboxAddress string   `json:"mailboxAddress"`
	TargetEmail    string   `json:"targetEmail"`
	TargetEmails   []string `json:"targetEmails"`
}

type ForwardingMailboxTargetSummary struct {
	Email    string `json:"email"`
	Verified bool   `json:"verified"`
	Source   string `json:"source"`
}

type ForwardingMailboxSummary struct {
	MailboxID          string                           `json:"mailboxId"`
	MailboxAddress     string                           `json:"mailboxAddress"`
	IndependentTargets int                              `json:"independentTargets"`
	InheritedTargets   int                              `json:"inheritedTargets"`
	Enabled            bool                             `json:"enabled"`
	Targets            []ForwardingMailboxTargetSummary `json:"targets"`
}

type ForwardingPendingBinding struct {
	ID              string     `json:"id"`
	VerifiedEmailID string     `json:"verifiedEmailId"`
	Email           string     `json:"email"`
	Scope           string     `json:"scope"`
	MailboxID       string     `json:"mailboxId,omitempty"`
	MailboxAddress  string     `json:"mailboxAddress,omitempty"`
	Status          string     `json:"status"`
	FailureReason   string     `json:"failureReason,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	ActivatedAt     *time.Time `json:"activatedAt,omitempty"`
}

type ForwardingActivation struct {
	Scope       string `json:"scope"`
	SourceEmail string `json:"sourceEmail"`
	TargetEmail string `json:"targetEmail"`
}

type ForwardingSettings struct {
	VerifiedEmails      []ForwardingVerifiedEmail  `json:"verifiedEmails"`
	AccountTargetEmail  string                     `json:"accountTargetEmail"`
	AccountTargetEmails []string                   `json:"accountTargetEmails"`
	MailboxRules        []MailboxForwardingRule    `json:"mailboxRules"`
	PendingBindings     []ForwardingPendingBinding `json:"pendingBindings"`
	MailboxSummaries    []ForwardingMailboxSummary `json:"mailboxSummaries"`
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
		if respondForwardingVerificationRateLimit(w, err) {
			return
		}
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

func (a *App) handleCreateForwardingPendingBinding(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	var req struct {
		Email     string `json:"email"`
		Scope     string `json:"scope"`
		MailboxID string `json:"mailboxId"`
	}
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	email, ok := a.cleanForwardingVerificationEmail(w, r, user.ID, req.Email)
	if !ok {
		return
	}
	req.Scope = strings.TrimSpace(req.Scope)
	req.MailboxID = strings.TrimSpace(req.MailboxID)
	if req.Scope != forwardingBindingScopeAccount && req.Scope != forwardingBindingScopeMailbox {
		badRequest(w, errors.New("转发绑定范围无效"))
		return
	}
	if req.Scope == forwardingBindingScopeMailbox {
		owns, err := a.userOwnsMailboxID(r.Context(), user.ID, req.MailboxID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to check mailbox")
			return
		}
		if !owns {
			respondError(w, http.StatusNotFound, "mailbox not found")
			return
		}
	} else {
		req.MailboxID = ""
	}

	verifiedID, verified, stateErr := a.forwardingVerifiedEmailState(r.Context(), user.ID, email)
	if stateErr != nil && !errors.Is(stateErr, sql.ErrNoRows) {
		respondError(w, http.StatusInternalServerError, "failed to load verified email")
		return
	}
	if verified {
		binding := ForwardingPendingBinding{ID: newID("fbind"), VerifiedEmailID: verifiedID, Email: email, Scope: req.Scope, MailboxID: req.MailboxID}
		if err := a.activateVerifiedForwardingBinding(r.Context(), user.ID, binding); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to enable forwarding")
			return
		}
		settings, err := a.forwardingSettings(r.Context(), user.ID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to load forwarding settings")
			return
		}
		respondJSON(w, http.StatusOK, settings)
		return
	}
	insert := errors.Is(stateErr, sql.ErrNoRows)
	if verifiedID == "" {
		verifiedID = newID("fwd")
	}
	if err := a.issueForwardingVerification(r.Context(), user.ID, verifiedID, email, insert); err != nil {
		if respondForwardingVerificationRateLimit(w, err) {
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to send verification email")
		return
	}
	now := a.now().UTC().Format(time.RFC3339Nano)
	_, err := a.db.ExecContext(r.Context(), `INSERT INTO forwarding_pending_bindings(
		id,user_id,verified_email_id,target_email,scope,mailbox_id,status,failure_reason,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(user_id,verified_email_id,scope,mailbox_id) DO UPDATE SET
			target_email=excluded.target_email,status=excluded.status,failure_reason='',updated_at=excluded.updated_at,activated_at=NULL`,
		newID("fbind"), user.ID, verifiedID, email, req.Scope, req.MailboxID, forwardingBindingPending, "", now, now)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save pending forwarding binding")
		return
	}
	settings, err := a.forwardingSettings(r.Context(), user.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load forwarding settings")
		return
	}
	respondJSON(w, http.StatusCreated, settings)
}

func (a *App) handleDeleteForwardingPendingBinding(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	now := a.now().UTC().Format(time.RFC3339Nano)
	result, err := a.db.ExecContext(r.Context(), `UPDATE forwarding_pending_bindings
		SET status=?,failure_reason='',updated_at=? WHERE id=? AND user_id=? AND status IN (?,?)`,
		forwardingBindingCancelled, now, id, user.ID, forwardingBindingPending, forwardingBindingFailed)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to cancel pending forwarding binding")
		return
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		respondError(w, http.StatusNotFound, "pending forwarding binding not found")
		return
	}
	settings, err := a.forwardingSettings(r.Context(), user.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load forwarding settings")
		return
	}
	respondJSON(w, http.StatusOK, settings)
}

func (a *App) handleRetryForwardingPendingBinding(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	binding, err := a.forwardingPendingBinding(r.Context(), user.ID, id)
	if errors.Is(err, sql.ErrNoRows) {
		respondError(w, http.StatusNotFound, "pending forwarding binding not found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load pending forwarding binding")
		return
	}
	if binding.Status != forwardingBindingFailed {
		badRequest(w, errors.New("只有启用失败的绑定可以重试"))
		return
	}
	if err := a.activateVerifiedForwardingBinding(r.Context(), user.ID, binding); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to enable forwarding")
		return
	}
	settings, err := a.forwardingSettings(r.Context(), user.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load forwarding settings")
		return
	}
	respondJSON(w, http.StatusOK, settings)
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
		if respondForwardingVerificationRateLimit(w, err) {
			return
		}
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
		a.respondForwardingVerificationResult(w, r, http.StatusBadRequest, false, "", "验证链接无效", nil)
		return
	}
	var id, userID, email string
	var verified int
	var expiresRaw sql.NullString
	err := a.db.QueryRowContext(r.Context(), `SELECT id,user_id,email,verified,verification_expires_at FROM forwarding_verified_emails WHERE verification_token_hash=?`, hashToken(token)).Scan(&id, &userID, &email, &verified, &expiresRaw)
	if errors.Is(err, sql.ErrNoRows) {
		a.respondForwardingVerificationResult(w, r, http.StatusBadRequest, false, "", "验证链接无效或已使用", nil)
		return
	}
	if err != nil {
		a.respondForwardingVerificationResult(w, r, http.StatusInternalServerError, false, "", "验证失败，请稍后重试", nil)
		return
	}
	if intBool(verified) {
		a.respondForwardingVerificationResult(w, r, http.StatusOK, true, email, "该邮箱已经验证完成", nil)
		return
	}
	if expiresRaw.Valid && expiresRaw.String != "" && parseTime(expiresRaw.String).Before(a.now().UTC()) {
		now := a.now().UTC().Format(time.RFC3339Nano)
		_, _ = a.db.ExecContext(r.Context(), `UPDATE forwarding_pending_bindings SET status=?,updated_at=? WHERE verified_email_id=? AND status=?`, forwardingBindingExpired, now, id, forwardingBindingPending)
		a.respondForwardingVerificationResult(w, r, http.StatusBadRequest, false, email, "验证链接已过期，请回到设置页重新发送", nil)
		return
	}
	now := a.now().UTC().Format(time.RFC3339Nano)
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		a.respondForwardingVerificationResult(w, r, http.StatusInternalServerError, false, email, "验证失败，请稍后重试", nil)
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `UPDATE forwarding_verified_emails
		SET verified=1,verified_at=?,delivery_status='verified',delivery_error='',updated_at=?
		WHERE id=? AND verification_token_hash=? AND verified=0`, now, now, id, hashToken(token))
	if err != nil {
		a.respondForwardingVerificationResult(w, r, http.StatusInternalServerError, false, email, "验证失败，请稍后重试", nil)
		return
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		var currentVerified int
		if queryErr := tx.QueryRowContext(r.Context(), `SELECT verified FROM forwarding_verified_emails WHERE id=? AND verification_token_hash=?`, id, hashToken(token)).Scan(&currentVerified); queryErr == nil && intBool(currentVerified) {
			a.respondForwardingVerificationResult(w, r, http.StatusOK, true, email, "该邮箱已经验证完成", nil)
			return
		}
		a.respondForwardingVerificationResult(w, r, http.StatusBadRequest, false, email, "验证链接无效或已使用", nil)
		return
	}
	rows, err := tx.QueryContext(r.Context(), `SELECT fpb.id,fpb.verified_email_id,fpb.target_email,fpb.scope,fpb.mailbox_id,
		COALESCE(mb.address,''),fpb.status,fpb.failure_reason,fpb.created_at,fpb.updated_at,fpb.activated_at
		FROM forwarding_pending_bindings fpb LEFT JOIN mailboxes mb ON mb.id=fpb.mailbox_id
		WHERE fpb.verified_email_id=? AND fpb.user_id=? AND fpb.status=? ORDER BY fpb.created_at`, id, userID, forwardingBindingPending)
	if err != nil {
		a.respondForwardingVerificationResult(w, r, http.StatusInternalServerError, false, email, "验证失败，请稍后重试", nil)
		return
	}
	var bindings []ForwardingPendingBinding
	for rows.Next() {
		var item ForwardingPendingBinding
		var createdAt, updatedAt string
		var activatedAt sql.NullString
		if err := rows.Scan(&item.ID, &item.VerifiedEmailID, &item.Email, &item.Scope, &item.MailboxID,
			&item.MailboxAddress, &item.Status, &item.FailureReason, &createdAt, &updatedAt, &activatedAt); err != nil {
			rows.Close()
			a.respondForwardingVerificationResult(w, r, http.StatusInternalServerError, false, email, "验证失败，请稍后重试", nil)
			return
		}
		bindings = append(bindings, item)
	}
	if err := rows.Close(); err != nil {
		a.respondForwardingVerificationResult(w, r, http.StatusInternalServerError, false, email, "验证失败，请稍后重试", nil)
		return
	}
	activations := make([]ForwardingActivation, 0, len(bindings))
	for _, binding := range bindings {
		if err := a.activateForwardingBindingTx(r.Context(), tx, userID, binding, now); err != nil {
			if _, updateErr := tx.ExecContext(r.Context(), `UPDATE forwarding_pending_bindings SET status=?,failure_reason=?,updated_at=? WHERE id=?`, forwardingBindingFailed, err.Error(), now, binding.ID); updateErr != nil {
				a.respondForwardingVerificationResult(w, r, http.StatusInternalServerError, false, email, "邮箱已验证，但自动启用失败，请在设置页重试", nil)
				return
			}
			_ = a.recordForwardingAuditTx(r.Context(), tx, userID, binding, "binding.activation_failed", forwardingBindingFailed, err.Error())
			continue
		}
		if _, err := tx.ExecContext(r.Context(), `UPDATE forwarding_pending_bindings SET status=?,failure_reason='',updated_at=?,activated_at=? WHERE id=?`, forwardingBindingActive, now, now, binding.ID); err != nil {
			a.respondForwardingVerificationResult(w, r, http.StatusInternalServerError, false, email, "邮箱已验证，但自动启用失败，请在设置页重试", nil)
			return
		}
		if err := a.recordForwardingAuditTx(r.Context(), tx, userID, binding, "binding.activated", forwardingBindingActive, "verification confirmed"); err != nil {
			a.respondForwardingVerificationResult(w, r, http.StatusInternalServerError, false, email, "验证失败，请稍后重试", nil)
			return
		}
		source := binding.MailboxAddress
		if binding.Scope == forwardingBindingScopeAccount {
			source = "账号下全部会员邮箱"
		}
		activations = append(activations, ForwardingActivation{Scope: binding.Scope, SourceEmail: source, TargetEmail: binding.Email})
	}
	if err := tx.Commit(); err != nil {
		a.respondForwardingVerificationResult(w, r, http.StatusInternalServerError, false, email, "验证失败，请稍后重试", nil)
		return
	}
	message := "邮箱验证成功"
	if len(activations) > 0 {
		message = "邮件转发已自动启用"
	}
	a.respondForwardingVerificationResult(w, r, http.StatusOK, true, email, message, activations)
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
		VerifiedEmails:   []ForwardingVerifiedEmail{},
		MailboxRules:     []MailboxForwardingRule{},
		PendingBindings:  []ForwardingPendingBinding{},
		MailboxSummaries: []ForwardingMailboxSummary{},
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
		item.MailboxBindings = []string{}
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
	rows, err = a.db.QueryContext(ctx, `SELECT mfs.mailbox_id,mb.address,mfs.target_email,mfs.target_emails
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
		if err := rows.Scan(&item.MailboxID, &item.MailboxAddress, &target, &targetsJSON); err != nil {
			return settings, err
		}
		item.TargetEmails = forwardingTargetsFromStored(target, targetsJSON)
		item.TargetEmail = firstForwardingTarget(item.TargetEmails)
		if len(item.TargetEmails) > 0 {
			settings.MailboxRules = append(settings.MailboxRules, item)
		}
	}
	if err := rows.Err(); err != nil {
		return settings, err
	}
	if err := rows.Close(); err != nil {
		return settings, err
	}
	verifiedByEmail := make(map[string]bool, len(settings.VerifiedEmails))
	verifiedIndexByEmail := make(map[string]int, len(settings.VerifiedEmails))
	for i := range settings.VerifiedEmails {
		key := strings.ToLower(settings.VerifiedEmails[i].Email)
		verifiedByEmail[key] = settings.VerifiedEmails[i].Verified
		verifiedIndexByEmail[key] = i
	}
	for _, target := range settings.AccountTargetEmails {
		if i, ok := verifiedIndexByEmail[strings.ToLower(target)]; ok {
			settings.VerifiedEmails[i].GlobalBinding = true
		}
	}
	for _, rule := range settings.MailboxRules {
		summary := ForwardingMailboxSummary{
			MailboxID:          rule.MailboxID,
			MailboxAddress:     rule.MailboxAddress,
			IndependentTargets: len(rule.TargetEmails),
			InheritedTargets:   len(settings.AccountTargetEmails),
			Enabled:            len(rule.TargetEmails) > 0,
			Targets:            []ForwardingMailboxTargetSummary{},
		}
		for _, target := range rule.TargetEmails {
			key := strings.ToLower(target)
			summary.Targets = append(summary.Targets, ForwardingMailboxTargetSummary{
				Email: target, Verified: verifiedByEmail[key], Source: "mailbox",
			})
			if i, ok := verifiedIndexByEmail[key]; ok {
				settings.VerifiedEmails[i].MailboxBindings = append(settings.VerifiedEmails[i].MailboxBindings, rule.MailboxAddress)
			}
		}
		settings.MailboxSummaries = append(settings.MailboxSummaries, summary)
	}
	rows, err = a.db.QueryContext(ctx, `SELECT fpb.id,fpb.verified_email_id,fpb.target_email,fpb.scope,
		fpb.mailbox_id,COALESCE(mb.address,''),fpb.status,fpb.failure_reason,
		fpb.created_at,fpb.updated_at,fpb.activated_at
		FROM forwarding_pending_bindings fpb
		LEFT JOIN mailboxes mb ON mb.id=fpb.mailbox_id
		WHERE fpb.user_id=? AND fpb.status<>?
		ORDER BY fpb.updated_at DESC,fpb.id`, userID, forwardingBindingCancelled)
	if err != nil {
		return settings, err
	}
	defer rows.Close()
	for rows.Next() {
		var item ForwardingPendingBinding
		var createdAt, updatedAt string
		var activatedAt sql.NullString
		if err := rows.Scan(&item.ID, &item.VerifiedEmailID, &item.Email, &item.Scope,
			&item.MailboxID, &item.MailboxAddress, &item.Status, &item.FailureReason,
			&createdAt, &updatedAt, &activatedAt); err != nil {
			return settings, err
		}
		item.CreatedAt = parseTime(createdAt)
		item.UpdatedAt = parseTime(updatedAt)
		item.ActivatedAt = nullableTime(activatedAt)
		settings.PendingBindings = append(settings.PendingBindings, item)
	}
	return settings, rows.Err()
}

func (a *App) forwardingPendingBinding(ctx context.Context, userID, id string) (ForwardingPendingBinding, error) {
	var item ForwardingPendingBinding
	var createdAt, updatedAt string
	var activatedAt sql.NullString
	err := a.db.QueryRowContext(ctx, `SELECT fpb.id,fpb.verified_email_id,fpb.target_email,fpb.scope,
		fpb.mailbox_id,COALESCE(mb.address,''),fpb.status,fpb.failure_reason,
		fpb.created_at,fpb.updated_at,fpb.activated_at
		FROM forwarding_pending_bindings fpb
		LEFT JOIN mailboxes mb ON mb.id=fpb.mailbox_id
		WHERE fpb.id=? AND fpb.user_id=?`, id, userID).Scan(
		&item.ID, &item.VerifiedEmailID, &item.Email, &item.Scope,
		&item.MailboxID, &item.MailboxAddress, &item.Status, &item.FailureReason,
		&createdAt, &updatedAt, &activatedAt)
	if err != nil {
		return item, err
	}
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	item.ActivatedAt = nullableTime(activatedAt)
	return item, nil
}

func (a *App) activateVerifiedForwardingBinding(ctx context.Context, userID string, binding ForwardingPendingBinding) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := a.now().UTC().Format(time.RFC3339Nano)
	if err := a.activateForwardingBindingTx(ctx, tx, userID, binding, now); err != nil {
		return err
	}
	if binding.ID != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE forwarding_pending_bindings SET status=?,failure_reason='',updated_at=?,activated_at=? WHERE id=? AND user_id=?`, forwardingBindingActive, now, now, binding.ID, userID); err != nil {
			return err
		}
	}
	if err := a.recordForwardingAuditTx(ctx, tx, userID, binding, "binding.activated", forwardingBindingActive, ""); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *App) activateForwardingBindingTx(ctx context.Context, tx *sql.Tx, userID string, binding ForwardingPendingBinding, now string) error {
	target := normalizeEmail(binding.Email)
	if target == "" {
		return errors.New("转发目标无效")
	}
	switch binding.Scope {
	case forwardingBindingScopeAccount:
		var legacy, targetsJSON string
		err := tx.QueryRowContext(ctx, `SELECT target_email,target_emails FROM account_forwarding_settings WHERE user_id=?`, userID).Scan(&legacy, &targetsJSON)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		targets := forwardingTargetsFromStored(legacy, targetsJSON)
		targets = dedupeEmails(append(targets, target))
		_, err = tx.ExecContext(ctx, `INSERT INTO account_forwarding_settings(user_id,target_email,target_emails,updated_at)
			VALUES(?,?,?,?) ON CONFLICT(user_id) DO UPDATE SET target_email=excluded.target_email,target_emails=excluded.target_emails,updated_at=excluded.updated_at`,
			userID, firstForwardingTarget(targets), jsonEncode(targets), now)
		return err
	case forwardingBindingScopeMailbox:
		var mailboxAddress string
		if err := tx.QueryRowContext(ctx, `SELECT address FROM mailboxes WHERE id=? AND user_id=? AND status='active'`, binding.MailboxID, userID).Scan(&mailboxAddress); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errors.New("会员邮箱不存在或已停用")
			}
			return err
		}
		var legacy, targetsJSON string
		err := tx.QueryRowContext(ctx, `SELECT target_email,target_emails FROM mailbox_forwarding_settings WHERE mailbox_id=?`, binding.MailboxID).Scan(&legacy, &targetsJSON)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		targets := forwardingTargetsFromStored(legacy, targetsJSON)
		targets = dedupeEmails(append(targets, target))
		_, err = tx.ExecContext(ctx, `INSERT INTO mailbox_forwarding_settings(mailbox_id,target_email,target_emails,updated_at)
			VALUES(?,?,?,?) ON CONFLICT(mailbox_id) DO UPDATE SET target_email=excluded.target_email,target_emails=excluded.target_emails,updated_at=excluded.updated_at`,
			binding.MailboxID, firstForwardingTarget(targets), jsonEncode(targets), now)
		return err
	default:
		return errors.New("转发绑定范围无效")
	}
}

func (a *App) recordForwardingAuditTx(ctx context.Context, tx *sql.Tx, userID string, binding ForwardingPendingBinding, event, status, detail string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO forwarding_audit_events(id,user_id,verified_email_id,binding_id,mailbox_id,target_email,event,status,detail,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, newID("fwdaudit"), userID, binding.VerifiedEmailID, binding.ID, binding.MailboxID, normalizeEmail(binding.Email), event, status, detail, a.now().UTC().Format(time.RFC3339Nano))
	return err
}

func (a *App) issueForwardingVerification(ctx context.Context, userID, id, email string, insert bool) error {
	now := a.now().UTC()
	expires := now.Add(forwardingVerificationTTL)
	token := randomToken()
	nowRaw := now.Format(time.RFC3339Nano)
	expiresRaw := expires.Format(time.RFC3339Nano)
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := reserveForwardingVerificationAttempt(ctx, tx, userID, email, now); err != nil {
		return err
	}
	if insert {
		if _, err := tx.ExecContext(ctx, `INSERT INTO forwarding_verified_emails(id,user_id,email,verified,verified_at,verification_token_hash,verification_sent_at,verification_expires_at,delivery_queue_id,delivery_status,delivery_error,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			id, userID, email, 0, nil, hashToken(token), nowRaw, expiresRaw, "", sendQueueStatusQueued, "", nowRaw, nowRaw); err != nil {
			return err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE forwarding_verified_emails
			SET verified=0,verified_at=NULL,verification_token_hash=?,verification_sent_at=?,verification_expires_at=?,delivery_queue_id='',delivery_status=?,delivery_error='',updated_at=?
			WHERE id=? AND user_id=?`,
			hashToken(token), nowRaw, expiresRaw, sendQueueStatusQueued, nowRaw, id, userID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO forwarding_verification_attempts(id,user_id,email,created_at) VALUES(?,?,?,?)`, newID("fva"), userID, email, nowRaw); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM forwarding_verification_attempts WHERE created_at<?`, now.Add(-forwardingVerificationTTL).Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
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

func reserveForwardingVerificationAttempt(ctx context.Context, tx *sql.Tx, userID, email string, now time.Time) error {
	var lastSentRaw string
	err := tx.QueryRowContext(ctx, `SELECT created_at FROM forwarding_verification_attempts WHERE user_id=? AND email=? ORDER BY created_at DESC LIMIT 1`, userID, email).Scan(&lastSentRaw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		retryAfter := parseTime(lastSentRaw).Add(forwardingVerificationUserCooldown).Sub(now)
		if retryAfter > 0 {
			return &forwardingVerificationRateLimitError{
				message:    "操作过于频繁，请在" + formatForwardingVerificationWait(retryAfter) + "后重试",
				retryAfter: retryAfter,
			}
		}
	}

	var count int
	var earliestRaw sql.NullString
	cutoff := now.Add(-forwardingVerificationTTL)
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1),MIN(created_at) FROM forwarding_verification_attempts WHERE email=? AND created_at>?`, email, cutoff.Format(time.RFC3339Nano)).Scan(&count, &earliestRaw); err != nil {
		return err
	}
	if count >= forwardingVerificationTargetLimit && earliestRaw.Valid {
		retryAfter := parseTime(earliestRaw.String).Add(forwardingVerificationTTL).Sub(now)
		if retryAfter < time.Second {
			retryAfter = time.Second
		}
		return &forwardingVerificationRateLimitError{
			message:    "该邮箱24小时内验证邮件发送次数已达上限，请在" + formatForwardingVerificationWait(retryAfter) + "后重试",
			retryAfter: retryAfter,
		}
	}
	return nil
}

func respondForwardingVerificationRateLimit(w http.ResponseWriter, err error) bool {
	var rateLimitErr *forwardingVerificationRateLimitError
	if !errors.As(err, &rateLimitErr) {
		return false
	}
	seconds := int((rateLimitErr.retryAfter + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	respondError(w, http.StatusTooManyRequests, rateLimitErr.Error())
	return true
}

func formatForwardingVerificationWait(wait time.Duration) string {
	seconds := int((wait + time.Second - 1) / time.Second)
	if seconds < 60 {
		return strconv.Itoa(seconds) + "秒"
	}
	minutes := (seconds + 59) / 60
	if minutes < 60 {
		return strconv.Itoa(minutes) + "分钟"
	}
	hours := minutes / 60
	remainingMinutes := minutes % 60
	if remainingMinutes == 0 {
		return strconv.Itoa(hours) + "小时"
	}
	return fmt.Sprintf("%d小时%d分钟", hours, remainingMinutes)
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
	maskedEmail := maskEmailAddress(targetEmail)
	text := "NewSzxcn Email Service\n\n请确认您的邮箱转发设置\n\n您正在 NewSzxcn 邮箱中添加此地址作为邮件转发目标，请确认这是您本人的操作。\n\n转发地址：" + maskedEmail + "\n有效时间：24 小时\n\n确认转发地址：\n" + link + "\n\n如果按钮无法点击，请复制上方完整链接到浏览器中打开。\n\n如果这不是您的操作，可以直接忽略此邮件。您的邮箱设置不会被更改。\n\n此邮件由 NewSzxcn Email Service 自动发送，请勿直接回复。"
	var htmlBuilder strings.Builder
	if err := forwardingVerificationHTMLTemplate.Execute(&htmlBuilder, forwardingVerificationTemplateData{MaskedEmail: maskedEmail, Link: link}); err != nil {
		return "", err
	}
	html := htmlBuilder.String()
	messageID := fmt.Sprintf("<%s@%s>", newID("fwdverify"), fromDomain)
	mimeBytes, err := BuildMIME(MIMEMessage{From: from, FromName: systemSenderDisplayName, To: []string{targetEmail}, Subject: "请确认您的邮箱转发设置", Text: text, HTML: html, MessageID: messageID, Date: now})
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

func maskEmailAddress(value string) string {
	value = strings.TrimSpace(value)
	at := strings.LastIndex(value, "@")
	if at <= 0 || at == len(value)-1 {
		return "***"
	}
	local := []rune(value[:at])
	domain := value[at+1:]
	var maskedLocal string
	switch len(local) {
	case 0:
		maskedLocal = "***"
	case 1:
		maskedLocal = "*"
	case 2:
		maskedLocal = string(local[0]) + "*"
	default:
		maskedLocal = string(local[0]) + "***" + string(local[len(local)-1])
	}
	return maskedLocal + "@" + domain
}

func (a *App) forwardingVerificationURL(token string) string {
	base := strings.TrimRight(strings.TrimSpace(a.config().PublicBaseURL), "/")
	if base == "" {
		base = "https://" + strings.Trim(strings.TrimSpace(a.config().PublicHostname), "/")
	}
	return base + "/mail/forwarding/verification/confirm?token=" + url.QueryEscape(token)
}

func (a *App) respondForwardingVerificationResult(w http.ResponseWriter, r *http.Request, status int, ok bool, email, message string, activations []ForwardingActivation) {
	if r.URL.Query().Get("format") == "json" || strings.Contains(r.Header.Get("Accept"), "application/json") {
		respondJSON(w, status, map[string]any{"ok": ok, "email": email, "message": message, "activations": activations})
		return
	}
	a.renderForwardingVerificationPage(w, status, ok, email, message, activations)
}

func (a *App) renderForwardingVerificationPage(w http.ResponseWriter, status int, ok bool, email, message string, activations []ForwardingActivation) {
	title := "邮箱转发验证"
	heading := "验证失败"
	color := "#dc2626"
	statusMark := "!"
	closingMessage := "请联系验证发起人重新发送链接"
	if ok {
		heading = "邮箱验证成功"
		color = "#16a34a"
		statusMark = "&#10003;"
		closingMessage = "验证结果已记录，可以关闭此页面"
	}
	relations := ""
	if len(activations) > 0 {
		relations = `<div style="margin:20px 0;padding:16px;border-radius:8px;background:#f1f5f9"><strong style="display:block;margin-bottom:10px">邮件转发已自动启用</strong>`
		for _, activation := range activations {
			relations += `<div style="padding:8px 0;color:#334155;word-break:break-all">` + htmlEscape(activation.SourceEmail) + `<br><span style="color:#64748b">&rarr; ` + htmlEscape(activation.TargetEmail) + `</span></div>`
		}
		relations += `</div>`
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>%s</title></head><body style="margin:0;background:#f8fafc;color:#0f172a;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Arial,sans-serif"><main style="min-height:100vh;display:grid;place-items:center;padding:24px"><section style="width:min(100%%,520px);background:white;border:1px solid #e2e8f0;border-radius:8px;padding:34px 30px;box-shadow:0 18px 45px rgba(15,23,42,.08)"><div aria-hidden="true" style="display:grid;place-items:center;width:44px;height:44px;margin:0 0 20px;border-radius:50%%;background:%s;color:white;font-size:24px;font-weight:700">%s</div><h1 style="margin:0 0 14px;font-size:28px">%s</h1><p style="margin:0 0 10px;font-size:17px;color:#475569">%s</p><p style="margin:0 0 24px;font-size:15px;color:#64748b;word-break:break-all">%s</p>%s<p style="margin:0;padding-top:20px;border-top:1px solid #e2e8f0;font-size:15px;color:#64748b">%s</p></section></main></body></html>`,
		title, color, statusMark, heading, htmlEscape(message), htmlEscape(email), relations, htmlEscape(closingMessage))
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
