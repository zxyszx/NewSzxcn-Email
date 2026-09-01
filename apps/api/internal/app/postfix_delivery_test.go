package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPostfixDeliveryImportTracksEachRecipient(t *testing.T) {
	a := newTestApp(t)
	stopTestWorkers(a)
	user, mailbox := defaultAdminUserAndMailbox(t, a)
	now := time.Date(2026, 8, 30, 19, 23, 0, 0, time.Local)
	a.now = func() time.Time { return now }
	a.updateConfig(func(cfg *Config) { cfg.StatusWebhookURL = "https://events.example.test/delivery" })
	_, err := a.db.Exec(`INSERT INTO send_queue(id,user_id,mailbox_id,sent_message_id,message_id,source,mail_from,header_from,recipients_json,mime_base64,status,next_attempt_at,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, "snd_postfix", user.ID, mailbox.ID, "msg_postfix", "<mail@example.test>", sendSourceForwarding, mailbox.Address, mailbox.Address, `["one@example.test","two@example.test"]`, "", sendQueueStatusDelivered, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := a.importPostfixLine(ctx, `Aug 30 19:23:01 mail postfix/cleanup[10]: A1B2C3D4: message-id=<mail@example.test>`); err != nil {
		t.Fatal(err)
	}
	if err := a.importPostfixLine(ctx, `Aug 30 19:23:01 mail postfix/cleanup[10]: A1B2C3D4: strip: header X-LanQin-Queue-ID: snd_postfix from localhost[127.0.0.1]`); err != nil {
		t.Fatal(err)
	}
	if err := a.importPostfixLine(ctx, `Aug 30 19:23:18 mail postfix/smtp[11]: A1B2C3D4: to=<one@example.test>, relay=mx.example.test[192.0.2.1]:25, delay=1, delays=0/0/0.5/0.5, dsn=2.0.0, status=sent (250 2.0.0 accepted)`); err != nil {
		t.Fatal(err)
	}
	if err := a.importPostfixLine(ctx, `Aug 30 19:23:19 mail postfix/smtp[11]: A1B2C3D4: to=<two@example.test>, relay=mx.example.test[192.0.2.1]:25, delay=2, delays=0/0/1/1, dsn=4.7.0, status=deferred (450 4.7.0 rate limited)`); err != nil {
		t.Fatal(err)
	}
	var queueID string
	if err := a.db.QueryRow(`SELECT postfix_queue_id FROM send_queue WHERE id='snd_postfix'`).Scan(&queueID); err != nil || queueID != "A1B2C3D4" {
		t.Fatalf("postfix queue id = %q, err=%v", queueID, err)
	}
	rows, err := a.db.Query(`SELECT recipient,status,smtp_code,dsn,relay,attempts,error FROM delivery_events ORDER BY recipient`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type result struct {
		recipient, status, smtpCode, dsn, relay, errorText string
		attempts                                           int
	}
	got := []result{}
	for rows.Next() {
		var item result
		if err := rows.Scan(&item.recipient, &item.status, &item.smtpCode, &item.dsn, &item.relay, &item.attempts, &item.errorText); err != nil {
			t.Fatal(err)
		}
		got = append(got, item)
	}
	if len(got) != 2 {
		t.Fatalf("delivery events = %#v", got)
	}
	if got[0].status != "delivered" || got[0].smtpCode != "250" || got[0].dsn != "2.0.0" || got[0].errorText != "" {
		t.Fatalf("delivered result = %#v", got[0])
	}
	if got[1].status != "deferred" || got[1].smtpCode != "450" || got[1].dsn != "4.7.0" || got[1].errorText == "" {
		t.Fatalf("deferred result = %#v", got[1])
	}
	var webhookEvents int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM status_webhook_outbox WHERE event_type IN ('delivery.delivered','delivery.deferred')`).Scan(&webhookEvents); err != nil || webhookEvents != 2 {
		t.Fatalf("delivery webhook events = %d, err=%v", webhookEvents, err)
	}
}

func TestAddMessageHeaderPreservesBodyBoundary(t *testing.T) {
	raw := []byte("X-LanQin-Queue-ID: untrusted\r\nSubject: Test\r\n\r\nbody")
	got := string(addMessageHeader(raw, "X-LanQin-Queue-ID", "snd_one"))
	if got != "Subject: Test\r\nX-LanQin-Queue-ID: snd_one\r\n\r\nbody" {
		t.Fatalf("message = %q", got)
	}
}

func TestPostfixTrackingDoesNotOverwriteExistingQueueID(t *testing.T) {
	a := newTestApp(t)
	stopTestWorkers(a)
	user, mailbox := defaultAdminUserAndMailbox(t, a)
	now := a.now().UTC().Format(time.RFC3339Nano)
	if _, err := a.db.Exec(`INSERT INTO send_queue(id,user_id,mailbox_id,source,mail_from,header_from,recipients_json,mime_base64,status,next_attempt_at,postfix_queue_id,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, "snd_existing", user.ID, mailbox.ID, sendSourceWebmail, mailbox.Address, mailbox.Address, `[]`, "", sendQueueStatusDelivered, now, "ORIGINAL", now, now); err != nil {
		t.Fatal(err)
	}
	if err := a.importPostfixLine(context.Background(), `Aug 30 19:23:01 mail postfix/cleanup[10]: A77ACCEE: strip: header X-LanQin-Queue-ID: snd_existing from unknown[192.0.2.10]`); err != nil {
		t.Fatal(err)
	}
	var queueID string
	if err := a.db.QueryRow(`SELECT postfix_queue_id FROM send_queue WHERE id='snd_existing'`).Scan(&queueID); err != nil || queueID != "ORIGINAL" {
		t.Fatalf("postfix queue id = %q, err=%v", queueID, err)
	}
}

func TestDeliveryEventsForAuditScopesEmptyQueueIDByMessage(t *testing.T) {
	a := newTestApp(t)
	stopTestWorkers(a)
	now := a.now().UTC().Format(time.RFC3339Nano)
	for _, item := range []struct{ id, messageID, recipient string }{
		{"event_one", "message_one", "one@example.test"},
		{"event_two", "message_two", "two@example.test"},
	} {
		if _, err := a.db.Exec(`INSERT INTO delivery_events(id,external_id,provider,sent_message_id,recipient,status,occurred_at,created_at) VALUES(?,?,?,?,?,?,?,?)`, item.id, item.id, "test", item.messageID, item.recipient, "delivered", now, now); err != nil {
			t.Fatal(err)
		}
	}
	events, err := a.deliveryEventsForAudit(context.Background(), "", "message_one")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Recipient != "one@example.test" {
		t.Fatalf("delivery events = %#v", events)
	}
	if events[0].LastAttemptAt != nil {
		t.Fatalf("last attempt should be omitted: %#v", events[0].LastAttemptAt)
	}
}

func TestPostfixLogImporterWaitsForCompleteLine(t *testing.T) {
	a := newTestApp(t)
	stopTestWorkers(a)
	user, mailbox := defaultAdminUserAndMailbox(t, a)
	now := a.now().UTC().Format(time.RFC3339Nano)
	if _, err := a.db.Exec(`INSERT INTO send_queue(id,user_id,mailbox_id,source,mail_from,header_from,recipients_json,mime_base64,status,next_attempt_at,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, "snd_partial", user.ID, mailbox.ID, sendSourceWebmail, mailbox.Address, mailbox.Address, `[]`, "", sendQueueStatusDelivered, now, now, now); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	logPath := filepath.Join(dir, "postfix.log")
	offsetPath := filepath.Join(dir, "postfix.offset")
	line := `Aug 30 19:23:01 mail postfix/cleanup[10]: ABCD1234: strip: header X-LanQin-Queue-ID: snd_partial from localhost[127.0.0.1]`
	if err := os.WriteFile(logPath, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.importPostfixLog(context.Background(), logPath, offsetPath); err != nil {
		t.Fatal(err)
	}
	var queueID string
	if err := a.db.QueryRow(`SELECT postfix_queue_id FROM send_queue WHERE id='snd_partial'`).Scan(&queueID); err != nil || queueID != "" {
		t.Fatalf("partial line queue id = %q, err=%v", queueID, err)
	}
	if offset := readPostfixOffset(offsetPath); offset != 0 {
		t.Fatalf("partial line offset = %d", offset)
	}
	if err := os.WriteFile(logPath, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.importPostfixLog(context.Background(), logPath, offsetPath); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow(`SELECT postfix_queue_id FROM send_queue WHERE id='snd_partial'`).Scan(&queueID); err != nil || queueID != "ABCD1234" {
		t.Fatalf("complete line queue id = %q, err=%v", queueID, err)
	}
}
