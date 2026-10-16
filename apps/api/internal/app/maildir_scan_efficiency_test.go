package app

import (
	"context"
	"testing"
)

func TestMaildirRescanDoesNotRewriteUnchangedMessage(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	a.updateConfig(func(cfg *Config) { cfg.MaildirRoot = t.TempDir() })
	user, mb := defaultAdminUserAndMailbox(t, a)
	clearMailboxMessagesForTest(t, a, mb.ID)
	msg, err := a.sendMailNow(ctx, user, mb, mailComposeInput{
		MailboxID: mb.ID, To: []string{"recipient@example.test"},
		Subject: "rescan", Text: "unchanged body", HTML: "<p>unchanged body</p>",
	})
	if err != nil {
		t.Fatal(err)
	}
	// A sentinel makes the assertion independent of clock resolution.
	const unchangedAt = "2001-01-01T00:00:00Z"
	if _, err := a.db.ExecContext(ctx, `UPDATE messages SET updated_at=? WHERE id=?`, unchangedAt, msg.ID); err != nil {
		t.Fatal(err)
	}
	var before int64
	if err := a.db.QueryRowContext(ctx, `SELECT imap_modseq FROM messages WHERE id=?`, msg.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if count, err := a.syncMaildirOnce(ctx); err != nil || count != 0 {
			t.Fatalf("rescan %d: count=%d err=%v", i, count, err)
		}
	}
	var updatedAt string
	var after int64
	if err := a.db.QueryRowContext(ctx, `SELECT updated_at,imap_modseq FROM messages WHERE id=?`, msg.ID).Scan(&updatedAt, &after); err != nil {
		t.Fatal(err)
	}
	if updatedAt != unchangedAt || after != before {
		t.Fatalf("unchanged message rewritten: updated_at=%s modseq=%d (was %d)", updatedAt, after, before)
	}
}
