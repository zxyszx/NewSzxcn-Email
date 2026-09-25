package app

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

const subNestProvider = "subnest"

type subNestMailbox struct {
	ID          string `json:"id"`
	Address     string `json:"address"`
	DisplayName string `json:"displayName"`
	Status      string `json:"status"`
}

type subNestFolder struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Role       string `json:"role"`
	TotalCount int    `json:"totalCount"`
}

type subNestGrant struct {
	ID              string     `json:"id"`
	ExternalGrantID string     `json:"externalGrantId"`
	MailboxID       string     `json:"mailboxId"`
	MailboxAddress  string     `json:"mailboxAddress"`
	FolderIDs       []string   `json:"folderIds"`
	WindowMinutes   int        `json:"windowMinutes"`
	Status          string     `json:"status"`
	CreatedBy       string     `json:"-"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	ExpiresAt       *time.Time `json:"expiresAt,omitempty"`
	RevokedAt       *time.Time `json:"revokedAt,omitempty"`
	LastAccessedAt  *time.Time `json:"lastAccessedAt,omitempty"`
}

type subNestGrantInput struct {
	ExternalGrantID string   `json:"externalGrantId"`
	MailboxID       string   `json:"mailboxId"`
	FolderIDs       []string `json:"folderIds"`
	WindowMinutes   int      `json:"windowMinutes"`
	ExpiresAt       string   `json:"expiresAt"`
}

func (a *App) handleSubNestMailboxes(w http.ResponseWriter, r *http.Request) {
	limit := parseOpenAPILimit(r, 50, 100)
	sortValue, cursorID, err := parseOpenAPIListCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		badRequest(w, err)
		return
	}
	rows, err := a.db.QueryContext(r.Context(), `SELECT mb.id,mb.address,mb.display_name,mb.status
		FROM mailboxes mb JOIN users u ON u.id=mb.user_id JOIN domains d ON d.id=mb.domain_id
		WHERE mb.status='active' AND u.disabled=0 AND d.status='active'
		AND (?='' OR mb.address>? OR (mb.address=? AND mb.id>?))
		ORDER BY mb.address,mb.id LIMIT ?`, sortValue, sortValue, sortValue, cursorID, limit+1)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list mailboxes")
		return
	}
	defer rows.Close()
	items := []subNestMailbox{}
	for rows.Next() {
		var item subNestMailbox
		if err := rows.Scan(&item.ID, &item.Address, &item.DisplayName, &item.Status); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to scan mailboxes")
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list mailboxes")
		return
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		next = encodeOpenAPIListCursor(last.Address, last.ID)
	}
	setSubNestHeaders(w)
	respondJSON(w, http.StatusOK, map[string]any{"items": items, "nextCursor": next})
}

func (a *App) handleSubNestFolders(w http.ResponseWriter, r *http.Request) {
	mailboxID := strings.TrimSpace(chi.URLParam(r, "id"))
	if !a.subNestMailboxActive(r.Context(), mailboxID) {
		respondError(w, http.StatusNotFound, "mailbox not found")
		return
	}
	rows, err := a.db.QueryContext(r.Context(), `SELECT f.id,f.name,f.role,COUNT(m.id)
		FROM folders f LEFT JOIN messages m ON m.folder_id=f.id WHERE f.mailbox_id=?
		GROUP BY f.id,f.name,f.role,f.sort_order,f.created_at ORDER BY CASE WHEN lower(f.name)='inbox' THEN 0 ELSE 1 END,f.sort_order,f.created_at,f.name`, mailboxID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list folders")
		return
	}
	defer rows.Close()
	items := []subNestFolder{}
	for rows.Next() {
		var item subNestFolder
		if err := rows.Scan(&item.ID, &item.Name, &item.Role, &item.TotalCount); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to scan folders")
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list folders")
		return
	}
	setSubNestHeaders(w)
	respondJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *App) handleCreateSubNestGrant(w http.ResponseWriter, r *http.Request) {
	var req subNestGrantInput
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	req.ExternalGrantID = strings.TrimSpace(req.ExternalGrantID)
	if req.ExternalGrantID == "" || len(req.ExternalGrantID) > 160 {
		badRequest(w, errors.New("externalGrantId is required and cannot exceed 160 characters"))
		return
	}
	folderIDs, expiresAt, err := a.validateSubNestGrantInput(r.Context(), req.MailboxID, req.FolderIDs, req.WindowMinutes, req.ExpiresAt)
	if err != nil {
		badRequest(w, err)
		return
	}
	user := currentUser(r)
	var existing string
	if err := a.db.QueryRowContext(r.Context(), `SELECT id FROM inbox_integration_grants WHERE provider=? AND created_by=? AND external_grant_id=?`, subNestProvider, user.ID, req.ExternalGrantID).Scan(&existing); err == nil {
		grant, loadErr := a.subNestGrantByID(r.Context(), user.ID, existing, false)
		if loadErr == nil && subNestGrantMatches(grant, strings.TrimSpace(req.MailboxID), folderIDs, req.WindowMinutes, expiresAt) {
			setSubNestHeaders(w)
			respondJSON(w, http.StatusOK, grant)
			return
		}
		respondError(w, http.StatusConflict, "externalGrantId already exists with different settings")
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		respondError(w, http.StatusInternalServerError, "failed to check integration grant")
		return
	}
	now := a.now().UTC()
	expires := ""
	if expiresAt != nil {
		expires = expiresAt.UTC().Format(time.RFC3339Nano)
	}
	id := newID("igr")
	_, err = a.db.ExecContext(r.Context(), `INSERT INTO inbox_integration_grants(id,provider,external_grant_id,mailbox_id,folder_ids,window_minutes,status,created_by,created_at,updated_at,expires_at,revoked_at,last_accessed_at)
		VALUES(?,?,?,?,?,?,'active',?,?,?,?, '', '')`, id, subNestProvider, req.ExternalGrantID, strings.TrimSpace(req.MailboxID), jsonEncode(folderIDs), req.WindowMinutes, user.ID, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), expires)
	if err != nil {
		// A concurrent retry may have inserted the same idempotency key after
		// our initial lookup. Return that grant only when its scope is identical.
		if lookupErr := a.db.QueryRowContext(r.Context(), `SELECT id FROM inbox_integration_grants WHERE provider=? AND created_by=? AND external_grant_id=?`, subNestProvider, user.ID, req.ExternalGrantID).Scan(&existing); lookupErr == nil {
			grant, loadErr := a.subNestGrantByID(r.Context(), user.ID, existing, false)
			if loadErr == nil && subNestGrantMatches(grant, strings.TrimSpace(req.MailboxID), folderIDs, req.WindowMinutes, expiresAt) {
				respondJSON(w, http.StatusOK, grant)
				return
			}
			respondError(w, http.StatusConflict, "externalGrantId already exists with different settings")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to create integration grant")
		return
	}
	grant, err := a.subNestGrantByID(r.Context(), user.ID, id, false)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load integration grant")
		return
	}
	a.recordSubNestAudit(r.Context(), grant, "grant.created", "")
	setSubNestHeaders(w)
	respondJSON(w, http.StatusCreated, grant)
}

func (a *App) handleGetSubNestGrant(w http.ResponseWriter, r *http.Request) {
	grant, err := a.subNestGrantByID(r.Context(), currentUser(r).ID, chi.URLParam(r, "id"), false)
	if err != nil {
		respondError(w, http.StatusNotFound, "integration grant not found")
		return
	}
	setSubNestHeaders(w)
	respondJSON(w, http.StatusOK, grant)
}

func (a *App) handleUpdateSubNestGrant(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	grant, err := a.subNestGrantByID(r.Context(), user.ID, chi.URLParam(r, "id"), true)
	if err != nil {
		respondSubNestGrantError(w, err)
		return
	}
	var req subNestGrantInput
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	mailboxID := grant.MailboxID
	if strings.TrimSpace(req.MailboxID) != "" && strings.TrimSpace(req.MailboxID) != mailboxID {
		badRequest(w, errors.New("mailboxId cannot be changed; create a new grant"))
		return
	}
	folderIDs, expiresAt, err := a.validateSubNestGrantInput(r.Context(), mailboxID, req.FolderIDs, req.WindowMinutes, req.ExpiresAt)
	if err != nil {
		badRequest(w, err)
		return
	}
	expires := ""
	if expiresAt != nil {
		expires = expiresAt.UTC().Format(time.RFC3339Nano)
	}
	_, err = a.db.ExecContext(r.Context(), `UPDATE inbox_integration_grants SET folder_ids=?,window_minutes=?,expires_at=?,updated_at=? WHERE id=? AND created_by=? AND status='active'`, jsonEncode(folderIDs), req.WindowMinutes, expires, a.now().UTC().Format(time.RFC3339Nano), grant.ID, user.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update integration grant")
		return
	}
	grant, err = a.subNestGrantByID(r.Context(), user.ID, grant.ID, true)
	if err != nil {
		respondSubNestGrantError(w, err)
		return
	}
	a.recordSubNestAudit(r.Context(), grant, "grant.updated", "")
	setSubNestHeaders(w)
	respondJSON(w, http.StatusOK, grant)
}

func (a *App) handleRevokeSubNestGrant(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	grant, err := a.subNestGrantByID(r.Context(), user.ID, chi.URLParam(r, "id"), false)
	if err != nil {
		respondError(w, http.StatusNotFound, "integration grant not found")
		return
	}
	now := a.now().UTC().Format(time.RFC3339Nano)
	if grant.Status != "revoked" {
		if _, err := a.db.ExecContext(r.Context(), `UPDATE inbox_integration_grants SET status='revoked',revoked_at=?,updated_at=? WHERE id=? AND created_by=?`, now, now, grant.ID, user.ID); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to revoke integration grant")
			return
		}
	}
	a.recordSubNestAudit(r.Context(), grant, "grant.revoked", "")
	setSubNestHeaders(w)
	respondJSON(w, http.StatusOK, map[string]any{"ok": true, "id": grant.ID, "status": "revoked"})
}

func (a *App) handleSubNestMessages(w http.ResponseWriter, r *http.Request) {
	grant, err := a.activeSubNestGrant(r)
	if err != nil {
		respondSubNestGrantError(w, err)
		return
	}
	limit := parseOpenAPILimit(r, 30, 100)
	offset, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("cursor")))
	if err != nil || offset < 0 || offset > 100000 {
		offset = 0
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(grant.FolderIDs)), ",")
	query := `SELECT m.id,m.folder_id,f.name,m.subject,m.from_addr,m.from_name,m.received_at,m.snippet,m.has_attachments
		FROM messages m JOIN folders f ON f.id=m.folder_id WHERE m.mailbox_id=? AND m.folder_id IN (` + placeholders + `)`
	args := []any{grant.MailboxID}
	for _, folderID := range grant.FolderIDs {
		args = append(args, folderID)
	}
	if grant.WindowMinutes > 0 {
		query += ` AND m.received_at>=?`
		args = append(args, a.now().UTC().Add(-time.Duration(grant.WindowMinutes)*time.Minute).Format(time.RFC3339Nano))
	}
	query += ` ORDER BY m.received_at DESC,m.id DESC LIMIT ? OFFSET ?`
	args = append(args, limit+1, offset)
	rows, err := a.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load messages")
		return
	}
	defer rows.Close()
	items := []sharedInboxMessage{}
	for rows.Next() {
		var item sharedInboxMessage
		var received string
		var hasAttachments int
		if err := rows.Scan(&item.ID, &item.FolderID, &item.Folder, &item.Subject, &item.From, &item.FromName, &received, &item.Snippet, &hasAttachments); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to scan messages")
			return
		}
		item.ReceivedAt = parseTime(received)
		item.HasAttachments = intBool(hasAttachments)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load messages")
		return
	}
	response := map[string]any{"items": items, "mailboxId": grant.MailboxID, "mailboxAddress": grant.MailboxAddress, "windowMinutes": grant.WindowMinutes, "grantId": grant.ID}
	if len(items) > limit {
		response["items"] = items[:limit]
		response["nextCursor"] = strconv.Itoa(offset + limit)
	}
	a.touchSubNestGrant(r.Context(), grant, "messages.list", "")
	setSubNestHeaders(w)
	respondJSON(w, http.StatusOK, response)
}

func (a *App) handleSubNestMessage(w http.ResponseWriter, r *http.Request) {
	grant, err := a.activeSubNestGrant(r)
	if err != nil {
		respondSubNestGrantError(w, err)
		return
	}
	messageID := strings.TrimSpace(chi.URLParam(r, "messageId"))
	if !a.subNestMessageAllowed(r.Context(), grant, messageID) {
		respondError(w, http.StatusNotFound, "message not found")
		return
	}
	msg, err := a.messageByID(r.Context(), messageID, true)
	if err != nil {
		respondError(w, http.StatusNotFound, "message not found")
		return
	}
	a.touchSubNestGrant(r.Context(), grant, "messages.get", messageID)
	setSubNestHeaders(w)
	respondJSON(w, http.StatusOK, sharedInboxMessageDetail{
		sharedInboxMessage: sharedInboxMessage{ID: msg.ID, FolderID: msg.FolderID, Folder: msg.Folder, Subject: msg.Subject, From: msg.From, FromName: msg.FromName, ReceivedAt: msg.ReceivedAt, Snippet: msg.Snippet, HasAttachments: msg.HasAttachments},
		BodyText:           msg.BodyText, BodyHTML: msg.BodyHTML, Attachments: msg.Attachments,
	})
}

func (a *App) handleSubNestAttachment(w http.ResponseWriter, r *http.Request) {
	grant, err := a.activeSubNestGrant(r)
	if err != nil {
		respondSubNestGrantError(w, err)
		return
	}
	attachmentID := strings.TrimSpace(chi.URLParam(r, "attachmentId"))
	var messageID, filename, contentType, path string
	var size int64
	err = a.db.QueryRowContext(r.Context(), `SELECT message_id,filename,content_type,size_bytes,storage_path FROM attachments WHERE id=?`, attachmentID).Scan(&messageID, &filename, &contentType, &size, &path)
	if err != nil || !a.subNestMessageAllowed(r.Context(), grant, messageID) {
		respondError(w, http.StatusNotFound, "attachment not found")
		return
	}
	file, err := os.Open(path)
	if err != nil {
		respondError(w, http.StatusNotFound, "attachment not found")
		return
	}
	defer file.Close()
	a.touchSubNestGrant(r.Context(), grant, "attachments.get", attachmentID)
	setSubNestHeaders(w)
	mediaType, _, parseErr := mime.ParseMediaType(contentType)
	if parseErr != nil || mediaType == "" {
		mediaType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mediaType)
	safeFilename := strings.NewReplacer("\r", "", "\n", "").Replace(filename)
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": safeFilename})
	if disposition == "" {
		disposition = `attachment; filename="attachment"`
	}
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	_, _ = io.Copy(w, file)
}

func (a *App) validateSubNestGrantInput(ctx context.Context, mailboxID string, folderIDs []string, windowMinutes int, expiresRaw string) ([]string, *time.Time, error) {
	mailboxID = strings.TrimSpace(mailboxID)
	if !a.subNestMailboxActive(ctx, mailboxID) {
		return nil, nil, errors.New("active mailbox not found")
	}
	if !allowedInboxShareWindows[windowMinutes] {
		return nil, nil, errors.New("invalid windowMinutes")
	}
	folderIDs = cleanIDList(folderIDs)
	if len(folderIDs) == 0 {
		return nil, nil, errors.New("select at least one folder")
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(folderIDs)), ",")
	args := []any{mailboxID}
	for _, folderID := range folderIDs {
		args = append(args, folderID)
	}
	var count int
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM folders WHERE mailbox_id=? AND id IN (`+placeholders+`)`, args...).Scan(&count); err != nil || count != len(folderIDs) {
		return nil, nil, errors.New("one or more folders are invalid")
	}
	var expiresAt *time.Time
	if strings.TrimSpace(expiresRaw) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(expiresRaw))
		if err != nil || !parsed.After(a.now().UTC()) {
			return nil, nil, errors.New("expiresAt must be a future RFC3339 timestamp")
		}
		parsed = parsed.UTC()
		expiresAt = &parsed
	}
	return folderIDs, expiresAt, nil
}

func (a *App) subNestMailboxActive(ctx context.Context, mailboxID string) bool {
	var count int
	err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mailboxes mb JOIN users u ON u.id=mb.user_id JOIN domains d ON d.id=mb.domain_id WHERE mb.id=? AND mb.status='active' AND u.disabled=0 AND d.status='active'`, strings.TrimSpace(mailboxID)).Scan(&count)
	return err == nil && count == 1
}

func (a *App) subNestGrantByID(ctx context.Context, userID, id string, requireActive bool) (subNestGrant, error) {
	var grant subNestGrant
	var foldersJSON, created, updated, expires, revoked, lastAccessed string
	err := a.db.QueryRowContext(ctx, `SELECT g.id,g.external_grant_id,g.mailbox_id,mb.address,g.folder_ids,g.window_minutes,g.status,g.created_by,g.created_at,g.updated_at,g.expires_at,g.revoked_at,g.last_accessed_at
		FROM inbox_integration_grants g JOIN mailboxes mb ON mb.id=g.mailbox_id JOIN users owner ON owner.id=mb.user_id JOIN domains d ON d.id=mb.domain_id
		WHERE g.id=? AND g.provider=? AND g.created_by=? AND mb.status='active' AND owner.disabled=0 AND d.status='active'`, strings.TrimSpace(id), subNestProvider, userID).Scan(
		&grant.ID, &grant.ExternalGrantID, &grant.MailboxID, &grant.MailboxAddress, &foldersJSON, &grant.WindowMinutes, &grant.Status, &grant.CreatedBy, &created, &updated, &expires, &revoked, &lastAccessed)
	if err != nil {
		return grant, err
	}
	grant.FolderIDs = jsonDecodeSlice(foldersJSON)
	grant.CreatedAt, grant.UpdatedAt = parseTime(created), parseTime(updated)
	grant.ExpiresAt = optionalStoredTime(expires)
	grant.RevokedAt = optionalStoredTime(revoked)
	grant.LastAccessedAt = optionalStoredTime(lastAccessed)
	if grant.Status == "active" && grant.ExpiresAt != nil && !grant.ExpiresAt.After(a.now().UTC()) {
		grant.Status = "expired"
	}
	if requireActive {
		if grant.Status != "active" {
			if grant.Status == "expired" {
				return subNestGrant{}, errSubNestGrantExpired
			}
			return subNestGrant{}, errSubNestGrantRevoked
		}
		if len(grant.FolderIDs) == 0 || !allowedInboxShareWindows[grant.WindowMinutes] {
			return subNestGrant{}, errSubNestGrantRevoked
		}
	}
	return grant, nil
}

var (
	errSubNestGrantRevoked = errors.New("integration grant revoked")
	errSubNestGrantExpired = errors.New("integration grant expired")
)

func (a *App) activeSubNestGrant(r *http.Request) (subNestGrant, error) {
	return a.subNestGrantByID(r.Context(), currentUser(r).ID, chi.URLParam(r, "id"), true)
}

func respondSubNestGrantError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errSubNestGrantRevoked):
		respondError(w, http.StatusForbidden, "integration grant revoked")
	case errors.Is(err, errSubNestGrantExpired):
		respondError(w, http.StatusForbidden, "integration grant expired")
	default:
		respondError(w, http.StatusNotFound, "integration grant not found")
	}
}

func (a *App) subNestMessageAllowed(ctx context.Context, grant subNestGrant, messageID string) bool {
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(grant.FolderIDs)), ",")
	query := `SELECT COUNT(*) FROM messages WHERE id=? AND mailbox_id=? AND folder_id IN (` + placeholders + `)`
	args := []any{strings.TrimSpace(messageID), grant.MailboxID}
	for _, folderID := range grant.FolderIDs {
		args = append(args, folderID)
	}
	if grant.WindowMinutes > 0 {
		query += ` AND received_at>=?`
		args = append(args, a.now().UTC().Add(-time.Duration(grant.WindowMinutes)*time.Minute).Format(time.RFC3339Nano))
	}
	var count int
	return a.db.QueryRowContext(ctx, query, args...).Scan(&count) == nil && count == 1
}

func (a *App) touchSubNestGrant(ctx context.Context, grant subNestGrant, action, resourceID string) {
	now := a.now().UTC().Format(time.RFC3339Nano)
	_, _ = a.db.ExecContext(ctx, `UPDATE inbox_integration_grants SET last_accessed_at=? WHERE id=? AND status='active'`, now, grant.ID)
	a.recordSubNestAudit(ctx, grant, action, resourceID)
}

func (a *App) recordSubNestAudit(ctx context.Context, grant subNestGrant, action, resourceID string) {
	_, _ = a.db.ExecContext(ctx, `INSERT INTO inbox_integration_audit(id,provider,grant_id,mailbox_id,action,resource_id,created_by,created_at) VALUES(?,?,?,?,?,?,?,?)`, newID("iga"), subNestProvider, grant.ID, grant.MailboxID, action, strings.TrimSpace(resourceID), grant.CreatedBy, a.now().UTC().Format(time.RFC3339Nano))
}

func optionalStoredTime(value string) *time.Time {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parsed := parseTime(value)
	return &parsed
}

func subNestGrantMatches(grant subNestGrant, mailboxID string, folderIDs []string, windowMinutes int, expiresAt *time.Time) bool {
	if grant.MailboxID != mailboxID || grant.WindowMinutes != windowMinutes || grant.Status != "active" || len(grant.FolderIDs) != len(folderIDs) {
		return false
	}
	folders := make(map[string]struct{}, len(grant.FolderIDs))
	for _, id := range grant.FolderIDs {
		folders[id] = struct{}{}
	}
	for _, id := range folderIDs {
		if _, ok := folders[id]; !ok {
			return false
		}
	}
	if grant.ExpiresAt == nil || expiresAt == nil {
		return grant.ExpiresAt == nil && expiresAt == nil
	}
	return grant.ExpiresAt.Equal(expiresAt.UTC())
}

func setSubNestHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func subNestSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setSubNestHeaders(w)
		next.ServeHTTP(w, r)
	})
}

func requireSystemAdminRole(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := currentUser(r)
		if user == nil || user.Role != "admin" {
			respondError(w, http.StatusForbidden, "system administrator required")
			return
		}
		next.ServeHTTP(w, r)
	})
}
