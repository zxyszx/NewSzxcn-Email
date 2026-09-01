package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
)

type userTelegramSettings struct {
	Enabled       bool     `json:"enabled"`
	PrivateChatID string   `json:"privateChatId"`
	MailboxIDs    []string `json:"mailboxIds"`
	BotConfigured bool     `json:"botConfigured"`
}

type userTelegramSettingsRequest struct {
	Enabled       bool     `json:"enabled"`
	BotToken      string   `json:"botToken"`
	PrivateChatID string   `json:"privateChatId"`
	MailboxIDs    []string `json:"mailboxIds"`
}

func (a *App) handleUserTelegramSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := a.userTelegramSettings(r.Context(), currentUser(r).ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "无法读取 Telegram 通知设置")
		return
	}
	respondJSON(w, http.StatusOK, settings)
}

func (a *App) handleUpdateUserTelegramSettings(w http.ResponseWriter, r *http.Request) {
	var req userTelegramSettingsRequest
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	userID := currentUser(r).ID
	botToken := strings.TrimSpace(req.BotToken)
	chatID := strings.TrimSpace(req.PrivateChatID)
	mailboxIDs := cleanIDList(req.MailboxIDs)
	var tokenCipher string
	_ = a.db.QueryRowContext(r.Context(), `SELECT bot_token_cipher FROM user_telegram_settings WHERE user_id=?`, userID).Scan(&tokenCipher)
	if botToken != "" {
		var err error
		tokenCipher, err = a.encryptBackupPassword(botToken)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "无法安全保存 Telegram Bot Token")
			return
		}
	}
	if req.Enabled {
		if tokenCipher == "" {
			badRequest(w, errors.New("请先填写自己的 Telegram Bot Token"))
			return
		}
		if botToken == "" {
			if _, err := a.decryptBackupPassword(tokenCipher); err != nil {
				badRequest(w, errors.New("已保存的 Telegram Bot Token 无法读取，请重新填写"))
				return
			}
		}
		if !validTelegramPrivateChatID(chatID) {
			badRequest(w, errors.New("请先完成 Telegram 私聊绑定"))
			return
		}
		if len(mailboxIDs) == 0 {
			badRequest(w, errors.New("请至少选择一个接收提醒的邮箱"))
			return
		}
	}
	allowed := make([]string, 0, len(mailboxIDs))
	for _, mailboxID := range mailboxIDs {
		var count int
		if err := a.db.QueryRowContext(r.Context(), `SELECT COUNT(1) FROM mailboxes WHERE id=? AND user_id=? AND status='active'`, mailboxID, userID).Scan(&count); err != nil || count != 1 {
			badRequest(w, errors.New("包含无权设置的邮箱"))
			return
		}
		allowed = append(allowed, mailboxID)
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "无法保存 Telegram 通知设置")
		return
	}
	defer tx.Rollback()
	now := a.now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO user_telegram_settings(user_id,enabled,bot_token_cipher,private_chat_id,created_at,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(user_id) DO UPDATE SET enabled=excluded.enabled,bot_token_cipher=excluded.bot_token_cipher,private_chat_id=excluded.private_chat_id,updated_at=excluded.updated_at`, userID, boolInt(req.Enabled), tokenCipher, chatID, now, now); err != nil {
		respondError(w, http.StatusInternalServerError, "无法保存 Telegram 通知设置")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM user_telegram_mailboxes WHERE user_id=?`, userID); err != nil {
		respondError(w, http.StatusInternalServerError, "无法保存 Telegram 通知设置")
		return
	}
	for _, mailboxID := range allowed {
		if _, err := tx.ExecContext(r.Context(), `INSERT INTO user_telegram_mailboxes(user_id,mailbox_id) VALUES(?,?)`, userID, mailboxID); err != nil {
			respondError(w, http.StatusInternalServerError, "无法保存 Telegram 通知设置")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		respondError(w, http.StatusInternalServerError, "无法保存 Telegram 通知设置")
		return
	}
	if !req.Enabled {
		_, _ = a.db.ExecContext(r.Context(), `DELETE FROM telegram_mail_outbox WHERE delivered_at IS NULL AND message_id IN (SELECT m.id FROM messages m JOIN mailboxes mb ON mb.id=m.mailbox_id WHERE mb.user_id=?)`, userID)
	}
	settings, _ := a.userTelegramSettings(r.Context(), userID)
	respondJSON(w, http.StatusOK, settings)
}

func (a *App) userTelegramSettings(ctx context.Context, userID string) (userTelegramSettings, error) {
	out := userTelegramSettings{MailboxIDs: []string{}}
	var enabled int
	var tokenCipher string
	err := a.db.QueryRowContext(ctx, `SELECT enabled,bot_token_cipher,private_chat_id FROM user_telegram_settings WHERE user_id=?`, userID).Scan(&enabled, &tokenCipher, &out.PrivateChatID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	out.Enabled = enabled == 1
	if strings.TrimSpace(tokenCipher) != "" {
		_, err := a.decryptBackupPassword(tokenCipher)
		out.BotConfigured = err == nil
	}
	rows, err := a.db.QueryContext(ctx, `SELECT tm.mailbox_id FROM user_telegram_mailboxes tm JOIN mailboxes m ON m.id=tm.mailbox_id WHERE tm.user_id=? AND m.user_id=? ORDER BY m.address`, userID, userID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return out, err
		}
		out.MailboxIDs = append(out.MailboxIDs, id)
	}
	return out, rows.Err()
}

func (a *App) userTelegramBotToken(ctx context.Context, userID string) (string, error) {
	var ciphertext string
	if err := a.db.QueryRowContext(ctx, `SELECT bot_token_cipher FROM user_telegram_settings WHERE user_id=?`, userID).Scan(&ciphertext); err != nil {
		return "", err
	}
	if strings.TrimSpace(ciphertext) == "" {
		return "", sql.ErrNoRows
	}
	return a.decryptBackupPassword(ciphertext)
}
