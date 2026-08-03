package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"unicode"
)

const forwardingHeaderName = "X-LanQin-Forwarded-By"

func (a *App) processInboundForwarding(ctx context.Context, messageID, mailboxID string, raw []byte) {
	targets, userID, mailboxAddress, err := a.inboundForwardingTargets(ctx, mailboxID)
	if err != nil {
		a.log.Warn("failed to load forwarding target", "message", messageID, "mailbox", mailboxID, "error", err)
		return
	}
	if len(targets) == 0 || userID == "" || mailboxAddress == "" {
		return
	}
	self := normalizeEmail(mailboxAddress)
	filteredTargets := make([]string, 0, len(targets))
	for _, target := range targets {
		if normalizeEmail(target) == self {
			continue
		}
		filteredTargets = append(filteredTargets, target)
	}
	targets = dedupeEmails(filteredTargets)
	if len(targets) == 0 {
		return
	}
	if len(raw) == 0 {
		raw, err = a.forwardingRawMessage(ctx, messageID)
		if err != nil {
			a.log.Warn("failed to load raw message for forwarding", "message", messageID, "error", err)
			return
		}
	}
	if hasForwardingHeader(raw) {
		a.log.Warn("skip forwarding message that already has LanQin forwarding header", "message", messageID, "mailbox", mailboxID)
		return
	}
	forwarded := addForwardingHeaders(raw, mailboxAddress, a.config().PublicHostname)
	var rfcMessageID string
	_ = a.db.QueryRowContext(ctx, `SELECT message_id FROM messages WHERE id=?`, messageID).Scan(&rfcMessageID)
	if strings.TrimSpace(rfcMessageID) == "" {
		rfcMessageID = messageID
	}
	queueID, err := a.enqueueSend(ctx, sendQueueInput{
		UserID:        userID,
		MailboxID:     mailboxID,
		SentMessageID: messageID,
		MessageID:     rfcMessageID,
		Source:        sendSourceForwarding,
		MailFrom:      mailboxAddress,
		HeaderFrom:    mailboxAddress,
		Recipients:    targets,
		MIMEBytes:     forwarded,
		Now:           a.now().UTC(),
	})
	if err != nil {
		a.log.Warn("failed to enqueue inbound forwarding", "message", messageID, "mailbox", mailboxID, "targets", strings.Join(targets, ","), "error", err)
		return
	}
	if queueID == "" {
		a.log.Warn("forwarding target configured but SMTP sending is not configured", "message", messageID, "mailbox", mailboxID, "targets", strings.Join(targets, ","))
	}
}

func (a *App) processRuleForwarding(ctx context.Context, messageID, mailboxID string, action MailRuleAction) error {
	var userID, mailboxAddress string
	if err := a.db.QueryRowContext(ctx, `SELECT user_id,address FROM mailboxes WHERE id=? AND status='active'`, mailboxID).Scan(&userID, &mailboxAddress); err != nil {
		return err
	}
	targets, err := a.cleanForwardingTargets(ctx, userID, splitRuleForwardTargets(action.Value))
	if err != nil {
		return err
	}
	self := normalizeEmail(mailboxAddress)
	filteredTargets := make([]string, 0, len(targets))
	for _, target := range targets {
		if normalizeEmail(target) == self {
			continue
		}
		filteredTargets = append(filteredTargets, target)
	}
	targets = dedupeEmails(filteredTargets)
	if len(targets) == 0 {
		return nil
	}
	raw, err := a.forwardingRawMessage(ctx, messageID)
	if err != nil {
		return err
	}
	if hasForwardingHeader(raw) {
		a.log.Warn("skip rule forwarding message that already has LanQin forwarding header", "message", messageID, "mailbox", mailboxID)
		return nil
	}
	forwarded := addForwardingHeaders(raw, mailboxAddress, a.config().PublicHostname)
	var rfcMessageID string
	_ = a.db.QueryRowContext(ctx, `SELECT message_id FROM messages WHERE id=?`, messageID).Scan(&rfcMessageID)
	if strings.TrimSpace(rfcMessageID) == "" {
		rfcMessageID = messageID
	}
	queueID, err := a.enqueueSend(ctx, sendQueueInput{
		UserID:        userID,
		MailboxID:     mailboxID,
		SentMessageID: messageID,
		MessageID:     ruleForwardQueueMessageID(rfcMessageID, targets),
		Source:        sendSourceRuleForwarding,
		MailFrom:      mailboxAddress,
		HeaderFrom:    mailboxAddress,
		Recipients:    targets,
		MIMEBytes:     forwarded,
		Now:           a.now().UTC(),
	})
	if err != nil {
		return err
	}
	if queueID == "" {
		a.log.Warn("rule forwarding target configured but SMTP sending is not configured", "message", messageID, "mailbox", mailboxID, "targets", strings.Join(targets, ","))
	}
	return nil
}

func (a *App) inboundForwardingTargets(ctx context.Context, mailboxID string) (targetEmails []string, userID, mailboxAddress string, err error) {
	var mailboxTarget, mailboxTargetsJSON, accountTarget, accountTargetsJSON string
	err = a.db.QueryRowContext(ctx, `SELECT mb.user_id,mb.address,COALESCE(mfs.target_email,''),COALESCE(mfs.target_emails,'[]'),COALESCE(afs.target_email,''),COALESCE(afs.target_emails,'[]')
		FROM mailboxes mb
		LEFT JOIN mailbox_forwarding_settings mfs ON mfs.mailbox_id=mb.id
		LEFT JOIN account_forwarding_settings afs ON afs.user_id=mb.user_id
		WHERE mb.id=? AND mb.status='active'`, mailboxID).Scan(&userID, &mailboxAddress, &mailboxTarget, &mailboxTargetsJSON, &accountTarget, &accountTargetsJSON)
	if err != nil {
		return nil, "", "", err
	}
	targets := forwardingTargetsFromStored(mailboxTarget, mailboxTargetsJSON)
	if len(targets) == 0 {
		targets = forwardingTargetsFromStored(accountTarget, accountTargetsJSON)
	}
	if len(targets) == 0 {
		return nil, userID, mailboxAddress, nil
	}
	verifiedTargets := make([]string, 0, len(targets))
	for _, target := range targets {
		verified, err := a.forwardingEmailVerified(ctx, userID, target)
		if err != nil {
			return nil, "", "", err
		}
		if verified {
			verifiedTargets = append(verifiedTargets, target)
		}
	}
	return dedupeEmails(verifiedTargets), userID, mailboxAddress, nil
}

func (a *App) forwardingRawMessage(ctx context.Context, messageID string) ([]byte, error) {
	msg, err := a.storedMessageByID(ctx, messageID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(msg.RawPath) != "" {
		if ok, err := a.pathIsUnderMaildirRoot(msg.RawPath); err == nil && ok {
			if raw, err := os.ReadFile(msg.RawPath); err == nil {
				return raw, nil
			}
		}
	}
	attachments, err := a.attachmentInputsForMessage(ctx, messageID)
	if err != nil {
		return nil, err
	}
	return BuildMIME(MIMEMessage{
		From:        msg.From,
		FromName:    msg.FromName,
		To:          msg.To,
		CC:          msg.CC,
		BCC:         msg.BCC,
		Subject:     msg.Subject,
		Text:        msg.BodyText,
		HTML:        msg.BodyHTML,
		MessageID:   msg.MessageID,
		Date:        messageDate(msg),
		Attachments: attachments,
	})
}

func splitRuleForwardTargets(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || r == ',' || r == '，' || r == ';' || r == '；'
	})
}

func ruleForwardQueueMessageID(messageID string, targets []string) string {
	base := strings.TrimSpace(messageID)
	if base == "" {
		base = newID("ruleforward")
	}
	sum := sha256.Sum256([]byte(strings.Join(dedupeEmails(targets), ",")))
	return base + "#rule-forward-" + hex.EncodeToString(sum[:])[:12]
}

func hasForwardingHeader(raw []byte) bool {
	header := raw
	if idx := bytes.Index(raw, []byte("\r\n\r\n")); idx >= 0 {
		header = raw[:idx]
	} else if idx := bytes.Index(raw, []byte("\n\n")); idx >= 0 {
		header = raw[:idx]
	}
	return strings.Contains(strings.ToLower(string(header)), strings.ToLower(forwardingHeaderName)+":")
}

func addForwardingHeaders(raw []byte, mailboxAddress, hostname string) []byte {
	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		hostname = "lanqin.local"
	}
	header := fmt.Sprintf("%s: %s\r\nX-LanQin-Forwarded-For: %s\r\n", forwardingHeaderName, hostname, normalizeEmail(mailboxAddress))
	if idx := bytes.Index(raw, []byte("\r\n\r\n")); idx >= 0 {
		out := make([]byte, 0, len(raw)+len(header))
		out = append(out, raw[:idx]...)
		out = append(out, []byte("\r\n"+header)...)
		out = append(out, raw[idx+2:]...)
		return out
	}
	if idx := bytes.Index(raw, []byte("\n\n")); idx >= 0 {
		lfHeader := strings.ReplaceAll(header, "\r\n", "\n")
		out := make([]byte, 0, len(raw)+len(lfHeader))
		out = append(out, raw[:idx]...)
		out = append(out, []byte("\n"+lfHeader)...)
		out = append(out, raw[idx+1:]...)
		return out
	}
	return append([]byte(header+"\r\n"), raw...)
}
