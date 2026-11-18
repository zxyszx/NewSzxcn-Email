package app

import (
	"context"
	"errors"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSMTPRoutingProviderAndMXBoundaries(t *testing.T) {
	a := newTestApp(t)
	stopTestWorkers(a)
	a.lookupMX = func(_ context.Context, domain string) ([]*net.MX, error) {
		switch domain {
		case "company.example":
			return []*net.MX{{Host: "tenant.mail.protection.outlook.com."}}, nil
		case "google.example":
			return []*net.MX{{Host: "aspmx.l.google.com."}}, nil
		case "yahoo.example":
			return []*net.MX{{Host: "mta.am0.yahoodns.net."}}, nil
		case "spoof.example":
			return []*net.MX{{Host: "mail.protection.outlook.com.attacker.test."}}, nil
		case "mixed.example":
			return []*net.MX{{Host: "tenant.mail.protection.outlook.com."}, {Host: "mx.other.test."}}, nil
		case "broken.example":
			return nil, errors.New("DNS timeout")
		default:
			return []*net.MX{{Host: "mx.other.test."}}, nil
		}
	}
	for _, tc := range []struct {
		providers, domains, recipient string
		want, fail                    bool
	}{
		{"", "", "user@outlook.com", true, false}, {"all", "", "user@broken.example", true, false},
		{"none", "", "user@broken.example", false, false}, {"none", "", "user@outlook.com", false, false}, {"microsoft,yahoo", "", "user@OUTLOOK.COM", true, false},
		{"microsoft,yahoo", "", "user@yahoo.com", true, false}, {"microsoft,yahoo", "", "user@gmail.com", false, false},
		{"microsoft", "", "user@company.example", true, false}, {"google", "", "user@google.example", true, false},
		{"yahoo", "", "user@yahoo.example", true, false}, {"microsoft", "", "user@spoof.example", false, false},
		{"microsoft", "", "user@mixed.example", false, false}, {"microsoft", "", "user@broken.example", false, true},
		{"custom", "example.com", "user@example.com", true, false}, {"custom", "example.com", "user@sub.example.com", false, false},
		{"custom", "example.com", "user@example.com.attacker.test", false, false},
	} {
		t.Run(tc.providers+"/"+tc.recipient, func(t *testing.T) {
			got, err := a.smtpRecipientUsesRelay(context.Background(), Config{SMTPRelayProviders: tc.providers, SMTPRelayDomains: tc.domains}, tc.recipient)
			if got != tc.want || (err != nil) != tc.fail {
				t.Fatalf("got %v,%v want %v fail=%v", got, err, tc.want, tc.fail)
			}
		})
	}
}

func TestSMTPRoutingSettingsPersistenceAndValidation(t *testing.T) {
	a := newTestApp(t)
	stopTestWorkers(a)
	server := httptest.NewServer(a.Router())
	defer server.Close()
	c := &testClient{t: t, server: server}
	if status := c.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, nil); status != 200 {
		t.Fatal(status)
	}
	var settings SystemSettings
	if status := c.do("GET", "/api/admin/settings", nil, &settings); status != 200 {
		t.Fatal(status)
	}
	if strings.Join(settings.SMTPRelayProviders, ",") != "all" {
		t.Fatal("legacy routing changed")
	}
	payload := systemSettingsPayload(settings)
	payload["smtpRelayProviders"] = []string{"microsoft", "yahoo", "custom"}
	payload["smtpRelayDomains"] = "Example.COM,example.com"
	payload["smtpDirectHost"] = "postfix"
	payload["smtpDirectPort"] = "25"
	if status := c.do("POST", "/api/admin/settings", payload, &settings); status != 200 {
		t.Fatal(status)
	}
	if settings.SMTPRelayDomains != "example.com" || len(settings.SMTPRelayProviders) != 3 {
		t.Fatalf("settings=%+v", settings)
	}
	a.updateConfig(func(cfg *Config) { cfg.SMTPRelayProviders = "all"; cfg.SMTPDirectHost = "wrong" })
	if err := a.loadPersistedSystemSettings(context.Background()); err != nil {
		t.Fatal(err)
	}
	if cfg := a.config(); cfg.SMTPRelayProviders != "microsoft,yahoo,custom" || cfg.SMTPDirectHost != "postfix" {
		t.Fatal("routing did not persist")
	}
	payload["smtpRelayProviders"] = []string{""}
	if status := c.do("POST", "/api/admin/settings", payload, nil); status != 400 {
		t.Fatal("blank provider changed routing", status)
	}
	payload["smtpRelayProviders"] = []string{"unknown"}
	if status := c.do("POST", "/api/admin/settings", payload, nil); status != 400 {
		t.Fatal("unknown provider accepted", status)
	}
	payload["smtpRelayProviders"] = []string{"custom"}
	payload["smtpRelayDomains"] = "https://example.com"
	if status := c.do("POST", "/api/admin/settings", payload, nil); status != 400 {
		t.Fatal("invalid domain accepted", status)
	}
	payload["smtpRelayProviders"] = []string{}
	payload["smtpRelayDomains"] = ""
	if status := c.do("POST", "/api/admin/settings", payload, &settings); status != 200 || len(settings.SMTPRelayProviders) != 0 {
		t.Fatal("empty selection not preserved", status)
	}
	delete(payload, "smtpRelayProviders")
	delete(payload, "smtpRelayDomains")
	delete(payload, "smtpDirectHost")
	delete(payload, "smtpDirectPort")
	if status := c.do("POST", "/api/admin/settings", payload, &settings); status != 200 || len(settings.SMTPRelayProviders) != 0 {
		t.Fatal("old client reset routing", status)
	}
}

func TestSMTPRoutingMixedRecipientsRetryDoesNotResendAcceptedGroup(t *testing.T) {
	a := newTestApp(t)
	stopTestWorkers(a)
	directHost, directPort, directMessages := startCapturingSMTP(t, 10)
	relayHost, relayPort, relayMessages := startCapturingSMTP(t, 10)
	refused, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, badPort, _ := net.SplitHostPort(refused.Addr().String())
	refused.Close()
	a.updateConfig(func(cfg *Config) {
		cfg.SMTPHost = relayHost
		cfg.SMTPPort = badPort
		cfg.SMTPRelayProviders = "microsoft"
		cfg.SMTPDirectHost = directHost
		cfg.SMTPDirectPort = directPort
	})
	user, mb := defaultAdminUserAndMailbox(t, a)
	input := sendQueueInput{UserID: user.ID, MailboxID: mb.ID, Source: sendSourceSubmission, MailFrom: mb.Address, HeaderFrom: mb.Address, MessageID: "routing-retry", Recipients: []string{"person@gmail.com", "person@outlook.com"}, MIMEBytes: []byte("From: sender@example.com\r\nTo: person@gmail.com\r\nSubject: routing\r\n\r\nhello\r\n"), Now: a.now()}
	id, err := a.enqueueSend(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	a.processSendQueueItem(context.Background(), id)
	select {
	case <-directMessages:
	case <-time.After(time.Second):
		t.Fatal("direct group not sent")
	}
	var count int
	if err = a.db.QueryRow(`SELECT COUNT(*) FROM send_queue_recipient_receipts WHERE queue_id=?`, id).Scan(&count); err != nil || count != 1 {
		t.Fatal("missing durable receipt", count, err)
	}
	var deliveredRecipient string
	if err = a.db.QueryRow(`SELECT recipient FROM send_queue_recipient_receipts WHERE queue_id=?`, id).Scan(&deliveredRecipient); err != nil || deliveredRecipient != "person@gmail.com" {
		t.Fatal("wrong recipient used direct route", deliveredRecipient, err)
	}
	a.updateConfig(func(cfg *Config) { cfg.SMTPPort = relayPort })
	if _, err = a.db.Exec(`UPDATE send_queue SET next_attempt_at=? WHERE id=?`, a.now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), id); err != nil {
		t.Fatal(err)
	}
	a.processSendQueueItem(context.Background(), id)
	select {
	case <-relayMessages:
	case <-time.After(time.Second):
		var state, detail string
		_ = a.db.QueryRow(`SELECT status,last_error FROM send_queue WHERE id=?`, id).Scan(&state, &detail)
		t.Fatalf("relay retry not sent: %s %s", state, detail)
	}
	select {
	case <-directMessages:
		t.Fatal("successful direct group duplicated on retry")
	default:
	}
	var status string
	if err = a.db.QueryRow(`SELECT status FROM send_queue WHERE id=?`, id).Scan(&status); err != nil || status != sendQueueStatusDelivered {
		t.Fatal(status, err)
	}
	if _, err = a.enqueueSend(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if err = a.db.QueryRow(`SELECT COUNT(*) FROM send_queue_recipient_receipts WHERE queue_id=?`, id).Scan(&count); err != nil || count != 0 {
		t.Fatal("resubmission retained old receipts", count, err)
	}
	a.processSendQueueItem(context.Background(), id)
	select {
	case <-directMessages:
	case <-time.After(time.Second):
		t.Fatal("resubmission skipped direct group")
	}
	select {
	case <-relayMessages:
	case <-time.After(time.Second):
		t.Fatal("resubmission skipped relay group")
	}
}

func TestSMTPRoutingRelayTLSFailureDoesNotFallBack(t *testing.T) {
	a := newTestApp(t)
	stopTestWorkers(a)
	relayHost, relayPort, relayMessages := startFakeSMTP(t)
	directHost, directPort, directMessages := startFakeSMTP(t)
	a.updateConfig(func(cfg *Config) {
		cfg.SMTPHost = relayHost
		cfg.SMTPPort = relayPort
		cfg.SMTPRequireTLS = true
		cfg.SMTPRelayProviders = "microsoft"
		cfg.SMTPDirectHost = directHost
		cfg.SMTPDirectPort = directPort
	})
	user, mb := defaultAdminUserAndMailbox(t, a)
	id, err := a.enqueueSend(context.Background(), sendQueueInput{UserID: user.ID, MailboxID: mb.ID, Source: sendSourceSubmission, MailFrom: mb.Address, MessageID: "no-tls-fallback", Recipients: []string{"person@outlook.com"}, MIMEBytes: []byte("Subject: test\r\n\r\ntest\r\n")})
	if err != nil {
		t.Fatal(err)
	}
	a.processSendQueueItem(context.Background(), id)
	var state, detail string
	if err = a.db.QueryRow(`SELECT status,last_error FROM send_queue WHERE id=?`, id).Scan(&state, &detail); err != nil || state != sendQueueStatusFailed || !strings.Contains(detail, "STARTTLS") {
		t.Fatal(state, detail, err)
	}
	select {
	case <-directMessages:
		t.Fatal("failed relay bypassed policy via direct SMTP")
	default:
	}
	select {
	case <-relayMessages:
		t.Fatal("message sent without required TLS")
	default:
	}
}

func TestSMTPRoutingExpandedProviders(t *testing.T) {
	for _, tc := range []struct{ key, domain, mx string }{
		{"tencent", "qq.com", "mxbiz1.qq.com"},
		{"netease", "126.com", "qiye163mx01.mxmail.netease.com"},
		{"alibaba", "aliyun.com", "mx1.qiye.aliyun.com"},
		{"feishu", "", "mx1.feishu.cn"},
		{"lark", "", "mx1.larksuite.com"},
		{"apple", "icloud.com", "mx01.mail.icloud.com"},
		{"proton", "pm.me", "mail.protonmail.ch"},
		{"fastmail", "fastmail.com", "in1-smtp.messagingengine.com"},
		{"zoho", "zohomail.eu", "mx.zoho.eu"},
		{"zohocn", "zoho.com.cn", "mx.zoho.com.cn"},
		{"sina", "sina.cn", ""}, {"sohu", "sohu.com", ""},
		{"china_mobile", "139.com", ""}, {"china_telecom", "189.cn", ""},
		{"china_unicom", "wo.cn", ""}, {"tom", "tom.com", ""},
		{"tuta", "tuta.com", ""}, {"gmx", "gmx.de", ""},
		{"mailcom", "mail.com", ""}, {"webde", "web.de", ""},
		{"yandex", "ya.ru", ""}, {"mailru", "mail.ru", ""},
		{"naver", "naver.com", ""}, {"daum", "hanmail.net", ""}, {"hey", "hey.com", ""},
	} {
		t.Run(tc.key, func(t *testing.T) {
			cfg := Config{SMTPRelayProviders: tc.key}
			if err := normalizeSMTPRoutingConfig(&cfg); err != nil {
				t.Fatal(err)
			}
			a := &App{lookupMX: func(context.Context, string) ([]*net.MX, error) {
				if tc.mx == "" {
					t.Fatal("unexpected DNS lookup for public domain")
				}
				return []*net.MX{{Host: tc.mx + "."}}, nil
			}}
			domain := tc.domain
			if domain == "" {
				domain = "business.example"
			}
			if got, err := a.smtpRecipientUsesRelay(context.Background(), cfg, "user@"+domain); err != nil || !got {
				t.Fatalf("selected provider not relayed: %v %v", got, err)
			}
			if tc.mx != "" {
				if got, err := a.smtpRecipientUsesRelay(context.Background(), cfg, "user@business.example"); err != nil || !got {
					t.Fatalf("MX not relayed: %v %v", got, err)
				}
				if smtpProviderForMX(tc.mx+".attacker.test") != "" {
					t.Fatal("MX lookalike matched")
				}
			}
		})
	}
	keys, domains := map[string]bool{}, map[string]bool{}
	for _, provider := range smtpProviderCatalog {
		if provider.Key == "" || provider.Label == "" || keys[provider.Key] {
			t.Fatal("invalid or duplicate provider", provider.Key)
		}
		keys[provider.Key] = true
		for _, domain := range provider.Domains {
			if domains[domain] {
				t.Fatal("ambiguous public domain", domain)
			}
			domains[domain] = true
			if smtpProviderForDomain(domain+".attacker.test") != "" {
				t.Fatal("public domain lookalike matched")
			}
		}
	}
}

func TestSMTPRoutingLocalMailBypassesPublicMX(t *testing.T) {
	a := newTestApp(t)
	stopTestWorkers(a)
	host, port, messages := startCapturingSMTP(t, 10)
	a.lookupMX = func(context.Context, string) ([]*net.MX, error) {
		t.Fatal("local mail must not query public DNS")
		return nil, nil
	}
	a.updateConfig(func(cfg *Config) {
		cfg.SMTPRelayProviders = "microsoft,custom"
		cfg.SMTPRelayDomains = "lanqin.local"
		cfg.SMTPHost = "relay.invalid"
		cfg.SMTPDirectHost, cfg.SMTPDirectPort = host, port
	})
	user, mailbox := defaultAdminUserAndMailbox(t, a)
	id, err := a.enqueueSend(context.Background(), sendQueueInput{UserID: user.ID, MailboxID: mailbox.ID, Source: sendSourceWebmail, MailFrom: mailbox.Address, HeaderFrom: mailbox.Address, Recipients: []string{mailbox.Address}, MIMEBytes: []byte("Subject: local\r\n\r\nhello\r\n"), Now: a.now()})
	if err != nil {
		t.Fatal(err)
	}
	a.processSendQueueItem(context.Background(), id)
	select {
	case <-messages:
	case <-time.After(time.Second):
		t.Fatal("local mail not submitted to Postfix")
	}
	var status string
	if err := a.db.QueryRow(`SELECT status FROM send_queue WHERE id=?`, id).Scan(&status); err != nil || status != sendQueueStatusDelivered {
		t.Fatal(status, err)
	}
}
