package app

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	postfixMessageLine  = regexp.MustCompile(`^([A-Z][a-z]{2}\s+\d{1,2}\s+\d{2}:\d{2}:\d{2}).*postfix/cleanup\[\d+\]:\s+([A-F0-9]+):\s+message-id=(<[^>]+>)`)
	postfixTrackingLine = regexp.MustCompile(`postfix/cleanup\[\d+\]:\s+([A-F0-9]+):\s+(?:warning|strip): header X-LanQin-Queue-ID:\s*([^\s;]+)`)
	postfixSMTPLine     = regexp.MustCompile(`^([A-Z][a-z]{2}\s+\d{1,2}\s+\d{2}:\d{2}:\d{2}).*postfix/smtp\[\d+\]:\s+([A-F0-9]+):\s+to=<([^>]+)>,\s+relay=([^,]+).*dsn=([^,]+),\s+status=([a-z]+)\s+\((.*)\)$`)
	smtpResponseCode    = regexp.MustCompile(`^([245]\d\d)(?:\s|$)`)
)

func (a *App) postfixDeliveryWorker(ctx context.Context) {
	logPath := filepath.Join(a.config().DataDir, "logs", "postfix.log")
	offsetPath := filepath.Join(a.config().DataDir, "postfix-log.offset")
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if err := a.importPostfixLog(ctx, logPath, offsetPath); err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, context.Canceled) {
			a.log.Warn("failed to import postfix delivery log", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *App) importPostfixLog(ctx context.Context, logPath, offsetPath string) error {
	f, err := os.Open(logPath)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	offset := readPostfixOffset(offsetPath)
	if offset < 0 || offset > info.Size() {
		offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	reader := bufio.NewReaderSize(f, 64*1024)
	next := offset
	for {
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return readErr
		}
		next += int64(len(line))
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if err := a.importPostfixLine(ctx, line); err != nil {
			a.log.Warn("failed to import postfix log line", "error", err)
		}
	}
	return os.WriteFile(offsetPath, []byte(strconv.FormatInt(next, 10)), 0o600)
}

func readPostfixOffset(path string) int64 {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	offset, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		return 0
	}
	return offset
}

func (a *App) importPostfixLine(ctx context.Context, line string) error {
	if match := postfixTrackingLine.FindStringSubmatch(line); len(match) == 3 {
		_, err := a.db.ExecContext(ctx, `UPDATE send_queue SET postfix_queue_id=? WHERE id=? AND postfix_queue_id=''`, match[1], match[2])
		return err
	}
	if match := postfixMessageLine.FindStringSubmatch(line); len(match) == 4 {
		_, err := a.db.ExecContext(ctx, `UPDATE send_queue SET postfix_queue_id=? WHERE id=(SELECT id FROM send_queue WHERE (message_id=? OR message_id LIKE ?) AND postfix_queue_id='' ORDER BY created_at LIMIT 1)`, match[2], match[3], match[3]+"#rule-forward-%")
		return err
	}
	match := postfixSMTPLine.FindStringSubmatch(line)
	if len(match) != 8 {
		return nil
	}
	postfixQueueID := match[2]
	recipient := normalizeEmail(match[3])
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var queueID, mailboxID, sentMessageID, rfcMessageID, recipientsJSON string
	err = tx.QueryRowContext(ctx, `SELECT id,mailbox_id,sent_message_id,message_id,recipients_json FROM send_queue WHERE postfix_queue_id=? ORDER BY created_at DESC LIMIT 1`, postfixQueueID).Scan(&queueID, &mailboxID, &sentMessageID, &rfcMessageID, &recipientsJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	found := false
	for _, address := range jsonDecodeSlice(recipientsJSON) {
		if normalizeEmail(address) == recipient {
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	status := postfixDeliveryStatus(match[6])
	reason := strings.TrimSpace(match[7])
	smtpCode := ""
	if code := smtpResponseCode.FindStringSubmatch(reason); len(code) == 2 {
		smtpCode = code[1]
	}
	occurredAt := parsePostfixLogTime(match[1], a.now())
	lineHash := sha256.Sum256([]byte(line))
	externalID := postfixQueueID + ":" + recipient + ":" + hex.EncodeToString(lineHash[:8])
	var attempts int
	_ = tx.QueryRowContext(ctx, `SELECT COUNT(1)+1 FROM delivery_events WHERE postfix_queue_id=? AND recipient=?`, postfixQueueID, recipient).Scan(&attempts)
	if attempts < 1 {
		attempts = 1
	}
	errorText := ""
	if status != "delivered" {
		errorText = reason
	}
	createdAt := a.now().UTC()
	id := newID("dev")
	res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO delivery_events(id,external_id,provider,queue_id,sent_message_id,rfc_message_id,recipient,status,reason,postfix_queue_id,smtp_code,dsn,relay,attempts,last_attempt_at,error,occurred_at,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, externalID, "postfix", queueID, sentMessageID, rfcMessageID, recipient, status, reason, postfixQueueID, smtpCode, strings.TrimSpace(match[5]), strings.TrimSpace(match[4]), attempts, occurredAt.Format(time.RFC3339Nano), errorText, occurredAt.Format(time.RFC3339Nano), createdAt.Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	if inserted, _ := res.RowsAffected(); inserted > 0 {
		item := DeliveryEvent{ID: id, ExternalID: externalID, Provider: "postfix", QueueID: queueID, MessageID: sentMessageID, RFCMessageID: rfcMessageID, Recipient: recipient, Status: status, Reason: reason, PostfixQueueID: postfixQueueID, SMTPCode: smtpCode, DSN: strings.TrimSpace(match[5]), Relay: strings.TrimSpace(match[4]), Attempts: attempts, LastAttemptAt: &occurredAt, Error: errorText, OccurredAt: occurredAt, CreatedAt: createdAt}
		if err := a.enqueueStatusWebhook(ctx, tx, "delivery:postfix:"+externalID, "delivery."+status, mailboxID, item); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func postfixDeliveryStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "sent":
		return "delivered"
	case "deferred":
		return "deferred"
	case "bounced", "expired":
		return "bounced"
	default:
		return "rejected"
	}
}

func parsePostfixLogTime(value string, now time.Time) time.Time {
	year := now.In(time.Local).Year()
	parsed, err := time.ParseInLocation("2006 Jan 2 15:04:05", fmt.Sprintf("%d %s", year, strings.Join(strings.Fields(value), " ")), time.Local)
	if err != nil {
		return now.UTC()
	}
	if parsed.After(now.Add(24 * time.Hour)) {
		parsed = parsed.AddDate(-1, 0, 0)
	}
	return parsed.UTC()
}
