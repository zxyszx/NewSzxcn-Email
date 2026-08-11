package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

type MailboxApplyOptions struct {
	Enabled          bool     `json:"enabled"`
	Domains          []Domain `json:"domains"`
	ReservedPrefixes []string `json:"reservedPrefixes,omitempty"`
}

func (a *App) handleMailboxApplyOptions(w http.ResponseWriter, r *http.Request) {
	domains, err := a.mailboxApplyDomains(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load domains")
		return
	}
	respondJSON(w, http.StatusOK, MailboxApplyOptions{
		Enabled:          a.config().UserMailboxApplyEnabled,
		Domains:          domains,
		ReservedPrefixes: parseReservedPrefixes(a.config().ReservedMailboxPrefixes),
	})
}

func (a *App) handleApplyMailbox(w http.ResponseWriter, r *http.Request) {
	if !a.config().UserMailboxApplyEnabled {
		respondError(w, http.StatusForbidden, "当前未开放邮箱申请")
		return
	}
	user := currentUser(r)
	var req struct {
		DomainID    string `json:"domainId"`
		LocalPart   string `json:"localPart"`
		DisplayName string `json:"displayName"`
	}
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	domainID := strings.TrimSpace(req.DomainID)
	if domainID == "" {
		badRequest(w, errors.New("请选择域名"))
		return
	}
	allowed, err := a.mailboxApplyDomainAllowed(r.Context(), domainID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to check domain")
		return
	}
	if !allowed {
		respondError(w, http.StatusForbidden, "该域名不可用")
		return
	}

	localPart := normalizeLocalPart(req.LocalPart)
	if localPart == "" {
		badRequest(w, errors.New("请输入邮箱前缀"))
		return
	}
	if len(localPart) > 64 {
		badRequest(w, errors.New("邮箱前缀过长"))
		return
	}
	reserved := map[string]bool{}
	for _, item := range parseReservedPrefixes(a.config().ReservedMailboxPrefixes) {
		reserved[item] = true
	}
	if reserved[localPart] {
		respondError(w, http.StatusForbidden, "localPart is reserved")
		return
	}
	var exists int
	if err := a.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM mailboxes WHERE domain_id=? AND local_part=?`, domainID, localPart).Scan(&exists); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to check mailbox")
		return
	}
	if exists > 0 {
		respondError(w, http.StatusConflict, "该邮箱地址已被占用")
		return
	}
	if user.Role != "admin" && user.Limits.MaxMailboxCount > 0 {
		var ownedCount int
		if err := a.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM mailboxes WHERE user_id=? AND status='active'`, user.ID).Scan(&ownedCount); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to check mailbox quota")
			return
		}
		if ownedCount >= user.Limits.MaxMailboxCount {
			respondError(w, http.StatusForbidden, "邮箱数量已达上限")
			return
		}
	}

	var passwordHash string
	if err := a.db.QueryRowContext(r.Context(), `SELECT password_hash FROM users WHERE id=? AND disabled=0`, user.ID).Scan(&passwordHash); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	displayName := strings.TrimSpace(req.DisplayName)
	if len([]rune(displayName)) > 80 {
		badRequest(w, errors.New("displayName must be at most 80 characters"))
		return
	}
	quotaMB := defaultUserStorageQuotaMB
	if user.Role == "admin" {
		quotaMB = 0
	}
	mailboxID, err := a.createMailboxWithPasswordHash(r.Context(), user.ID, domainID, localPart, displayName, passwordHash, quotaMB, "active")
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			respondError(w, http.StatusConflict, "该邮箱地址已被占用")
			return
		}
		badRequest(w, err)
		return
	}
	mailbox, err := a.mailboxByID(r.Context(), mailboxID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load mailbox")
		return
	}
	respondJSON(w, http.StatusCreated, mailbox)
}

func (a *App) mailboxApplyDomains(ctx context.Context) ([]Domain, error) {
	if !a.config().UserMailboxApplyEnabled {
		return []Domain{}, nil
	}
	ids := cleanIDList(strings.Split(a.config().UserMailboxDomainIDs, ","))
	if len(ids) == 0 {
		return []Domain{}, nil
	}
	items := make([]Domain, 0, len(ids))
	for _, id := range ids {
		domain, err := a.domainByID(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if domain.Status == "active" {
			items = append(items, *domain)
		}
	}
	return items, nil
}

func (a *App) mailboxApplyDomainAllowed(ctx context.Context, domainID string) (bool, error) {
	domains, err := a.mailboxApplyDomains(ctx)
	if err != nil {
		return false, err
	}
	for _, domain := range domains {
		if domain.ID == domainID {
			return true, nil
		}
	}
	return false, nil
}

func (a *App) handleListContacts(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	rows, err := a.db.QueryContext(r.Context(), `SELECT id,user_id,name,email,note,created_at FROM contacts WHERE user_id=? ORDER BY name,email`, user.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load contacts")
		return
	}
	defer rows.Close()
	items := []Contact{}
	for rows.Next() {
		item, err := scanContact(rows)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to scan contacts")
			return
		}
		items = append(items, item)
	}
	respondJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *App) handleCreateContact(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	var req struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		Note  string `json:"note"`
	}
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	email := normalizeEmail(req.Email)
	if email == "" || !strings.Contains(email, "@") {
		badRequest(w, errors.New("invalid email"))
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = email
	}
	id := newID("ctc")
	now := a.now().UTC().Format(time.RFC3339Nano)
	_, err := a.db.ExecContext(r.Context(), `INSERT INTO contacts(id,user_id,name,email,note,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(user_id,email) DO UPDATE SET name=excluded.name,note=excluded.note,updated_at=excluded.updated_at`,
		id, user.ID, name, email, strings.TrimSpace(req.Note), now, now)
	if err != nil {
		badRequest(w, err)
		return
	}
	row := a.db.QueryRowContext(r.Context(), `SELECT id,user_id,name,email,note,created_at FROM contacts WHERE user_id=? AND email=?`, user.ID, email)
	item, err := scanContact(row)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load contact")
		return
	}
	respondJSON(w, http.StatusCreated, item)
}

func (a *App) handleDeleteContact(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	res, err := a.db.ExecContext(r.Context(), `DELETE FROM contacts WHERE id=? AND user_id=?`, chi.URLParam(r, "id"), user.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete contact")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondError(w, http.StatusNotFound, "contact not found")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleListSignatures(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	rows, err := a.db.QueryContext(r.Context(), `SELECT id,user_id,mailbox_id,name,content,is_default,created_at,updated_at FROM mail_signatures WHERE user_id=? ORDER BY is_default DESC, updated_at DESC, created_at DESC`, user.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load signatures")
		return
	}
	defer rows.Close()
	items := []MailSignature{}
	for rows.Next() {
		item, err := scanSignature(rows)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to scan signatures")
			return
		}
		items = append(items, item)
	}
	respondJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *App) handleCreateSignature(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	var req struct {
		MailboxID string `json:"mailboxId"`
		Name      string `json:"name"`
		Content   string `json:"content"`
		IsDefault bool   `json:"isDefault"`
	}
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	mailboxID, name, content, ok := a.normalizeSignatureInput(w, r, user.ID, req.MailboxID, req.Name, req.Content)
	if !ok {
		return
	}
	id := newID("sig")
	now := a.now().UTC().Format(time.RFC3339Nano)
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save signature")
		return
	}
	defer tx.Rollback()
	if req.IsDefault {
		if _, err := tx.ExecContext(r.Context(), `UPDATE mail_signatures SET is_default=0, updated_at=? WHERE user_id=? AND mailbox_id=?`, now, user.ID, mailboxID); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to update default signature")
			return
		}
	}
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO mail_signatures(id,user_id,mailbox_id,name,content,is_default,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`,
		id, user.ID, mailboxID, name, content, boolInt(req.IsDefault), now, now); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save signature")
		return
	}
	if err := tx.Commit(); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save signature")
		return
	}
	item, err := a.signatureByID(r.Context(), user.ID, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load signature")
		return
	}
	respondJSON(w, http.StatusCreated, item)
}

func (a *App) handleUpdateSignature(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	id := chi.URLParam(r, "id")
	_, err := a.signatureByID(r.Context(), user.ID, id)
	if errors.Is(err, sql.ErrNoRows) {
		respondError(w, http.StatusNotFound, "signature not found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load signature")
		return
	}
	var req struct {
		MailboxID string `json:"mailboxId"`
		Name      string `json:"name"`
		Content   string `json:"content"`
		IsDefault bool   `json:"isDefault"`
	}
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	mailboxID, name, content, ok := a.normalizeSignatureInput(w, r, user.ID, req.MailboxID, req.Name, req.Content)
	if !ok {
		return
	}
	now := a.now().UTC().Format(time.RFC3339Nano)
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update signature")
		return
	}
	defer tx.Rollback()
	if req.IsDefault {
		if _, err := tx.ExecContext(r.Context(), `UPDATE mail_signatures SET is_default=0, updated_at=? WHERE user_id=? AND mailbox_id=?`, now, user.ID, mailboxID); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to update default signature")
			return
		}
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE mail_signatures SET mailbox_id=?, name=?, content=?, is_default=?, updated_at=? WHERE id=? AND user_id=?`,
		mailboxID, name, content, boolInt(req.IsDefault), now, id, user.ID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update signature")
		return
	}
	if err := tx.Commit(); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update signature")
		return
	}
	item, err := a.signatureByID(r.Context(), user.ID, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load signature")
		return
	}
	respondJSON(w, http.StatusOK, item)
}

func (a *App) handleDeleteSignature(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	res, err := a.db.ExecContext(r.Context(), `DELETE FROM mail_signatures WHERE id=? AND user_id=?`, chi.URLParam(r, "id"), user.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete signature")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondError(w, http.StatusNotFound, "signature not found")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleSetDefaultSignature(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	id := chi.URLParam(r, "id")
	item, err := a.signatureByID(r.Context(), user.ID, id)
	if errors.Is(err, sql.ErrNoRows) {
		respondError(w, http.StatusNotFound, "signature not found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load signature")
		return
	}
	now := a.now().UTC().Format(time.RFC3339Nano)
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update signature")
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), `UPDATE mail_signatures SET is_default=0, updated_at=? WHERE user_id=? AND mailbox_id=?`, now, user.ID, item.MailboxID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update signature")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE mail_signatures SET is_default=1, updated_at=? WHERE id=? AND user_id=?`, now, id, user.ID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update signature")
		return
	}
	if err := tx.Commit(); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update signature")
		return
	}
	item, err = a.signatureByID(r.Context(), user.ID, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load signature")
		return
	}
	respondJSON(w, http.StatusOK, item)
}

func (a *App) handleDefaultSignature(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	mailboxID := strings.TrimSpace(r.URL.Query().Get("mailboxId"))
	if mailboxID != "" {
		if _, err := a.mailboxForUserByID(r.Context(), user.ID, mailboxID); err != nil {
			respondError(w, http.StatusForbidden, "mailbox not found")
			return
		}
	}
	item, err := a.defaultSignatureForMailbox(r.Context(), user.ID, mailboxID)
	if errors.Is(err, sql.ErrNoRows) {
		respondJSON(w, http.StatusOK, map[string]any{"signature": nil})
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load signature")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"signature": item})
}

func (a *App) handleListRules(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	rows, err := a.db.QueryContext(r.Context(), `SELECT id,user_id,mailbox_id,name,match_mode,conditions_json,actions_json,from_contains,subject_contains,action,apply_to_existing,stop_processing,enabled,created_at FROM mail_rules WHERE user_id=? ORDER BY created_at DESC`, user.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load rules")
		return
	}
	defer rows.Close()
	items := []MailRule{}
	for rows.Next() {
		item, err := scanRule(rows)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to scan rules")
			return
		}
		items = append(items, item)
	}
	respondJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *App) handleCreateRule(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	var req struct {
		MailboxID       string              `json:"mailboxId"`
		Name            string              `json:"name"`
		MatchMode       string              `json:"matchMode"`
		Conditions      []MailRuleCondition `json:"conditions"`
		Actions         []MailRuleAction    `json:"actions"`
		FromContains    string              `json:"fromContains"`
		SubjectContains string              `json:"subjectContains"`
		Action          string              `json:"action"`
		ApplyToExisting bool                `json:"applyToExisting"`
		StopProcessing  bool                `json:"stopProcessing"`
		Enabled         *bool               `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	mailboxID, ok := a.optionalMailboxIDForUser(r, req.MailboxID)
	if !ok {
		respondError(w, http.StatusNotFound, "mailbox not found")
		return
	}
	rawMatchMode := strings.ToLower(strings.TrimSpace(req.MatchMode))
	if rawMatchMode != "" && rawMatchMode != "all" && rawMatchMode != "and" && rawMatchMode != "any" && rawMatchMode != "or" {
		badRequest(w, errors.New("invalid match mode"))
		return
	}
	matchMode := normalizeRuleMatchMode(rawMatchMode)
	conditions := normalizeRuleConditions(req.Conditions, req.FromContains, req.SubjectContains)
	if len(conditions) == 0 {
		badRequest(w, errors.New("rule condition is required"))
		return
	}
	actions := normalizeRuleActions(req.Actions, req.Action)
	if len(actions) == 0 {
		badRequest(w, errors.New("rule action is required"))
		return
	}
	actions, err := a.cleanRuleActions(r.Context(), user.ID, actions)
	if err != nil {
		badRequest(w, err)
		return
	}
	if len(actions) == 0 {
		badRequest(w, errors.New("rule action is required"))
		return
	}
	conditionsJSON, err := json.Marshal(conditions)
	if err != nil {
		badRequest(w, err)
		return
	}
	actionsJSON, err := json.Marshal(actions)
	if err != nil {
		badRequest(w, err)
		return
	}
	fromContains := legacyConditionValue(conditions, "from")
	subjectContains := legacyConditionValue(conditions, "subject")
	action := actions[0].Type
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "收件规则"
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	id := newID("rule")
	now := a.now().UTC().Format(time.RFC3339Nano)
	_, err = a.db.ExecContext(r.Context(), `INSERT INTO mail_rules(id,user_id,mailbox_id,name,match_mode,conditions_json,actions_json,from_contains,subject_contains,action,apply_to_existing,stop_processing,enabled,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, user.ID, mailboxID, name, matchMode, string(conditionsJSON), string(actionsJSON), fromContains, subjectContains, action, boolInt(req.ApplyToExisting), boolInt(req.StopProcessing), boolInt(enabled), now, now)
	if err != nil {
		badRequest(w, err)
		return
	}
	appliedCount := int64(0)
	if req.ApplyToExisting {
		appliedCount, err = a.applyRuleToExistingMessages(r.Context(), user.ID, mailboxID, MailRule{
			ID: id, UserID: user.ID, MailboxID: mailboxID, Name: name, MatchMode: matchMode,
			Conditions: conditions, Actions: actions, ApplyToExisting: req.ApplyToExisting, StopProcessing: req.StopProcessing,
			FromContains: fromContains, SubjectContains: subjectContains, Action: action, Enabled: enabled,
		})
		if err != nil {
			respondError(w, http.StatusInternalServerError, "rule saved but failed to apply to existing messages")
			return
		}
	}
	row := a.db.QueryRowContext(r.Context(), `SELECT id,user_id,mailbox_id,name,match_mode,conditions_json,actions_json,from_contains,subject_contains,action,apply_to_existing,stop_processing,enabled,created_at FROM mail_rules WHERE id=?`, id)
	item, err := scanRule(row)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load rule")
		return
	}
	item.AppliedExistingCount = appliedCount
	respondJSON(w, http.StatusCreated, item)
}

func (a *App) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	res, err := a.db.ExecContext(r.Context(), `DELETE FROM mail_rules WHERE id=? AND user_id=?`, chi.URLParam(r, "id"), user.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete rule")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondError(w, http.StatusNotFound, "rule not found")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleUpdateRule(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	id := chi.URLParam(r, "id")
	item, err := a.ruleByID(r.Context(), user.ID, id)
	if errors.Is(err, sql.ErrNoRows) {
		respondError(w, http.StatusNotFound, "rule not found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load rule")
		return
	}
	var req struct {
		MailboxID      *string              `json:"mailboxId"`
		Name           *string              `json:"name"`
		MatchMode      *string              `json:"matchMode"`
		Conditions     *[]MailRuleCondition `json:"conditions"`
		Actions        *[]MailRuleAction    `json:"actions"`
		ApplyExisting  *bool                `json:"applyToExisting"`
		StopProcessing *bool                `json:"stopProcessing"`
		Enabled        *bool                `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	if req.MailboxID != nil {
		mailboxID, ok := a.optionalMailboxIDForUser(r, *req.MailboxID)
		if !ok {
			respondError(w, http.StatusNotFound, "mailbox not found")
			return
		}
		item.MailboxID = mailboxID
	}
	if req.Name != nil {
		item.Name = strings.TrimSpace(*req.Name)
		if item.Name == "" {
			item.Name = "收件规则"
		}
	}
	if req.MatchMode != nil {
		raw := strings.ToLower(strings.TrimSpace(*req.MatchMode))
		if raw != "all" && raw != "and" && raw != "any" && raw != "or" {
			badRequest(w, errors.New("invalid match mode"))
			return
		}
		item.MatchMode = normalizeRuleMatchMode(raw)
	}
	if req.Conditions != nil {
		item.Conditions = normalizeRuleConditions(*req.Conditions, "", "")
		if len(item.Conditions) == 0 {
			badRequest(w, errors.New("rule condition is required"))
			return
		}
	}
	if req.Actions != nil {
		item.Actions = normalizeRuleActions(*req.Actions, "")
		item.Actions, err = a.cleanRuleActions(r.Context(), user.ID, item.Actions)
		if err != nil || len(item.Actions) == 0 {
			if err == nil {
				err = errors.New("rule action is required")
			}
			badRequest(w, err)
			return
		}
	}
	if req.ApplyExisting != nil {
		item.ApplyToExisting = *req.ApplyExisting
	}
	if req.StopProcessing != nil {
		item.StopProcessing = *req.StopProcessing
	}
	if req.Enabled != nil {
		item.Enabled = *req.Enabled
	}
	conditionsJSON, err := json.Marshal(item.Conditions)
	if err != nil {
		badRequest(w, err)
		return
	}
	actionsJSON, err := json.Marshal(item.Actions)
	if err != nil {
		badRequest(w, err)
		return
	}
	item.FromContains = legacyConditionValue(item.Conditions, "from")
	item.SubjectContains = legacyConditionValue(item.Conditions, "subject")
	item.Action = item.Actions[0].Type
	now := a.now().UTC().Format(time.RFC3339Nano)
	_, err = a.db.ExecContext(r.Context(), `UPDATE mail_rules SET mailbox_id=?,name=?,match_mode=?,conditions_json=?,actions_json=?,from_contains=?,subject_contains=?,action=?,apply_to_existing=?,stop_processing=?,enabled=?,updated_at=? WHERE id=? AND user_id=?`, item.MailboxID, item.Name, item.MatchMode, string(conditionsJSON), string(actionsJSON), item.FromContains, item.SubjectContains, item.Action, boolInt(item.ApplyToExisting), boolInt(item.StopProcessing), boolInt(item.Enabled), now, id, user.ID)
	if err != nil {
		badRequest(w, err)
		return
	}
	updated, err := a.ruleByID(r.Context(), user.ID, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load rule")
		return
	}
	respondJSON(w, http.StatusOK, updated)
}

func (a *App) handleMoveRule(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	var req struct {
		Direction string `json:"direction"`
	}
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	if req.Direction != "up" && req.Direction != "down" {
		badRequest(w, errors.New("invalid direction"))
		return
	}
	type orderedRule struct{ id, createdAt string }
	rows, err := a.db.QueryContext(r.Context(), `SELECT id,created_at FROM mail_rules WHERE user_id=? ORDER BY created_at DESC`, user.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load rules")
		return
	}
	items := []orderedRule{}
	for rows.Next() {
		var item orderedRule
		if err := rows.Scan(&item.id, &item.createdAt); err != nil {
			rows.Close()
			respondError(w, http.StatusInternalServerError, "failed to scan rules")
			return
		}
		items = append(items, item)
	}
	rows.Close()
	index := -1
	for i := range items {
		if items[i].id == chi.URLParam(r, "id") {
			index = i
			break
		}
	}
	if index < 0 {
		respondError(w, http.StatusNotFound, "rule not found")
		return
	}
	target := index - 1
	if req.Direction == "down" {
		target = index + 1
	}
	if target < 0 || target >= len(items) {
		respondJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to move rule")
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), `UPDATE mail_rules SET created_at=? WHERE id=? AND user_id=?`, items[target].createdAt, items[index].id, user.ID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to move rule")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE mail_rules SET created_at=? WHERE id=? AND user_id=?`, items[index].createdAt, items[target].id, user.ID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to move rule")
		return
	}
	if err := tx.Commit(); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to move rule")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleApplyRule(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	item, err := a.ruleByID(r.Context(), user.ID, chi.URLParam(r, "id"))
	if errors.Is(err, sql.ErrNoRows) {
		respondError(w, http.StatusNotFound, "rule not found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load rule")
		return
	}
	affected, err := a.applyRuleToExistingMessages(r.Context(), user.ID, item.MailboxID, item)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to apply rule")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true, "affected": affected})
}

func (a *App) ruleByID(ctx context.Context, userID, id string) (MailRule, error) {
	return scanRule(a.db.QueryRowContext(ctx, `SELECT id,user_id,mailbox_id,name,match_mode,conditions_json,actions_json,from_contains,subject_contains,action,apply_to_existing,stop_processing,enabled,created_at FROM mail_rules WHERE id=? AND user_id=?`, id, userID))
}

func (a *App) handleListBlockedSenders(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	rows, err := a.db.QueryContext(r.Context(), `SELECT id,user_id,mailbox_id,email,reason,created_at FROM blocked_senders WHERE user_id=? ORDER BY created_at DESC`, user.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load blocked senders")
		return
	}
	defer rows.Close()
	items := []BlockedSender{}
	for rows.Next() {
		item, err := scanBlockedSender(rows)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to scan blocked senders")
			return
		}
		items = append(items, item)
	}
	respondJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *App) handleCreateBlockedSender(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	var req struct {
		MailboxID string `json:"mailboxId"`
		Email     string `json:"email"`
		Reason    string `json:"reason"`
	}
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	mailboxID, ok := a.optionalMailboxIDForUser(r, req.MailboxID)
	if !ok {
		respondError(w, http.StatusNotFound, "mailbox not found")
		return
	}
	email := normalizeEmail(req.Email)
	if email == "" || !strings.Contains(email, "@") {
		badRequest(w, errors.New("invalid email"))
		return
	}
	id := newID("blk")
	now := a.now().UTC().Format(time.RFC3339Nano)
	_, err := a.db.ExecContext(r.Context(), `INSERT INTO blocked_senders(id,user_id,mailbox_id,email,reason,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(user_id,mailbox_id,email) DO UPDATE SET reason=excluded.reason,updated_at=excluded.updated_at`,
		id, user.ID, mailboxID, email, strings.TrimSpace(req.Reason), now, now)
	if err != nil {
		badRequest(w, err)
		return
	}
	row := a.db.QueryRowContext(r.Context(), `SELECT id,user_id,mailbox_id,email,reason,created_at FROM blocked_senders WHERE user_id=? AND mailbox_id=? AND email=?`, user.ID, mailboxID, email)
	item, err := scanBlockedSender(row)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load blocked sender")
		return
	}
	respondJSON(w, http.StatusCreated, item)
}

func (a *App) handleDeleteBlockedSender(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	res, err := a.db.ExecContext(r.Context(), `DELETE FROM blocked_senders WHERE id=? AND user_id=?`, chi.URLParam(r, "id"), user.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete blocked sender")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondError(w, http.StatusNotFound, "blocked sender not found")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleMailStats(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	mailboxID := strings.TrimSpace(r.URL.Query().Get("mailboxId"))
	rangeDays := mailStatsRangeDays(r.URL.Query().Get("days"))
	args := []any{user.ID}
	where := `mb.user_id=?`
	if mailboxID != "" && !isAllMailboxID(mailboxID) {
		if _, err := a.mailboxForCurrentUserWithID(r, mailboxID); err != nil {
			respondError(w, http.StatusNotFound, "mailbox not found")
			return
		}
		where += ` AND mb.id=?`
		args = append(args, mailboxID)
	}
	now := a.now().UTC()
	stats := MailStats{
		ByFolder:     []MailStatsFolderCount{},
		Trend:        emptyMailStatsTrend(now, rangeDays),
		Distribution: []MailStatsDistributionItem{},
		TopContacts:  []MailStatsContact{},
	}
	row := a.db.QueryRowContext(r.Context(), `SELECT COUNT(m.id),
			COALESCE(SUM(CASE WHEN f.role NOT IN ('sent','drafts') THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN f.role='sent' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN m.is_read=0 THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN f.role='drafts' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN m.is_starred=1 THEN 1 ELSE 0 END),0),
			COALESCE(SUM(m.size_bytes),0)
		FROM mailboxes mb
		LEFT JOIN messages m ON m.mailbox_id=mb.id
		LEFT JOIN folders f ON f.id=m.folder_id
		WHERE `+where, args...)
	if err := row.Scan(&stats.TotalMessages, &stats.TotalIncoming, &stats.TotalOutgoing, &stats.UnreadMessages, &stats.DraftMessages, &stats.StarredMessages, &stats.StorageBytes); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load stats")
		return
	}
	if stats.TotalMessages > 0 {
		stats.AverageMessageBytes = stats.StorageBytes / stats.TotalMessages
	}
	if err := a.db.QueryRowContext(r.Context(), `SELECT COUNT(a.id),COALESCE(SUM(a.size_bytes),0) FROM attachments a JOIN messages m ON m.id=a.message_id JOIN mailboxes mb ON mb.id=m.mailbox_id WHERE `+where, args...).Scan(&stats.AttachmentCount, &stats.AttachmentBytes); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load attachment stats")
		return
	}
	var attachmentMessageCount int64
	if err := a.db.QueryRowContext(r.Context(), `SELECT COUNT(m.id) FROM mailboxes mb JOIN messages m ON m.mailbox_id=mb.id WHERE `+where+` AND m.has_attachments=1`, args...).Scan(&attachmentMessageCount); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load attachment message stats")
		return
	}
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	todayArgs := append(append([]any{}, args...), todayStart)
	if err := a.db.QueryRowContext(r.Context(), `SELECT COUNT(m.id)
		FROM mailboxes mb JOIN messages m ON m.mailbox_id=mb.id JOIN folders f ON f.id=m.folder_id
		WHERE `+where+` AND f.role='sent' AND m.sent_at>=?`, todayArgs...).Scan(&stats.TodayOutgoing); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load today stats")
		return
	}
	if err := a.db.QueryRowContext(r.Context(), `SELECT COUNT(sq.id)
		FROM send_queue sq JOIN mailboxes mb ON mb.id=sq.mailbox_id
		WHERE `+where+` AND sq.status='failed'`, args...).Scan(&stats.FailedSends); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load send queue stats")
		return
	}
	var quotaMB int64
	if err := a.db.QueryRowContext(r.Context(), `SELECT storage_quota_mb FROM users WHERE id=?`, user.ID).Scan(&quotaMB); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load quota")
		return
	}
	stats.QuotaBytes = quotaMB * 1024 * 1024
	if stats.QuotaBytes > 0 {
		stats.QuotaUsedPct = float64(stats.StorageBytes) / float64(stats.QuotaBytes) * 100
	}
	rows, err := a.db.QueryContext(r.Context(), `SELECT f.name,f.role,COUNT(m.id),COALESCE(SUM(CASE WHEN m.is_read=0 THEN 1 ELSE 0 END),0),COALESCE(SUM(m.size_bytes),0)
		FROM mailboxes mb JOIN folders f ON f.mailbox_id=mb.id LEFT JOIN messages m ON m.folder_id=f.id
		WHERE `+where+` GROUP BY f.id,f.name,f.role ORDER BY CASE f.role WHEN 'inbox' THEN 1 WHEN 'sent' THEN 2 WHEN 'drafts' THEN 3 WHEN 'archive' THEN 4 WHEN 'spam' THEN 5 WHEN 'trash' THEN 6 ELSE 99 END`, args...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load folder stats")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var item MailStatsFolderCount
		if err := rows.Scan(&item.Folder, &item.Role, &item.Count, &item.Unread, &item.Bytes); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to scan folder stats")
			return
		}
		stats.ByFolder = append(stats.ByFolder, item)
	}
	if err := rows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to scan folder stats")
		return
	}
	stats.Distribution = mailStatsDistribution(stats.ByFolder, attachmentMessageCount, stats.StarredMessages)
	if err := a.loadMailStatsTrend(r.Context(), where, args, rangeDays, stats.Trend); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load trend stats")
		return
	}
	topContacts, err := a.mailStatsTopContacts(r.Context(), where, args)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load contact stats")
		return
	}
	stats.TopContacts = topContacts
	respondJSON(w, http.StatusOK, stats)
}

func mailStatsRangeDays(raw string) int {
	days, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || days <= 0 {
		return 30
	}
	switch days {
	case 7, 30, 90, 365:
		return days
	default:
		if days < 7 {
			return 7
		}
		if days > 365 {
			return 365
		}
		return days
	}
}

func emptyMailStatsTrend(now time.Time, days int) []MailStatsTrendPoint {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	points := make([]MailStatsTrendPoint, 0, days)
	for i := days - 1; i >= 0; i-- {
		points = append(points, MailStatsTrendPoint{Date: today.AddDate(0, 0, -i).Format("2006-01-02")})
	}
	return points
}

func mailStatsDistribution(rows []MailStatsFolderCount, attachmentMessages, starred int64) []MailStatsDistributionItem {
	roles := map[string]int64{}
	for _, row := range rows {
		roles[strings.ToLower(row.Role)] += row.Count
	}
	return []MailStatsDistributionItem{
		{Key: "inbox", Label: "收件箱", Count: roles["inbox"]},
		{Key: "archive", Label: "已归档", Count: roles["archive"]},
		{Key: "spam", Label: "垃圾邮件", Count: roles["spam"]},
		{Key: "trash", Label: "已删除", Count: roles["trash"]},
		{Key: "attachments", Label: "有附件", Count: attachmentMessages},
		{Key: "starred", Label: "已加旗标", Count: starred},
	}
}

func (a *App) loadMailStatsTrend(ctx context.Context, where string, args []any, days int, trend []MailStatsTrendPoint) error {
	start := ""
	if len(trend) > 0 {
		start = trend[0].Date + "T00:00:00Z"
	}
	queryArgs := append(append([]any{}, args...), start)
	rows, err := a.db.QueryContext(ctx, `SELECT substr(CASE WHEN f.role='sent' THEN m.sent_at ELSE m.received_at END, 1, 10),
			COALESCE(SUM(CASE WHEN f.role='sent' THEN 0 ELSE 1 END),0),
			COALESCE(SUM(CASE WHEN f.role='sent' THEN 1 ELSE 0 END),0)
		FROM mailboxes mb JOIN messages m ON m.mailbox_id=mb.id JOIN folders f ON f.id=m.folder_id
		WHERE `+where+` AND f.role<>'drafts' AND (CASE WHEN f.role='sent' THEN m.sent_at ELSE m.received_at END)>=?
		GROUP BY 1`, queryArgs...)
	if err != nil {
		return err
	}
	defer rows.Close()
	byDate := map[string]*MailStatsTrendPoint{}
	for i := range trend {
		byDate[trend[i].Date] = &trend[i]
	}
	for rows.Next() {
		var date string
		var incoming, outgoing int64
		if err := rows.Scan(&date, &incoming, &outgoing); err != nil {
			return err
		}
		if point := byDate[date]; point != nil {
			point.Incoming = incoming
			point.Outgoing = outgoing
		}
	}
	return rows.Err()
}

func (a *App) mailStatsTopContacts(ctx context.Context, where string, args []any) ([]MailStatsContact, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT f.role,m.from_addr,m.to_addrs,m.cc_addrs,m.bcc_addrs
		FROM mailboxes mb JOIN messages m ON m.mailbox_id=mb.id JOIN folders f ON f.id=m.folder_id
		WHERE `+where+`
		ORDER BY m.received_at DESC LIMIT 2000`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int64{}
	for rows.Next() {
		var role, from, toJSON, ccJSON, bccJSON string
		if err := rows.Scan(&role, &from, &toJSON, &ccJSON, &bccJSON); err != nil {
			return nil, err
		}
		if strings.EqualFold(role, "sent") {
			for _, email := range append(append(mailStatsEmailList(toJSON), mailStatsEmailList(ccJSON)...), mailStatsEmailList(bccJSON)...) {
				if email != "" {
					counts[email]++
				}
			}
			continue
		}
		if email := normalizeEmail(from); email != "" && strings.Contains(email, "@") {
			counts[email]++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	items := make([]MailStatsContact, 0, len(counts))
	for email, count := range counts {
		items = append(items, MailStatsContact{Email: email, Count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count == items[j].Count {
			return items[i].Email < items[j].Email
		}
		return items[i].Count > items[j].Count
	})
	if len(items) > 10 {
		items = items[:10]
	}
	return items, nil
}

func mailStatsEmailList(raw string) []string {
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		email := normalizeEmail(value)
		if email == "" || !strings.Contains(email, "@") || seen[email] {
			continue
		}
		seen[email] = true
		out = append(out, email)
	}
	return out
}

func (a *App) handleMailCleanup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MailboxID string `json:"mailboxId"`
		Target    string `json:"target"`
	}
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	mb, err := a.mailboxForCurrentUserWithID(r, req.MailboxID)
	if err != nil {
		respondError(w, http.StatusNotFound, "mailbox not found")
		return
	}
	target := strings.TrimSpace(req.Target)
	affected := int64(0)
	switch target {
	case "empty-trash":
		affected, err = a.deleteMessagesInFolder(r.Context(), mb.ID, "Trash")
	case "empty-spam":
		affected, err = a.deleteMessagesInFolder(r.Context(), mb.ID, "Spam")
	case "archive-read-inbox":
		affected, err = a.archiveReadInbox(r.Context(), mb.ID)
	default:
		badRequest(w, errors.New("invalid cleanup target"))
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to cleanup messages")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true, "affected": affected})
}

func (a *App) optionalMailboxIDForUser(r *http.Request, mailboxID string) (string, bool) {
	mailboxID = strings.TrimSpace(mailboxID)
	if mailboxID == "" || mailboxID == "all" {
		return "", true
	}
	_, err := a.mailboxForCurrentUserWithID(r, mailboxID)
	return mailboxID, err == nil
}

func (a *App) deleteMessagesInFolder(ctx context.Context, mailboxID, folder string) (int64, error) {
	folderID, err := a.ensureFolder(ctx, mailboxID, folder)
	if err != nil {
		return 0, err
	}
	rows, err := a.db.QueryContext(ctx, `SELECT id FROM messages WHERE mailbox_id=? AND folder_id=?`, mailboxID, folderID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	for _, id := range ids {
		a.deleteMessage(ctx, id)
	}
	return int64(len(ids)), nil
}

func (a *App) archiveReadInbox(ctx context.Context, mailboxID string) (int64, error) {
	inboxID, err := a.ensureFolder(ctx, mailboxID, "Inbox")
	if err != nil {
		return 0, err
	}
	archiveID, err := a.ensureFolder(ctx, mailboxID, "Archive")
	if err != nil {
		return 0, err
	}
	rows, err := a.db.QueryContext(ctx, `SELECT id FROM messages WHERE mailbox_id=? AND folder_id=? AND is_read=1`, mailboxID, inboxID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	for _, id := range ids {
		if err := a.moveMessageMaildir(ctx, id, archiveID); err != nil {
			return 0, err
		}
	}
	return int64(len(ids)), nil
}

func scanContact(row messageSummaryScanner) (Contact, error) {
	var item Contact
	var created string
	err := row.Scan(&item.ID, &item.UserID, &item.Name, &item.Email, &item.Note, &created)
	item.CreatedAt = parseTime(created)
	return item, err
}

func scanSignature(row messageSummaryScanner) (MailSignature, error) {
	var item MailSignature
	var isDefault int
	var created, updated string
	err := row.Scan(&item.ID, &item.UserID, &item.MailboxID, &item.Name, &item.Content, &isDefault, &created, &updated)
	item.IsDefault = intBool(isDefault)
	item.CreatedAt = parseTime(created)
	item.UpdatedAt = parseTime(updated)
	return item, err
}

func (a *App) normalizeSignatureInput(w http.ResponseWriter, r *http.Request, userID, rawMailboxID, rawName, rawContent string) (string, string, string, bool) {
	mailboxID := strings.TrimSpace(rawMailboxID)
	if mailboxID != "" {
		if _, err := a.mailboxForUserByID(r.Context(), userID, mailboxID); err != nil {
			respondError(w, http.StatusForbidden, "mailbox not found")
			return "", "", "", false
		}
	}
	name := strings.TrimSpace(rawName)
	if name == "" {
		badRequest(w, errors.New("signature name is required"))
		return "", "", "", false
	}
	if len([]rune(name)) > 80 {
		badRequest(w, errors.New("signature name is too long"))
		return "", "", "", false
	}
	content := strings.TrimSpace(rawContent)
	if content == "" {
		badRequest(w, errors.New("signature content is required"))
		return "", "", "", false
	}
	if len([]rune(content)) > 5000 {
		badRequest(w, errors.New("signature content is too long"))
		return "", "", "", false
	}
	return mailboxID, name, content, true
}

func (a *App) signatureByID(ctx context.Context, userID, id string) (MailSignature, error) {
	row := a.db.QueryRowContext(ctx, `SELECT id,user_id,mailbox_id,name,content,is_default,created_at,updated_at FROM mail_signatures WHERE id=? AND user_id=?`, id, userID)
	return scanSignature(row)
}

func (a *App) mailboxForUserByID(ctx context.Context, userID, mailboxID string) (*Mailbox, error) {
	row := a.db.QueryRowContext(ctx, `SELECT id,user_id,domain_id,local_part,address,display_name,quota_mb,status,created_at FROM mailboxes WHERE id=? AND user_id=? AND status='active'`, mailboxID, userID)
	var m Mailbox
	var created string
	if err := row.Scan(&m.ID, &m.UserID, &m.DomainID, &m.LocalPart, &m.Address, &m.DisplayName, &m.QuotaMB, &m.Status, &created); err != nil {
		return nil, err
	}
	m.CreatedAt = parseTime(created)
	return &m, nil
}

func (a *App) defaultSignatureForMailbox(ctx context.Context, userID, mailboxID string) (MailSignature, error) {
	row := a.db.QueryRowContext(ctx, `SELECT id,user_id,mailbox_id,name,content,is_default,created_at,updated_at
		FROM mail_signatures
		WHERE user_id=? AND is_default=1 AND (mailbox_id=? OR mailbox_id='')
		ORDER BY CASE WHEN mailbox_id=? THEN 0 ELSE 1 END, updated_at DESC
		LIMIT 1`, userID, mailboxID, mailboxID)
	return scanSignature(row)
}

func scanRule(row messageSummaryScanner) (MailRule, error) {
	var item MailRule
	var enabled, applyToExisting, stopProcessing int
	var created string
	var conditionsJSON, actionsJSON string
	err := row.Scan(&item.ID, &item.UserID, &item.MailboxID, &item.Name, &item.MatchMode, &conditionsJSON, &actionsJSON, &item.FromContains, &item.SubjectContains, &item.Action, &applyToExisting, &stopProcessing, &enabled, &created)
	if err == nil {
		item.Conditions = decodeRuleConditions(conditionsJSON, item.FromContains, item.SubjectContains)
		item.Actions = decodeRuleActions(actionsJSON, item.Action)
		item.MatchMode = normalizeRuleMatchMode(item.MatchMode)
	}
	item.ApplyToExisting = intBool(applyToExisting)
	item.StopProcessing = intBool(stopProcessing)
	item.Enabled = intBool(enabled)
	item.CreatedAt = parseTime(created)
	return item, err
}

func scanBlockedSender(row messageSummaryScanner) (BlockedSender, error) {
	var item BlockedSender
	var created string
	err := row.Scan(&item.ID, &item.UserID, &item.MailboxID, &item.Email, &item.Reason, &created)
	item.CreatedAt = parseTime(created)
	return item, err
}

func (a *App) applyInboundControls(ctx context.Context, messageID, mailboxID, from, subject string) {
	var userID string
	if err := a.db.QueryRowContext(ctx, `SELECT user_id FROM mailboxes WHERE id=?`, mailboxID).Scan(&userID); err != nil {
		return
	}
	if a.senderBlocked(ctx, userID, mailboxID, from) {
		a.moveBlockedMessageToSpam(ctx, messageID, mailboxID)
		return
	}
	rows, err := a.db.QueryContext(ctx, `SELECT id,user_id,mailbox_id,name,match_mode,conditions_json,actions_json,from_contains,subject_contains,action,apply_to_existing,stop_processing,enabled,created_at FROM mail_rules WHERE user_id=? AND (mailbox_id='' OR mailbox_id=?) AND enabled=1 ORDER BY created_at DESC`, userID, mailboxID)
	if err != nil {
		return
	}
	rules := []MailRule{}
	for rows.Next() {
		rule, err := scanRule(rows)
		if err == nil {
			rules = append(rules, rule)
		}
	}
	rows.Close()
	msg := ruleMessage{ID: messageID, MailboxID: mailboxID, From: from, Subject: subject}
	msg, ok := a.ruleMessageByID(ctx, messageID)
	if !ok {
		msg = ruleMessage{ID: messageID, MailboxID: mailboxID, From: from, Subject: subject}
	}
	for _, rule := range rules {
		if !ruleMatches(rule, msg) {
			continue
		}
		if err := a.applyRuleActions(ctx, mailboxID, messageID, rule.Actions); err != nil {
			continue
		}
		if rule.StopProcessing {
			break
		}
	}
	if a.senderBlocked(ctx, userID, mailboxID, from) {
		a.moveBlockedMessageToSpam(ctx, messageID, mailboxID)
	}
}

func (a *App) senderBlocked(ctx context.Context, userID, mailboxID, from string) bool {
	from = normalizeEmail(from)
	if from == "" {
		return false
	}
	var blocked int
	_ = a.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM blocked_senders WHERE user_id=? AND (mailbox_id='' OR mailbox_id=?) AND email=?`, userID, mailboxID, from).Scan(&blocked)
	return blocked > 0
}

func (a *App) moveBlockedMessageToSpam(ctx context.Context, messageID, mailboxID string) {
	spamID, err := a.ensureFolder(ctx, mailboxID, "Spam")
	if err != nil {
		return
	}
	_ = a.moveMessageMaildir(ctx, messageID, spamID)
}

type ruleMessage struct {
	ID              string
	MailboxID       string
	From            string
	To              string
	CC              string
	Subject         string
	Snippet         string
	BodyText        string
	AttachmentNames string
	SizeBytes       int64
	ReceivedAt      time.Time
}

func (a *App) ruleMessageByID(ctx context.Context, messageID string) (ruleMessage, bool) {
	var msg ruleMessage
	var toAddrs, ccAddrs, receivedAt string
	err := a.db.QueryRowContext(ctx, `SELECT id,COALESCE(mailbox_id,''),from_addr,to_addrs,cc_addrs,subject,snippet,body_text,size_bytes,received_at FROM messages WHERE id=?`, messageID).
		Scan(&msg.ID, &msg.MailboxID, &msg.From, &toAddrs, &ccAddrs, &msg.Subject, &msg.Snippet, &msg.BodyText, &msg.SizeBytes, &receivedAt)
	if err != nil {
		return ruleMessage{}, false
	}
	msg.To = ruleAddressText(toAddrs)
	msg.CC = ruleAddressText(ccAddrs)
	msg.ReceivedAt = parseTime(receivedAt)
	msg.AttachmentNames = a.ruleAttachmentNames(ctx, messageID)
	return msg, true
}

func ruleAddressText(raw string) string {
	var items []string
	if strings.TrimSpace(raw) != "" && json.Unmarshal([]byte(raw), &items) == nil {
		return strings.Join(items, " ")
	}
	return raw
}

func (a *App) ruleAttachmentNames(ctx context.Context, messageID string) string {
	rows, err := a.db.QueryContext(ctx, `SELECT filename,content_type FROM attachments WHERE message_id=? ORDER BY filename`, messageID)
	if err != nil {
		return ""
	}
	defer rows.Close()
	parts := []string{}
	for rows.Next() {
		var filename, contentType string
		if err := rows.Scan(&filename, &contentType); err != nil {
			return strings.Join(parts, " ")
		}
		parts = append(parts, filename)
	}
	return strings.Join(parts, " ")
}

func normalizeRuleConditions(items []MailRuleCondition, legacyFrom, legacySubject string) []MailRuleCondition {
	if len(items) == 0 {
		if strings.TrimSpace(legacyFrom) != "" {
			items = append(items, MailRuleCondition{Field: "from", Operator: "contains", Value: legacyFrom})
		}
		if strings.TrimSpace(legacySubject) != "" {
			items = append(items, MailRuleCondition{Field: "subject", Operator: "contains", Value: legacySubject})
		}
	}
	out := []MailRuleCondition{}
	for _, item := range items {
		if normalized, ok := normalizeRuleCondition(item); ok {
			out = append(out, normalized)
		}
	}
	return out
}

func normalizeRuleCondition(item MailRuleCondition) (MailRuleCondition, bool) {
	matchMode := normalizeRuleMatchMode(item.MatchMode)
	if len(item.Conditions) > 0 {
		children := normalizeRuleConditions(item.Conditions, "", "")
		if len(children) == 0 {
			return MailRuleCondition{}, false
		}
		return MailRuleCondition{MatchMode: matchMode, Conditions: children}, true
	}
	field := strings.ToLower(strings.TrimSpace(item.Field))
	operator := strings.ToLower(strings.TrimSpace(item.Operator))
	value := strings.TrimSpace(item.Value)
	if value == "" {
		return MailRuleCondition{}, false
	}
	switch field {
	case "from", "to", "cc", "subject", "body", "attachment", "size", "date":
	default:
		return MailRuleCondition{}, false
	}
	if operator == "" {
		operator = "contains"
	}
	if !validRuleConditionOperator(field, operator) {
		return MailRuleCondition{}, false
	}
	return MailRuleCondition{Field: field, Operator: operator, Value: value}, true
}

func validRuleConditionOperator(field, operator string) bool {
	switch field {
	case "size":
		return operator == "gt" || operator == "gte" || operator == "lt" || operator == "lte" || operator == "equals" || operator == "not-equals"
	case "date":
		return operator == "before" || operator == "after" || operator == "on" || operator == "equals" || operator == "not-equals"
	default:
		return operator == "contains" || operator == "not-contains" || operator == "equals" || operator == "not-equals" || operator == "starts-with" || operator == "ends-with"
	}
}

func normalizeRuleMatchMode(matchMode string) string {
	switch strings.ToLower(strings.TrimSpace(matchMode)) {
	case "any", "or":
		return "any"
	case "all", "and":
		return "all"
	default:
		return "all"
	}
}

func normalizeRuleActions(items []MailRuleAction, legacyAction string) []MailRuleAction {
	if len(items) == 0 && strings.TrimSpace(legacyAction) != "" {
		items = append(items, MailRuleAction{Type: strings.TrimSpace(legacyAction)})
	}
	out := []MailRuleAction{}
	for _, item := range items {
		typ := strings.ToLower(strings.TrimSpace(item.Type))
		value := strings.TrimSpace(item.Value)
		labelID := strings.TrimSpace(item.LabelID)
		if typ != "archive" && typ != "trash" && typ != "star" && typ != "mark-read" && typ != "label" && typ != "move" && typ != "forward" {
			continue
		}
		if typ == "label" && value == "" && labelID == "" {
			continue
		}
		if typ == "move" && value == "" {
			continue
		}
		if typ == "forward" && value == "" {
			continue
		}
		out = append(out, MailRuleAction{Type: typ, Value: value, LabelID: labelID})
	}
	return out
}

func (a *App) cleanRuleActions(ctx context.Context, userID string, actions []MailRuleAction) ([]MailRuleAction, error) {
	out := make([]MailRuleAction, 0, len(actions))
	for _, action := range actions {
		if action.Type == "forward" {
			targets, err := a.cleanForwardingTargets(ctx, userID, splitRuleForwardTargets(action.Value))
			if err != nil {
				return nil, err
			}
			if len(targets) == 0 {
				continue
			}
			action.Value = strings.Join(targets, ", ")
		}
		out = append(out, action)
	}
	return out, nil
}

func legacyConditionValue(items []MailRuleCondition, field string) string {
	for _, item := range items {
		if item.Field == field && item.Operator == "contains" {
			return item.Value
		}
	}
	return ""
}

func decodeRuleConditions(raw, legacyFrom, legacySubject string) []MailRuleCondition {
	var items []MailRuleCondition
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &items)
	}
	return normalizeRuleConditions(items, legacyFrom, legacySubject)
}

func decodeRuleActions(raw, legacyAction string) []MailRuleAction {
	var items []MailRuleAction
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &items)
	}
	return normalizeRuleActions(items, legacyAction)
}

func ruleMatches(rule MailRule, msg ruleMessage) bool {
	conditions := rule.Conditions
	if len(conditions) == 0 {
		conditions = normalizeRuleConditions(nil, rule.FromContains, rule.SubjectContains)
	}
	if len(conditions) == 0 {
		return false
	}
	matchMode := normalizeRuleMatchMode(rule.MatchMode)
	return ruleConditionsMatch(conditions, matchMode, msg)
}

func ruleConditionsMatch(conditions []MailRuleCondition, matchMode string, msg ruleMessage) bool {
	matched := 0
	for _, condition := range conditions {
		if ruleConditionMatches(condition, msg) {
			matched++
			if matchMode == "any" {
				return true
			}
		} else if matchMode == "all" {
			return false
		}
	}
	return matched == len(conditions)
}

func ruleConditionMatches(condition MailRuleCondition, msg ruleMessage) bool {
	if len(condition.Conditions) > 0 {
		return ruleConditionsMatch(condition.Conditions, normalizeRuleMatchMode(condition.MatchMode), msg)
	}
	var source string
	switch strings.ToLower(strings.TrimSpace(condition.Field)) {
	case "from":
		source = msg.From
	case "to":
		source = msg.To
	case "cc":
		source = msg.CC
	case "subject":
		source = msg.Subject
	case "body":
		source = msg.BodyText
		if source == "" {
			source = msg.Snippet
		}
	case "attachment":
		source = msg.AttachmentNames
	case "size":
		return ruleNumericConditionMatches(condition, msg.SizeBytes)
	case "date":
		return ruleDateConditionMatches(condition, msg.ReceivedAt)
	default:
		return false
	}
	source = strings.ToLower(source)
	value := strings.ToLower(condition.Value)
	switch strings.ToLower(strings.TrimSpace(condition.Operator)) {
	case "contains":
		return strings.Contains(source, value)
	case "not-contains":
		return !strings.Contains(source, value)
	case "equals":
		return source == value
	case "not-equals":
		return source != value
	case "starts-with":
		return strings.HasPrefix(source, value)
	case "ends-with":
		return strings.HasSuffix(source, value)
	default:
		return strings.Contains(source, value)
	}
}

func ruleNumericConditionMatches(condition MailRuleCondition, source int64) bool {
	value, ok := parseRuleSizeValue(condition.Value)
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(condition.Operator)) {
	case "gt":
		return source > value
	case "gte":
		return source >= value
	case "lt":
		return source < value
	case "lte":
		return source <= value
	case "equals":
		return source == value
	case "not-equals":
		return source != value
	default:
		return false
	}
}

func parseRuleSizeValue(raw string) (int64, bool) {
	value := strings.ToLower(strings.TrimSpace(raw))
	multiplier := int64(1)
	for _, suffix := range []struct {
		text       string
		multiplier int64
	}{
		{"kb", 1024},
		{"k", 1024},
		{"mb", 1024 * 1024},
		{"m", 1024 * 1024},
		{"gb", 1024 * 1024 * 1024},
		{"g", 1024 * 1024 * 1024},
		{"b", 1},
	} {
		if strings.HasSuffix(value, suffix.text) {
			multiplier = suffix.multiplier
			value = strings.TrimSpace(strings.TrimSuffix(value, suffix.text))
			break
		}
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return n * multiplier, true
}

func ruleDateConditionMatches(condition MailRuleCondition, source time.Time) bool {
	if source.IsZero() {
		return false
	}
	target, ok := parseRuleDateValue(condition.Value)
	if !ok {
		return false
	}
	source = source.UTC()
	switch strings.ToLower(strings.TrimSpace(condition.Operator)) {
	case "before", "lt":
		return source.Before(target)
	case "after", "gt":
		return source.After(target)
	case "on", "equals":
		y1, m1, d1 := source.Date()
		y2, m2, d2 := target.Date()
		return y1 == y2 && m1 == m2 && d1 == d2
	case "not-equals":
		y1, m1, d1 := source.Date()
		y2, m2, d2 := target.Date()
		return y1 != y2 || m1 != m2 || d1 != d2
	default:
		return false
	}
}

func parseRuleDateValue(raw string) (time.Time, bool) {
	value := strings.TrimSpace(raw)
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func (a *App) applyRuleActions(ctx context.Context, mailboxID, messageID string, actions []MailRuleAction) error {
	now := a.now().UTC().Format(time.RFC3339Nano)
	for _, action := range normalizeRuleActions(actions, "") {
		switch action.Type {
		case "archive":
			folderID, err := a.ensureFolder(ctx, mailboxID, "Archive")
			if err != nil {
				return err
			}
			if err := a.moveMessageMaildir(ctx, messageID, folderID); err != nil {
				return err
			}
		case "trash":
			folderID, err := a.ensureFolder(ctx, mailboxID, "Trash")
			if err != nil {
				return err
			}
			if err := a.moveMessageMaildir(ctx, messageID, folderID); err != nil {
				return err
			}
		case "move":
			target, err := normalizeFolderNameForUser(action.Value)
			if err != nil {
				return err
			}
			var folderID string
			if isSystemFolderName(target) {
				folderID, err = a.ensureFolder(ctx, mailboxID, target)
			} else {
				folderID, err = a.ensureCustomFolder(ctx, mailboxID, target, "auto")
			}
			if err != nil {
				return err
			}
			if err := a.moveMessageMaildir(ctx, messageID, folderID); err != nil {
				return err
			}
		case "star":
			starred := true
			if err := a.updateMessageMaildirFlags(ctx, messageID, nil, &starred); err != nil {
				return err
			}
			modSeq, err := a.updateMessageModSeq(ctx, messageID, "")
			if err != nil {
				return err
			}
			if _, err := a.db.ExecContext(ctx, `UPDATE messages SET is_starred=1, imap_modseq=CASE WHEN ? > 0 THEN ? ELSE imap_modseq END, updated_at=? WHERE id=?`, modSeq, modSeq, now, messageID); err != nil {
				return err
			}
		case "mark-read":
			read := true
			if err := a.updateMessageMaildirFlags(ctx, messageID, &read, nil); err != nil {
				return err
			}
			modSeq, err := a.updateMessageModSeq(ctx, messageID, "")
			if err != nil {
				return err
			}
			if _, err := a.db.ExecContext(ctx, `UPDATE messages SET is_read=1, imap_modseq=CASE WHEN ? > 0 THEN ? ELSE imap_modseq END, updated_at=? WHERE id=?`, modSeq, modSeq, now, messageID); err != nil {
				return err
			}
		case "label":
			if err := a.applyRuleLabel(ctx, mailboxID, messageID, action); err != nil {
				return err
			}
		case "forward":
			if err := a.processRuleForwarding(ctx, messageID, mailboxID, action); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *App) applyRuleLabel(ctx context.Context, mailboxID, messageID string, action MailRuleAction) error {
	var label MailLabel
	var err error
	if action.LabelID != "" {
		var count int
		_ = a.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM mail_labels WHERE id=? AND mailbox_id=?`, action.LabelID, mailboxID).Scan(&count)
		if count > 0 {
			label.ID = action.LabelID
		}
	}
	if label.ID == "" {
		name := strings.TrimSpace(action.Value)
		if name == "" {
			name = "规则标签"
		}
		label, err = a.ensureLabel(ctx, mailboxID, name, "")
		if err != nil {
			return err
		}
	}
	_, err = a.db.ExecContext(ctx, `INSERT OR IGNORE INTO message_labels(message_id,label_id,created_at) VALUES(?,?,?)`, messageID, label.ID, a.now().UTC().Format(time.RFC3339Nano))
	return err
}

func (a *App) applyRuleToExistingMessages(ctx context.Context, userID, mailboxID string, rule MailRule) (int64, error) {
	args := []any{userID}
	where := `mb.user_id=?`
	if mailboxID != "" {
		where += ` AND m.mailbox_id=?`
		args = append(args, mailboxID)
	}
	rows, err := a.db.QueryContext(ctx, `SELECT m.id FROM messages m JOIN mailboxes mb ON mb.id=m.mailbox_id JOIN folders f ON f.id=m.folder_id WHERE `+where+` AND lower(f.name) NOT IN ('sent','drafts')`, args...)
	if err != nil {
		return 0, err
	}
	messageIDs := []string{}
	var count int64
	for rows.Next() {
		var messageID string
		if err := rows.Scan(&messageID); err != nil {
			rows.Close()
			return count, err
		}
		messageIDs = append(messageIDs, messageID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return count, err
	}
	rows.Close()
	messages := []ruleMessage{}
	for _, messageID := range messageIDs {
		msg, ok := a.ruleMessageByID(ctx, messageID)
		if !ok {
			continue
		}
		if !ruleMatches(rule, msg) {
			continue
		}
		messages = append(messages, msg)
	}
	for _, msg := range messages {
		if err := a.applyRuleActions(ctx, msg.MailboxID, msg.ID, rule.Actions); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}
