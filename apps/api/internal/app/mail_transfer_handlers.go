package app

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const maxMailImportBytes int64 = 256 << 20

var exportFilenameUnsafe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func (a *App) handleExportMail(w http.ResponseWriter, r *http.Request) {
	ids, err := a.exportMessageIDs(r)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondError(w, http.StatusNotFound, "mailbox or label not found")
			return
		}
		if errors.Is(err, errSystemAdminRequired) {
			respondError(w, http.StatusForbidden, "system admin required")
			return
		}
		badRequest(w, err)
		return
	}

	filename := fmt.Sprintf("mail-export-%s.zip", a.now().UTC().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Cache-Control", "no-store")

	zw := zip.NewWriter(w)
	usedNames := make(map[string]int, len(ids))
	for index, id := range ids {
		raw, subject, err := a.rawMessageForExport(r.Context(), id)
		if err != nil {
			_ = zw.Close()
			return
		}
		entryName := uniqueExportFilename(exportMessageFilename(subject, id, index), usedNames)
		entry, err := zw.CreateHeader(&zip.FileHeader{Name: entryName, Method: zip.Deflate})
		if err != nil {
			_ = zw.Close()
			return
		}
		if _, err := entry.Write(raw); err != nil {
			_ = zw.Close()
			return
		}
	}
	_ = zw.Close()
}

var errSystemAdminRequired = errors.New("system admin required")

func (a *App) exportMessageIDs(r *http.Request) ([]string, error) {
	user := currentUser(r)
	if user == nil {
		return nil, errors.New("no user")
	}
	view := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("view")))
	mailboxID := strings.TrimSpace(r.URL.Query().Get("mailboxId"))
	where := []string{}
	args := []any{}

	if view == "unknown" {
		if user.Role != "admin" {
			return nil, errSystemAdminRequired
		}
		where = append(where, "m.mailbox_id IS NULL")
	} else {
		where = append(where, "EXISTS (SELECT 1 FROM mailboxes owner_mb WHERE owner_mb.id=m.mailbox_id AND owner_mb.user_id=? AND owner_mb.status='active')")
		args = append(args, user.ID)
		if mailboxID != "" && !isAllMailboxID(mailboxID) {
			if _, err := a.mailboxForCurrentUserWithID(r, mailboxID); err != nil {
				return nil, err
			}
			where = append(where, "m.mailbox_id=?")
			args = append(args, mailboxID)
		}
		switch view {
		case "", "folder":
			folder := strings.TrimSpace(r.URL.Query().Get("folder"))
			if folder == "" {
				folder = "Inbox"
			}
			normalized, err := normalizeFolderNameForUser(folder)
			if err != nil {
				return nil, err
			}
			where = append(where, "f.name=?")
			args = append(args, normalized)
		case "starred":
			where = append(where, "m.is_starred=1")
		case "label":
			labelID := strings.TrimSpace(r.URL.Query().Get("labelId"))
			if labelID == "" || !a.labelBelongsToUser(r.Context(), labelID, user.ID) {
				return nil, sql.ErrNoRows
			}
			where = append(where, "EXISTS (SELECT 1 FROM message_labels ml WHERE ml.message_id=m.id AND ml.label_id=?)")
			args = append(args, labelID)
		default:
			return nil, errors.New("unsupported mail view")
		}
	}

	rows, err := a.db.QueryContext(r.Context(), `SELECT m.id FROM messages m LEFT JOIN folders f ON f.id=m.folder_id WHERE `+strings.Join(where, " AND ")+` ORDER BY m.received_at DESC,m.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (a *App) rawMessageForExport(ctx context.Context, id string) ([]byte, string, error) {
	msg, err := a.storedMessageByID(ctx, id)
	if err != nil {
		return nil, "", err
	}
	if msg.RawPath != "" {
		if ok, pathErr := a.pathIsUnderMaildirRoot(msg.RawPath); pathErr == nil && ok {
			if raw, readErr := os.ReadFile(msg.RawPath); readErr == nil {
				return raw, msg.Subject, nil
			}
		}
	}
	attachments, err := a.attachmentInputsForMessage(ctx, id)
	if err != nil {
		return nil, "", err
	}
	raw, err := BuildMIME(MIMEMessage{
		From: msg.From, FromName: msg.FromName, To: msg.To, CC: msg.CC, BCC: msg.BCC,
		Subject: msg.Subject, Text: msg.BodyText, HTML: msg.BodyHTML, MessageID: msg.MessageID,
		Date: messageDate(msg), Attachments: attachments,
	})
	return raw, msg.Subject, err
}

func exportMessageFilename(subject, id string, index int) string {
	name := exportFilenameUnsafe.ReplaceAllString(strings.TrimSpace(subject), "-")
	name = strings.Trim(name, ".-_")
	if name == "" {
		name = "message"
	}
	if len(name) > 80 {
		name = name[:80]
	}
	return fmt.Sprintf("%04d-%s-%s.eml", index+1, name, id)
}

func uniqueExportFilename(name string, used map[string]int) string {
	used[name]++
	if used[name] == 1 {
		return name
	}
	base := strings.TrimSuffix(name, filepath.Ext(name))
	return fmt.Sprintf("%s-%d%s", base, used[name], filepath.Ext(name))
}

func (a *App) handleImportMail(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxMailImportBytes)
	if err := r.ParseMultipartForm(maxMailImportBytes); err != nil {
		respondError(w, http.StatusRequestEntityTooLarge, "import is too large")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	mb, err := a.mailboxForCurrentUserWithID(r, r.FormValue("mailboxId"))
	if err != nil {
		respondError(w, http.StatusNotFound, "mailbox not found")
		return
	}
	folderName := strings.TrimSpace(r.FormValue("folder"))
	if folderName == "" {
		folderName = "Inbox"
	}
	folderName, err = normalizeFolderNameForUser(folderName)
	if err != nil {
		badRequest(w, err)
		return
	}
	folderID, err := a.ensureFolder(r.Context(), mb.ID, folderName)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load folder")
		return
	}
	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		files = r.MultipartForm.File["file"]
	}
	if len(files) == 0 {
		badRequest(w, errors.New("at least one EML or MBOX file is required"))
		return
	}

	imported, skipped := 0, 0
	problems := []string{}
	maxMessageBytes := int64(a.config().SubmissionMaxMessageMB) * 1024 * 1024
	if maxMessageBytes <= 0 {
		maxMessageBytes = 35 * 1024 * 1024
	}
	for _, header := range files {
		messages, fileErr := readImportFile(header, maxMessageBytes)
		if fileErr != nil {
			skipped++
			problems = appendImportProblem(problems, fmt.Sprintf("%s: %v", header.Filename, fileErr))
			continue
		}
		for _, raw := range messages {
			if err := a.importRawMessage(r.Context(), mb, folderID, raw); err != nil {
				skipped++
				problems = appendImportProblem(problems, fmt.Sprintf("%s: %v", header.Filename, err))
				continue
			}
			imported++
		}
	}
	if imported == 0 && len(problems) > 0 {
		badRequest(w, errors.New(problems[0]))
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true, "imported": imported, "skipped": skipped, "errors": problems})
}

func readImportFile(header *multipart.FileHeader, maxMessageBytes int64) ([][]byte, error) {
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".eml" && ext != ".mbox" {
		return nil, errors.New("only .eml and .mbox files are supported")
	}
	file, err := header.Open()
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if ext == ".eml" {
		raw, err := io.ReadAll(io.LimitReader(file, maxMessageBytes+1))
		if err != nil {
			return nil, err
		}
		if int64(len(raw)) > maxMessageBytes {
			return nil, fmt.Errorf("message exceeds %d MB", maxMessageBytes/(1024*1024))
		}
		if len(bytes.TrimSpace(raw)) == 0 {
			return nil, errors.New("message is empty")
		}
		return [][]byte{raw}, nil
	}
	return parseMBOX(file, maxMessageBytes)
}

func parseMBOX(reader io.Reader, maxMessageBytes int64) ([][]byte, error) {
	scanner := bufio.NewScanner(reader)
	bufferSize := int(maxMessageBytes + 1024)
	if bufferSize < 64*1024 {
		bufferSize = 64 * 1024
	}
	scanner.Buffer(make([]byte, 64*1024), bufferSize)
	var current bytes.Buffer
	messages := [][]byte{}
	seenSeparator := false
	flush := func() error {
		raw := bytes.TrimSpace(current.Bytes())
		current.Reset()
		if len(raw) == 0 {
			return nil
		}
		if int64(len(raw)) > maxMessageBytes {
			return fmt.Errorf("message exceeds %d MB", maxMessageBytes/(1024*1024))
		}
		messages = append(messages, append([]byte(nil), raw...))
		return nil
	}
	for scanner.Scan() {
		line := scanner.Bytes()
		if bytes.HasPrefix(line, []byte("From ")) {
			if seenSeparator {
				if err := flush(); err != nil {
					return nil, err
				}
			}
			seenSeparator = true
			continue
		}
		if bytes.HasPrefix(line, []byte(">From ")) {
			line = line[1:]
		}
		current.Write(line)
		current.WriteString("\r\n")
		if int64(current.Len()) > maxMessageBytes {
			return nil, fmt.Errorf("message exceeds %d MB", maxMessageBytes/(1024*1024))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if err := flush(); err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, errors.New("MBOX contains no messages")
	}
	return messages, nil
}

func (a *App) importRawMessage(ctx context.Context, mb *Mailbox, folderID string, raw []byte) error {
	msg, attachments, err := a.parseMaildirMessage(raw, mb.Address)
	if err != nil {
		return fmt.Errorf("invalid message: %w", err)
	}
	msg.MailboxID = mb.ID
	msg.FolderID = folderID
	msg.RecipientAddr = mb.Address
	msg.RawPath = ""
	id, err := a.insertMessage(ctx, msg, attachments)
	if err != nil {
		return err
	}
	if err := a.writeRawMessageToMaildir(ctx, id, raw, false); err != nil {
		a.deleteMessage(ctx, id)
		return err
	}
	return nil
}

func appendImportProblem(items []string, problem string) []string {
	if len(items) >= 5 {
		return items
	}
	return append(items, problem)
}
