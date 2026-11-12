package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	netmail "net/mail"
	"strings"
	"testing"
)

func TestPersonalSMTPProviderDomains(t *testing.T) {
	for address, want := range map[string]string{"u@outlook.com": "microsoft", "u@hotmail.com": "microsoft", "u@rocketmail.com": "yahoo", "u@yahoo.com.tw": "yahoo", "u@gmail.com": "", "u@outlook.com.attacker.test": "", "u@myoutlook.com": "", "u@company.test": ""} {
		if got := personalSMTPProvider(address); got != want {
			t.Errorf("%s: %s want %s", address, got, want)
		}
	}
}
func TestPersonalGmailMIMEPreservesBodyAndReplyTarget(t *testing.T) {
	raw := []byte("From: Original <original@example.test>\r\nTo: a@outlook.com\r\nReply-To: reply@example.test\r\nBcc: hidden@yahoo.com\r\nDKIM-Signature: invalid-after-rewrite\r\nARC-Seal: invalid\r\nSubject: folded\r\n subject\r\nContent-Type: multipart/mixed; boundary=test\r\n\r\n--test\r\nContent-Type: text/plain\r\n\r\nunchanged body\r\n--test--\r\n")
	rewritten, err := personalGmailMIME(raw, "sender@gmail.com")
	if err != nil {
		t.Fatal(err)
	}
	msg, err := netmail.ReadMessage(bytes.NewReader(rewritten))
	if err != nil {
		t.Fatal(err)
	}
	from, err := msg.Header.AddressList("From")
	if err != nil || len(from) != 1 || from[0].Address != "sender@gmail.com" {
		t.Fatalf("wrong from %v %v", from, err)
	}
	if msg.Header.Get("Reply-To") != "reply@example.test" || msg.Header.Get("Bcc") != "" || msg.Header.Get("Dkim-Signature") != "" || msg.Header.Get("Arc-Seal") != "" {
		t.Fatal("identity or privacy headers incorrect")
	}
	body, _ := io.ReadAll(msg.Body)
	original, _ := netmail.ReadMessage(bytes.NewReader(raw))
	want, _ := io.ReadAll(original.Body)
	if !bytes.Equal(body, want) {
		t.Fatal("body or attachments changed")
	}
}
func TestPersonalSMTPSettingsOwnershipAndEncryption(t *testing.T) {
	a := newTestApp(t)
	a.updateConfig(func(c *Config) { c.ExternalIMAPEnabled = true; c.ExternalIMAPSecretKey = "test-key" })
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, nil); code != 200 {
		t.Fatal(code)
	}
	other := createTestMailbox(t, admin, mustDefaultDomainID(t, a), "smtp-other", "Other", "Password123!", nil)
	client := &testClient{t: t, server: ts}
	if code := client.do("POST", "/api/auth/login", map[string]string{"email": other.Address, "password": "Password123!"}, nil); code != 200 {
		t.Fatal(code)
	}
	payload := map[string]any{"enabled": true, "gmailAddress": "sender@gmail.com", "appPassword": "abcdefghijklmnop", "microsoft": true, "yahoo": true}
	var response map[string]any
	if code := admin.do("POST", "/api/me/personal-smtp-route", payload, &response); code != http.StatusOK {
		t.Fatalf("save %d %v", code, response)
	}
	if _, ok := response["passwordCiphertext"]; ok {
		t.Fatal("secret returned")
	}
	if _, ok := response["appPassword"]; ok {
		t.Fatal("password returned")
	}
	owner, _ := defaultAdminUserAndMailbox(t, a)
	route, err := a.personalSMTPRoute(context.Background(), owner.ID)
	if err != nil || route.PasswordCiphertext == "abcdefghijklmnop" || !route.PasswordSet {
		t.Fatalf("secret not encrypted %v", err)
	}
	var otherRoute personalSMTPRoute
	if code := client.do("GET", "/api/me/personal-smtp-route", nil, &otherRoute); code != 200 || otherRoute.PasswordSet || otherRoute.Enabled || otherRoute.GmailAddress != "" {
		t.Fatalf("cross user leak %d", code)
	}
	payload["gmailAddress"] = "new@gmail.com"
	payload["appPassword"] = ""
	if code := admin.do("POST", "/api/me/personal-smtp-route", payload, nil); code != http.StatusBadRequest {
		t.Fatalf("address switch reused secret %d", code)
	}
}
func TestPersonalSMTPMixedRecipientsRetryDoesNotResubmitAccepted(t *testing.T) {
	a := newTestApp(t)
	owner, _ := defaultAdminUserAndMailbox(t, a)
	a.updateConfig(func(c *Config) {
		c.ExternalIMAPEnabled = true
		c.ExternalIMAPSecretKey = "test-key"
		c.SMTPHost = "postfix"
	})
	ciphertext, err := a.encryptExternalIMAPPassword("abcdefghijklmnop")
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.db.Exec(`INSERT INTO personal_smtp_routes(user_id,enabled,gmail_address,password_ciphertext,microsoft,yahoo) VALUES(?,1,'sender@gmail.com',?,1,1)`, owner.ID, ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	item := sendQueueItem{ID: "test-route", UserID: owner.ID, MailFrom: "sender@example.test", Recipients: []string{"normal@example.test", "first@outlook.com", "hidden@rocketmail.com"}}
	raw := []byte("From: sender@example.test\r\nTo: normal@example.test, first@outlook.com\r\nSubject: route test\r\n\r\nbody")
	counts := map[string]int{}
	fail := true
	a.personalSMTPSender = func(cfg Config, from string, recipients []string, body []byte) error {
		for _, rcpt := range recipients {
			counts[rcpt]++
		}
		if cfg.SMTPHost == "smtp.gmail.com" {
			if !cfg.SMTPRequireTLS || cfg.SMTPPort != "465" || from != "sender@gmail.com" || cfg.SMTPUsername != from {
				t.Fatal("wrong encrypted Gmail configuration")
			}
			if bytes.Contains(body, []byte("hidden@rocketmail.com")) {
				t.Fatal("Bcc recipient leaked")
			}
			if recipients[0] == "hidden@rocketmail.com" && fail {
				return errors.New("temporary failure")
			}
		} else if from != "sender@example.test" {
			t.Fatal("direct identity changed")
		}
		return nil
	}
	if err := a.sendQueueWithPersonalRoute(context.Background(), item, raw); err == nil {
		t.Fatal("expected partial failure")
	}
	fail = false
	if err := a.sendQueueWithPersonalRoute(context.Background(), item, raw); err != nil {
		t.Fatal(err)
	}
	if counts["normal@example.test"] != 1 || counts["first@outlook.com"] != 1 || counts["hidden@rocketmail.com"] != 2 {
		t.Fatalf("duplicate submissions %v", counts)
	}
	a.deleteSendQueueDeliveredMarker(item.ID)
	for _, rcpt := range item.Recipients {
		done, err := a.hasSendQueueDeliveredMarker(personalSubmissionMarker(item.ID, []string{rcpt}))
		if err != nil || done {
			t.Fatal("markers not cleaned")
		}
	}
	// Malformed input must fail before it can introduce a second header.
	if _, err := personalGmailMIME([]byte("From: bad\r\n\r\nbody"), "sender@gmail.com"); err == nil || !strings.Contains(err.Error(), "发件人") {
		t.Fatal("invalid From accepted")
	}
}
