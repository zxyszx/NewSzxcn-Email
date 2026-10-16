package app

import (
	"bytes"
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/jwt"
)

type pushSender interface {
	Send(context.Context, string, string, string) (bool, error)
}

type fcmSender struct {
	project string
	tokens  oauth2.TokenSource
	client  *http.Client
}

type pushRetryError struct {
	status int
	after  time.Duration
}

func (e *pushRetryError) Error() string { return fmt.Sprintf("FCM HTTP status %d", e.status) }

func pushRetryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
		return time.Duration(min(max(seconds, 0), 86400)) * time.Second
	}
	if deadline, err := http.ParseTime(value); err == nil {
		return min(max(deadline.Sub(now), 0), 24*time.Hour)
	}
	return 0
}

var pushIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{20,80}$`)
var pushTokenPattern = regexp.MustCompile(`^[a-zA-Z0-9_:-]+$`)
var projectPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{4,61}[a-z0-9]$`)

func (a *App) configurePush() error {
	cfg := a.config()
	if cfg.FCMProjectID == "" && cfg.FCMCredentialsFile == "" {
		return nil
	}
	if !projectPattern.MatchString(cfg.FCMProjectID) || cfg.FCMCredentialsFile == "" {
		return errors.New("FCM project and credential file must both be configured")
	}
	if cfg.UpdateServiceToken == "" && cfg.ExternalIMAPSecretKey == "" {
		return errors.New("push token encryption requires an existing server encryption secret")
	}
	info, err := os.Stat(cfg.FCMCredentialsFile)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 65536 || info.Mode().Perm()&0077 != 0 {
		return errors.New("FCM credential file must be private, regular and at most 64 KiB")
	}
	data, err := os.ReadFile(cfg.FCMCredentialsFile)
	if err != nil {
		return errors.New("cannot read FCM credential file")
	}
	var identity struct {
		ProjectID  string `json:"project_id"`
		Type       string `json:"type"`
		Email      string `json:"client_email"`
		PrivateKey string `json:"private_key"`
	}
	if json.Unmarshal(data, &identity) != nil || identity.ProjectID != cfg.FCMProjectID || identity.Type != "service_account" || identity.PrivateKey == "" || !strings.HasSuffix(identity.Email, ".iam.gserviceaccount.com") {
		return errors.New("FCM credential project does not match")
	}
	identityConfig := &jwt.Config{Email: identity.Email, PrivateKey: []byte(identity.PrivateKey), Scopes: []string{"https://www.googleapis.com/auth/firebase.messaging"}, TokenURL: "https://oauth2.googleapis.com/token"}
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, client)
	a.pushSender = &fcmSender{project: cfg.FCMProjectID, tokens: oauth2.ReuseTokenSource(nil, identityConfig.TokenSource(ctx)), client: client}
	return nil
}

func (s *fcmSender) Send(ctx context.Context, token, eventID, bindingID string) (bool, error) {
	access, err := s.tokens.Token()
	if err != nil {
		return false, errors.New("FCM authentication unavailable")
	}
	body, _ := json.Marshal(map[string]any{"message": map[string]any{
		"token":   token,
		"data":    map[string]string{"type": "new_mail", "event_id": eventID, "binding_id": bindingID},
		"android": map[string]any{"priority": "high", "ttl": "3600s"},
	}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://fcm.googleapis.com/v1/projects/"+s.project+"/messages:send", bytes.NewReader(body))
	if err != nil {
		return false, errors.New("FCM request unavailable")
	}
	req.Header.Set("Authorization", "Bearer "+access.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		return false, errors.New("FCM transport unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		io.Copy(io.Discard, io.LimitReader(res.Body, 65536))
		return false, nil
	}
	var failure struct {
		Error struct {
			Details []struct {
				Type string `json:"@type"`
				Code string `json:"errorCode"`
			} `json:"details"`
		} `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 65536)).Decode(&failure)
	for _, detail := range failure.Error.Details {
		if detail.Type == "type.googleapis.com/google.firebase.fcm.v1.FcmError" && detail.Code == "UNREGISTERED" {
			return true, nil
		}
	}
	return false, &pushRetryError{status: res.StatusCode, after: pushRetryAfter(res.Header.Get("Retry-After"), time.Now())}
}

func (a *App) migratePush(ctx context.Context) error {
	rows, err := a.db.QueryContext(ctx, `PRAGMA table_info(messages)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, notNull, pk int
		var name, kind string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
			rows.Close()
			return err
		}
		found = found || name == "push_eligible"
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !found {
		if _, err := a.db.ExecContext(ctx, `ALTER TABLE messages ADD COLUMN push_eligible INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS push_devices (
		 id TEXT PRIMARY KEY, secret_hash TEXT NOT NULL,
		 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		 session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
		 token_hash TEXT NOT NULL UNIQUE, token_cipher TEXT NOT NULL,
		 created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_push_devices_user ON push_devices(user_id)`,
		`CREATE TABLE IF NOT EXISTS push_outbox (
		 id TEXT PRIMARY KEY, device_id TEXT NOT NULL REFERENCES push_devices(id) ON DELETE CASCADE,
		 message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
		 attempts INTEGER NOT NULL DEFAULT 0, next_attempt INTEGER NOT NULL,
		 lease_until INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL,
		 finished INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '',
		 UNIQUE(device_id,message_id))`,
		`CREATE INDEX IF NOT EXISTS idx_push_outbox_due ON push_outbox(finished,next_attempt)`,
		// The trigger makes message storage and notification enqueue one atomic write.
		`CREATE TRIGGER IF NOT EXISTS enqueue_mail_push AFTER INSERT ON messages
		 WHEN NEW.push_eligible=1 AND NEW.is_read=0
		 BEGIN
		 INSERT OR IGNORE INTO push_outbox(id,device_id,message_id,next_attempt,created_at)
		 SELECT lower(hex(randomblob(16))),d.id,NEW.id,unixepoch()+2,unixepoch()
		 FROM push_devices d JOIN mailboxes mb ON mb.user_id=d.user_id
		 JOIN sessions s ON s.id=d.session_id AND s.user_id=d.user_id
		 WHERE mb.id=NEW.mailbox_id AND mb.status='active' AND julianday(s.expires_at)>julianday('now');
		 END`,
	}
	for _, statement := range statements {
		if _, err := a.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) handlePushPreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	eventID := chi.URLParam(r, "eventID")
	if !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(eventID) {
		respondError(w, http.StatusNotFound, "not found")
		return
	}
	cookie, err := r.Cookie(a.config().CookieName)
	if err != nil {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var subject, mailboxAddress string
	err = a.db.QueryRowContext(r.Context(), `SELECT m.subject,mb.address FROM push_outbox o
	 JOIN push_devices d ON d.id=o.device_id
	 JOIN sessions s ON s.id=d.session_id AND s.user_id=d.user_id
	 JOIN messages m ON m.id=o.message_id
	 JOIN mailboxes mb ON mb.id=m.mailbox_id AND mb.user_id=d.user_id
	 WHERE o.id=? AND d.user_id=? AND s.token_hash=? AND mb.status='active'
	 AND julianday(s.expires_at)>julianday('now') AND o.created_at>?`, eventID, currentUser(r).ID, hashToken(cookie.Value), a.now().Unix()-3600).Scan(&subject, &mailboxAddress)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondError(w, http.StatusNotFound, "not found")
		} else {
			respondError(w, http.StatusServiceUnavailable, "preview unavailable")
		}
		return
	}
	subject = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || (r >= '\u202a' && r <= '\u202e') || (r >= '\u2066' && r <= '\u2069') {
			return ' '
		}
		return r
	}, subject)
	runes := []rune(strings.TrimSpace(subject))
	if len(runes) > 160 {
		runes = runes[:160]
	}
	respondJSON(w, http.StatusOK, map[string]string{"subject": string(runes), "mailboxAddress": mailboxAddress})
}

func (a *App) handleRegisterPushDevice(w http.ResponseWriter, r *http.Request) {
	if a.pushSender == nil {
		respondError(w, http.StatusServiceUnavailable, "push unavailable")
		return
	}
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		respondError(w, http.StatusUnsupportedMediaType, "JSON required")
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		base, err := url.Parse(a.config().PublicBaseURL)
		if err != nil || origin != base.Scheme+"://"+base.Host {
			respondError(w, http.StatusForbidden, "origin denied")
			return
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	var input struct {
		ID     string `json:"id"`
		Secret string `json:"secret"`
		Token  string `json:"token"`
	}
	if decodeJSON(r, &input) != nil || !pushIDPattern.MatchString(input.ID) || !pushIDPattern.MatchString(input.Secret) || len(input.Secret) < 40 || len(input.Token) < 40 || len(input.Token) > 4096 || !pushTokenPattern.MatchString(input.Token) {
		respondError(w, http.StatusBadRequest, "invalid device registration")
		return
	}
	cookie, err := r.Cookie(a.config().CookieName)
	if err != nil {
		respondError(w, http.StatusUnauthorized, "session required")
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		respondError(w, 500, "device registration unavailable")
		return
	}
	defer tx.Rollback()
	userID := currentUser(r).ID
	var sessionID string
	if tx.QueryRowContext(r.Context(), `SELECT id FROM sessions WHERE token_hash=? AND user_id=? AND expires_at>?`, hashToken(cookie.Value), userID, a.now().UTC().Format(time.RFC3339Nano)).Scan(&sessionID) != nil {
		respondError(w, 401, "session required")
		return
	}
	var secretHash, oldToken, oldSession string
	err = tx.QueryRowContext(r.Context(), `SELECT secret_hash,token_hash,session_id FROM push_devices WHERE id=?`, input.ID).Scan(&secretHash, &oldToken, &oldSession)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		respondError(w, 500, "device registration unavailable")
		return
	}
	if err == nil && subtle.ConstantTimeCompare([]byte(secretHash), []byte(hashToken(input.Secret))) != 1 {
		respondError(w, 409, "device conflict")
		return
	}
	var otherID string
	err = tx.QueryRowContext(r.Context(), `SELECT id FROM push_devices WHERE token_hash=? AND id<>?`, hashToken(input.Token), input.ID).Scan(&otherID)
	if err == nil {
		respondError(w, 409, "device conflict")
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		respondError(w, 500, "device registration unavailable")
		return
	}
	var count int
	if tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM push_devices WHERE user_id=? AND id<>?`, userID, input.ID).Scan(&count) != nil || count >= 20 {
		respondError(w, 429, "device limit reached")
		return
	}
	ciphertext, err := a.encryptBackupPassword(input.Token)
	if err != nil {
		respondError(w, 503, "device encryption unavailable")
		return
	}
	// Drop queued work when either login identity or token changes, before rebinding.
	if oldToken != hashToken(input.Token) || oldSession != sessionID {
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM push_outbox WHERE device_id=?`, input.ID); err != nil {
			respondError(w, 500, "device registration unavailable")
			return
		}
	}
	now := a.now().Unix()
	_, err = tx.ExecContext(r.Context(), `INSERT INTO push_devices(id,secret_hash,user_id,session_id,token_hash,token_cipher,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)
	 ON CONFLICT(id) DO UPDATE SET user_id=excluded.user_id,session_id=excluded.session_id,token_hash=excluded.token_hash,token_cipher=excluded.token_cipher,updated_at=excluded.updated_at`, input.ID, hashToken(input.Secret), userID, sessionID, hashToken(input.Token), ciphertext, now, now)
	if err != nil || tx.Commit() != nil {
		respondError(w, 500, "device registration unavailable")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respondJSON(w, http.StatusOK, map[string]string{"bindingId": hashToken(sessionID + ":" + hashToken(input.Secret))})
}

func (a *App) pushWorker(ctx context.Context) {
	if a.pushSender == nil {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.deliverPushBatch(ctx)
		}
	}
}

func (a *App) deliverPushBatch(ctx context.Context) {
	a.pushMu.Lock()
	defer a.pushMu.Unlock()
	now := a.now().Unix()
	_, _ = a.db.ExecContext(ctx, `DELETE FROM push_outbox WHERE created_at<?`, now-30*86400)
	_, _ = a.db.ExecContext(ctx, `DELETE FROM push_devices WHERE session_id IN (SELECT id FROM sessions WHERE julianday(expires_at)<=julianday('now'))`)
	_, _ = a.db.ExecContext(ctx, `UPDATE push_outbox SET finished=1,lease_until=0,last_error='attempt limit reached' WHERE finished=0 AND attempts>=8 AND lease_until<=?`, now)
	rows, err := a.db.QueryContext(ctx, `SELECT id FROM push_outbox WHERE finished=0 AND attempts<8 AND next_attempt<=? AND lease_until<=? ORDER BY next_attempt LIMIT 20`, now, now)
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		a.deliverPush(ctx, id)
	}
}

func (a *App) deliverPush(ctx context.Context, id string) {
	now := a.now().Unix()
	result, err := a.db.ExecContext(ctx, `UPDATE push_outbox SET lease_until=?,attempts=attempts+1 WHERE id=? AND finished=0 AND attempts<8 AND lease_until<=? AND next_attempt<=?`, now+90, id, now, now)
	if err != nil {
		return
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return
	}
	var deviceID, tokenCipher, userID, tokenHash, sessionID, proofHash string
	var attempts int
	err = a.db.QueryRowContext(ctx, `SELECT d.id,d.token_cipher,d.user_id,d.token_hash,o.attempts,s.id,d.secret_hash
	 FROM push_outbox o JOIN push_devices d ON d.id=o.device_id
	 JOIN sessions s ON s.id=d.session_id AND s.user_id=d.user_id
	 JOIN users u ON u.id=d.user_id JOIN messages m ON m.id=o.message_id
	 JOIN mailboxes mb ON mb.id=m.mailbox_id AND mb.user_id=d.user_id
	 JOIN folders f ON f.id=m.folder_id AND f.mailbox_id=mb.id
	 WHERE o.id=? AND u.disabled=0 AND mb.status='active' AND lower(f.name)='inbox'
	 AND m.is_read=0 AND o.created_at>? AND julianday(s.expires_at)>julianday('now')`, id, now-3600).Scan(&deviceID, &tokenCipher, &userID, &tokenHash, &attempts, &sessionID, &proofHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			_, _ = a.db.ExecContext(ctx, `UPDATE push_outbox SET finished=1,last_error='ineligible',lease_until=0 WHERE id=?`, id)
		}
		// A transient database error leaves the lease to expire for another attempt.
		return
	}
	user, err := a.userByID(ctx, userID)
	if err != nil {
		if errors.Is(err, errNotFound) {
			_, _ = a.db.ExecContext(ctx, `UPDATE push_outbox SET finished=1,last_error='ineligible',lease_until=0 WHERE id=?`, id)
		}
		return
	}
	if user.Disabled || !userHasPermission(user, PermissionMailRead) {
		_, _ = a.db.ExecContext(ctx, `UPDATE push_outbox SET finished=1,last_error='ineligible',lease_until=0 WHERE id=?`, id)
		return
	}
	token, err := a.decryptBackupPassword(tokenCipher)
	invalid := false
	if err == nil {
		sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		invalid, err = a.pushSender.Send(sendCtx, token, id, hashToken(sessionID+":"+proofHash))
		cancel()
	}
	if invalid {
		_, _ = a.db.ExecContext(ctx, `DELETE FROM push_devices WHERE id=? AND token_hash=?`, deviceID, tokenHash)
		return
	}
	if err == nil {
		_, _ = a.db.ExecContext(ctx, `UPDATE push_outbox SET finished=1,last_error='',lease_until=0 WHERE id=?`, id)
		return
	}
	delay := int64(60) << min(attempts-1, 4)
	var retry *pushRetryError
	if errors.As(err, &retry) {
		delay = max(delay, int64((retry.after+time.Second-1)/time.Second))
	}
	_, _ = a.db.ExecContext(ctx, `UPDATE push_outbox SET next_attempt=?,lease_until=0,last_error='delivery unavailable',finished=? WHERE id=?`, now+delay, boolInt(attempts >= 8), id)
}
