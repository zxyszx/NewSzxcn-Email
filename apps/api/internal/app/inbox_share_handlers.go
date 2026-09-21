package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

const sharedInboxPageSize = 30

var allowedInboxShareWindows = map[int]bool{0: true, 30: true, 60: true, 360: true, 1440: true, 10080: true}

type inboxShareRecord struct {
	MailboxID      string
	MailboxAddress string
	OwnerID        string
	WindowMinutes  int
	FolderIDs      []string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	LastAccessedAt *time.Time
}

type inboxShareFolder struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Role       string `json:"role"`
	TotalCount int    `json:"totalCount"`
	Selected   bool   `json:"selected"`
}

type inboxShareSettingsResponse struct {
	Enabled        bool               `json:"enabled"`
	MailboxID      string             `json:"mailboxId"`
	MailboxAddress string             `json:"mailboxAddress"`
	WindowMinutes  int                `json:"windowMinutes"`
	FolderIDs      []string           `json:"folderIds"`
	Folders        []inboxShareFolder `json:"folders"`
	ShareURL       string             `json:"shareUrl,omitempty"`
	CreatedAt      *time.Time         `json:"createdAt,omitempty"`
	UpdatedAt      *time.Time         `json:"updatedAt,omitempty"`
	LastAccessedAt *time.Time         `json:"lastAccessedAt,omitempty"`
}

type inboxShareSummary struct {
	MailboxID     string    `json:"mailboxId"`
	WindowMinutes int       `json:"windowMinutes"`
	FolderCount   int       `json:"folderCount"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

func (a *App) handleInboxShareSummaries(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	rows, err := a.db.QueryContext(r.Context(), `SELECT s.mailbox_id,s.window_minutes,s.folder_ids,s.updated_at
		FROM inbox_shares s JOIN mailboxes mb ON mb.id=s.mailbox_id
		WHERE mb.user_id=? AND mb.status='active' ORDER BY s.updated_at DESC`, user.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load inbox share summaries")
		return
	}
	defer rows.Close()
	items := []inboxShareSummary{}
	for rows.Next() {
		var item inboxShareSummary
		var foldersJSON, updated string
		if err := rows.Scan(&item.MailboxID, &item.WindowMinutes, &foldersJSON, &updated); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to scan inbox share summaries")
			return
		}
		item.FolderCount = len(jsonDecodeSlice(foldersJSON))
		item.UpdatedAt = parseTime(updated)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load inbox share summaries")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"items": items})
}

type sharedInboxMessage struct {
	ID             string    `json:"id"`
	FolderID       string    `json:"folderId"`
	Folder         string    `json:"folder"`
	Subject        string    `json:"subject"`
	From           string    `json:"from"`
	FromName       string    `json:"fromName,omitempty"`
	ReceivedAt     time.Time `json:"receivedAt"`
	Snippet        string    `json:"snippet"`
	HasAttachments bool      `json:"hasAttachments"`
}

type sharedInboxMessageDetail struct {
	sharedInboxMessage
	BodyText    string       `json:"bodyText,omitempty"`
	BodyHTML    string       `json:"bodyHtml,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

func (a *App) handleInboxShareSettings(w http.ResponseWriter, r *http.Request) {
	mb, err := a.mailboxForCurrentUserWithID(r, chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusNotFound, "mailbox not found")
		return
	}
	response, err := a.inboxShareSettings(r.Context(), mb)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load inbox share settings")
		return
	}
	respondJSON(w, http.StatusOK, response)
}

func (a *App) handleCreateInboxShare(w http.ResponseWriter, r *http.Request) {
	mb, err := a.mailboxForCurrentUserWithID(r, chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusNotFound, "mailbox not found")
		return
	}
	windowMinutes, folderIDs, ok := a.parseInboxShareInput(w, r, mb.ID)
	if !ok {
		return
	}
	token := "nis_" + randomToken()
	tokenCipher, err := a.encryptBackupPassword(token)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "share link encryption is not configured")
		return
	}
	now := a.now().UTC()
	_, err = a.db.ExecContext(r.Context(), `INSERT INTO inbox_shares(mailbox_id,token_hash,token_cipher,window_minutes,folder_ids,created_by,created_at,updated_at,last_accessed_at)
		VALUES(?,?,?,?,?,?,?,?, '')
		ON CONFLICT(mailbox_id) DO UPDATE SET token_hash=excluded.token_hash,token_cipher=excluded.token_cipher,window_minutes=excluded.window_minutes,folder_ids=excluded.folder_ids,created_by=excluded.created_by,updated_at=excluded.updated_at,last_accessed_at=''`,
		mb.ID, hashToken(token), tokenCipher, windowMinutes, jsonEncode(folderIDs), currentUser(r).ID, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create inbox share")
		return
	}
	response, err := a.inboxShareSettings(r.Context(), mb)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load inbox share settings")
		return
	}
	response.ShareURL = a.inboxShareURL(r, token)
	respondJSON(w, http.StatusCreated, response)
}

func (a *App) handleUpdateInboxShare(w http.ResponseWriter, r *http.Request) {
	mb, err := a.mailboxForCurrentUserWithID(r, chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusNotFound, "mailbox not found")
		return
	}
	windowMinutes, folderIDs, ok := a.parseInboxShareInput(w, r, mb.ID)
	if !ok {
		return
	}
	result, err := a.db.ExecContext(r.Context(), `UPDATE inbox_shares SET window_minutes=?,folder_ids=?,updated_at=? WHERE mailbox_id=?`, windowMinutes, jsonEncode(folderIDs), a.now().UTC().Format(time.RFC3339Nano), mb.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update inbox share")
		return
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		respondError(w, http.StatusNotFound, "inbox share not enabled")
		return
	}
	response, err := a.inboxShareSettings(r.Context(), mb)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load inbox share settings")
		return
	}
	respondJSON(w, http.StatusOK, response)
}

func (a *App) handleDeleteInboxShare(w http.ResponseWriter, r *http.Request) {
	mb, err := a.mailboxForCurrentUserWithID(r, chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusNotFound, "mailbox not found")
		return
	}
	if _, err := a.db.ExecContext(r.Context(), `DELETE FROM inbox_shares WHERE mailbox_id=?`, mb.ID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to disable inbox share")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) parseInboxShareInput(w http.ResponseWriter, r *http.Request, mailboxID string) (int, []string, bool) {
	var req struct {
		WindowMinutes int      `json:"windowMinutes"`
		FolderIDs     []string `json:"folderIds"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return 0, nil, false
	}
	if !allowedInboxShareWindows[req.WindowMinutes] {
		respondError(w, http.StatusBadRequest, "invalid inbox share time range")
		return 0, nil, false
	}
	folderIDs := cleanIDList(req.FolderIDs)
	if len(folderIDs) == 0 {
		respondError(w, http.StatusBadRequest, "select at least one folder")
		return 0, nil, false
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(folderIDs)), ",")
	args := make([]any, 0, len(folderIDs)+1)
	args = append(args, mailboxID)
	for _, id := range folderIDs {
		args = append(args, id)
	}
	var count int
	if err := a.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM folders WHERE mailbox_id=? AND id IN (`+placeholders+`)`, args...).Scan(&count); err != nil || count != len(folderIDs) {
		respondError(w, http.StatusBadRequest, "one or more folders are invalid")
		return 0, nil, false
	}
	return req.WindowMinutes, folderIDs, true
}

func (a *App) inboxShareSettings(ctx context.Context, mb *Mailbox) (inboxShareSettingsResponse, error) {
	response := inboxShareSettingsResponse{MailboxID: mb.ID, MailboxAddress: mb.Address, WindowMinutes: 30, FolderIDs: []string{}, Folders: []inboxShareFolder{}}
	rows, err := a.db.QueryContext(ctx, `SELECT f.id,f.name,f.role,COUNT(m.id) FROM folders f LEFT JOIN messages m ON m.folder_id=f.id WHERE f.mailbox_id=? GROUP BY f.id,f.name,f.role,f.sort_order,f.created_at ORDER BY CASE WHEN lower(f.name)='inbox' THEN 0 ELSE 1 END,f.sort_order,f.created_at,f.name`, mb.ID)
	if err != nil {
		return response, err
	}
	defer rows.Close()
	for rows.Next() {
		var folder inboxShareFolder
		if err := rows.Scan(&folder.ID, &folder.Name, &folder.Role, &folder.TotalCount); err != nil {
			return response, err
		}
		response.Folders = append(response.Folders, folder)
		if strings.EqualFold(folder.Name, "Inbox") {
			response.FolderIDs = []string{folder.ID}
		}
	}
	if err := rows.Err(); err != nil {
		return response, err
	}
	var tokenCipher, folderJSON, created, updated, lastAccessed string
	err = a.db.QueryRowContext(ctx, `SELECT token_cipher,window_minutes,folder_ids,created_at,updated_at,last_accessed_at FROM inbox_shares WHERE mailbox_id=?`, mb.ID).Scan(&tokenCipher, &response.WindowMinutes, &folderJSON, &created, &updated, &lastAccessed)
	if errors.Is(err, sql.ErrNoRows) {
		for i := range response.Folders {
			response.Folders[i].Selected = inboxShareContains(response.FolderIDs, response.Folders[i].ID)
		}
		return response, nil
	}
	if err != nil {
		return response, err
	}
	response.Enabled = true
	if tokenCipher != "" {
		if token, decryptErr := a.decryptBackupPassword(tokenCipher); decryptErr == nil && strings.HasPrefix(token, "nis_") {
			response.ShareURL = a.inboxShareURL(nil, token)
		}
	}
	storedFolderIDs := jsonDecodeSlice(folderJSON)
	response.FolderIDs = response.FolderIDs[:0]
	createdAt, updatedAt := parseTime(created), parseTime(updated)
	response.CreatedAt, response.UpdatedAt = &createdAt, &updatedAt
	if strings.TrimSpace(lastAccessed) != "" {
		value := parseTime(lastAccessed)
		response.LastAccessedAt = &value
	}
	for i := range response.Folders {
		response.Folders[i].Selected = inboxShareContains(storedFolderIDs, response.Folders[i].ID)
		if response.Folders[i].Selected {
			response.FolderIDs = append(response.FolderIDs, response.Folders[i].ID)
		}
	}
	return response, nil
}

func inboxShareContains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func (a *App) inboxShareURL(r *http.Request, token string) string {
	base := strings.TrimRight(strings.TrimSpace(a.config().PublicBaseURL), "/")
	if base == "" {
		scheme := "https"
		if r != nil && r.TLS == nil && a.config().AllowInsecureHTTP {
			scheme = "http"
		}
		if r != nil {
			base = scheme + "://" + r.Host
		}
	}
	if base == "" {
		return ""
	}
	return base + "/shared-inbox#" + token
}

func (a *App) sharedInboxFromRequest(r *http.Request) (inboxShareRecord, error) {
	var share inboxShareRecord
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(auth, "Bearer ") {
		return share, sql.ErrNoRows
	}
	token := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	if !strings.HasPrefix(token, "nis_") || len(token) > 96 {
		return share, sql.ErrNoRows
	}
	var foldersJSON, created, updated, lastAccessed string
	err := a.db.QueryRowContext(r.Context(), `SELECT s.mailbox_id,mb.address,mb.user_id,s.window_minutes,s.folder_ids,s.created_at,s.updated_at,s.last_accessed_at
		FROM inbox_shares s JOIN mailboxes mb ON mb.id=s.mailbox_id JOIN domains d ON d.id=mb.domain_id
		WHERE s.token_hash=? AND mb.status='active' AND d.status='active'`, hashToken(token)).Scan(&share.MailboxID, &share.MailboxAddress, &share.OwnerID, &share.WindowMinutes, &foldersJSON, &created, &updated, &lastAccessed)
	if err != nil {
		return share, err
	}
	owner, err := a.userByID(r.Context(), share.OwnerID)
	if err != nil || owner.Disabled || !userHasPermission(owner, PermissionInboxShare) {
		return inboxShareRecord{}, sql.ErrNoRows
	}
	share.FolderIDs = jsonDecodeSlice(foldersJSON)
	if len(share.FolderIDs) == 0 || !allowedInboxShareWindows[share.WindowMinutes] {
		return inboxShareRecord{}, sql.ErrNoRows
	}
	share.CreatedAt, share.UpdatedAt = parseTime(created), parseTime(updated)
	if strings.TrimSpace(lastAccessed) != "" {
		value := parseTime(lastAccessed)
		share.LastAccessedAt = &value
	}
	cutoff := a.now().UTC().Add(-5 * time.Minute).Format(time.RFC3339Nano)
	_, _ = a.db.ExecContext(r.Context(), `UPDATE inbox_shares SET last_accessed_at=? WHERE mailbox_id=? AND (last_accessed_at='' OR last_accessed_at<?)`, a.now().UTC().Format(time.RFC3339Nano), share.MailboxID, cutoff)
	return share, nil
}

func setSharedInboxHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
}

func (a *App) handleSharedInboxMessages(w http.ResponseWriter, r *http.Request) {
	setSharedInboxHeaders(w)
	share, err := a.sharedInboxFromRequest(r)
	if err != nil {
		respondError(w, http.StatusNotFound, "shared inbox not found")
		return
	}
	offset, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("cursor")))
	if err != nil || offset < 0 || offset > 100000 {
		offset = 0
	}
	folderPlaceholders := strings.TrimSuffix(strings.Repeat("?,", len(share.FolderIDs)), ",")
	query := `SELECT m.id,m.folder_id,f.name,m.subject,m.from_addr,m.from_name,m.received_at,m.snippet,m.has_attachments
		FROM messages m JOIN folders f ON f.id=m.folder_id WHERE m.mailbox_id=? AND m.folder_id IN (` + folderPlaceholders + `)`
	args := []any{share.MailboxID}
	for _, id := range share.FolderIDs {
		args = append(args, id)
	}
	if share.WindowMinutes > 0 {
		query += ` AND m.received_at>=?`
		args = append(args, a.now().UTC().Add(-time.Duration(share.WindowMinutes)*time.Minute).Format(time.RFC3339Nano))
	}
	query += ` ORDER BY m.received_at DESC,m.id DESC LIMIT ? OFFSET ?`
	args = append(args, sharedInboxPageSize+1, offset)
	rows, err := a.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load shared inbox")
		return
	}
	defer rows.Close()
	items := []sharedInboxMessage{}
	for rows.Next() {
		var item sharedInboxMessage
		var received string
		var hasAttachments int
		if err := rows.Scan(&item.ID, &item.FolderID, &item.Folder, &item.Subject, &item.From, &item.FromName, &received, &item.Snippet, &hasAttachments); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to scan shared inbox")
			return
		}
		item.ReceivedAt = parseTime(received)
		item.HasAttachments = intBool(hasAttachments)
		items = append(items, item)
	}
	response := map[string]any{"items": items, "mailboxAddress": share.MailboxAddress, "windowMinutes": share.WindowMinutes}
	if len(items) > sharedInboxPageSize {
		response["items"] = items[:sharedInboxPageSize]
		response["nextCursor"] = strconv.Itoa(offset + sharedInboxPageSize)
	}
	respondJSON(w, http.StatusOK, response)
}

func (a *App) sharedMessageAllowed(ctx context.Context, share inboxShareRecord, messageID string) bool {
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(share.FolderIDs)), ",")
	query := `SELECT COUNT(*) FROM messages WHERE id=? AND mailbox_id=? AND folder_id IN (` + placeholders + `)`
	args := []any{messageID, share.MailboxID}
	for _, id := range share.FolderIDs {
		args = append(args, id)
	}
	if share.WindowMinutes > 0 {
		query += ` AND received_at>=?`
		args = append(args, a.now().UTC().Add(-time.Duration(share.WindowMinutes)*time.Minute).Format(time.RFC3339Nano))
	}
	var count int
	return a.db.QueryRowContext(ctx, query, args...).Scan(&count) == nil && count == 1
}

func (a *App) handleSharedInboxMessage(w http.ResponseWriter, r *http.Request) {
	setSharedInboxHeaders(w)
	share, err := a.sharedInboxFromRequest(r)
	id := chi.URLParam(r, "id")
	if err != nil || !a.sharedMessageAllowed(r.Context(), share, id) {
		respondError(w, http.StatusNotFound, "shared message not found")
		return
	}
	msg, err := a.messageByID(r.Context(), id, true)
	if err != nil {
		respondError(w, http.StatusNotFound, "shared message not found")
		return
	}
	respondJSON(w, http.StatusOK, sharedInboxMessageDetail{
		sharedInboxMessage: sharedInboxMessage{ID: msg.ID, FolderID: msg.FolderID, Folder: msg.Folder, Subject: msg.Subject, From: msg.From, FromName: msg.FromName, ReceivedAt: msg.ReceivedAt, Snippet: msg.Snippet, HasAttachments: msg.HasAttachments},
		BodyText:           msg.BodyText, BodyHTML: msg.BodyHTML, Attachments: msg.Attachments,
	})
}

func (a *App) handleSharedInboxAttachment(w http.ResponseWriter, r *http.Request) {
	setSharedInboxHeaders(w)
	share, err := a.sharedInboxFromRequest(r)
	if err != nil {
		respondError(w, http.StatusNotFound, "shared attachment not found")
		return
	}
	var messageID, filename, contentType, path string
	var size int64
	err = a.db.QueryRowContext(r.Context(), `SELECT a.message_id,a.filename,a.content_type,a.size_bytes,a.storage_path FROM attachments a WHERE a.id=?`, chi.URLParam(r, "id")).Scan(&messageID, &filename, &contentType, &size, &path)
	if err != nil || !a.sharedMessageAllowed(r.Context(), share, messageID) {
		respondError(w, http.StatusNotFound, "shared attachment not found")
		return
	}
	file, err := os.Open(path)
	if err != nil {
		respondError(w, http.StatusNotFound, "shared attachment not found")
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, strings.ReplaceAll(filename, `"`, "")))
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	_, _ = io.Copy(w, file)
}
