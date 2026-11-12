package app

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
)

func putTestSMTPPool(t *testing.T, a *App, scope string, p smtpPoolSettings) smtpPoolSettings {
	t.Helper()
	old, _, err := a.loadSMTPPool(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.validateSMTPPool(context.Background(), &p, old); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(p)
	_, err = a.db.Exec(`INSERT INTO smtp_pools(scope,settings,cursor) VALUES(?,?,0) ON CONFLICT(scope) DO UPDATE SET settings=excluded.settings`, scope, string(raw))
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func testPool() smtpPoolSettings {
	return smtpPoolSettings{Source: "personal", Enabled: true, Providers: []string{"microsoft", "yahoo"}, Strategy: "rotate", Accounts: []smtpPoolAccount{
		{Name: "First", Host: "smtp.first.example", Port: 465, Username: "first@example.test", From: "first@example.test", Password: "test-app-password", Enabled: true, RotateAfter: 2},
		{Name: "Second", Host: "smtp.second.example", Port: 587, Username: "second@example.test", From: "second@example.test", Password: "other-app-password", Enabled: true, RotateAfter: 1},
	}}
}
func TestSMTPPoolInheritanceRoutingRotationAndRetry(t *testing.T) {
	a := newTestApp(t)
	owner, _ := defaultAdminUserAndMailbox(t, a)
	a.updateConfig(func(c *Config) {
		c.ExternalIMAPEnabled = true
		c.ExternalIMAPSecretKey = "test-key"
		c.SMTPHost = "postfix"
	})
	putTestSMTPPool(t, a, "admin", testPool())
	counts := map[string]int{}
	hosts := map[string]string{}
	fail := true
	a.personalSMTPSender = func(cfg Config, from string, recipients []string, raw []byte) error {
		r := recipients[0]
		counts[r]++
		hosts[r] = cfg.SMTPHost
		if cfg.SMTPHost != "postfix" {
			if !cfg.SMTPRequireTLS || from != cfg.SMTPUsername || strings.Contains(string(raw), "Bcc:") {
				t.Fatal("unsafe external submission")
			}
			if r == "one@outlook.com" && fail {
				return errors.New("sensitive-upstream-text")
			}
		}
		return nil
	}
	item := sendQueueItem{ID: "pool-mixed", UserID: owner.ID, MailFrom: "local@example.test", Recipients: []string{"one@outlook.com", "two@hotmail.com", "three@rocketmail.com", "four@yahoo.com", "five@gmail.com", "six@qq.com"}}
	raw := []byte("From: local@example.test\r\nTo: five@gmail.com\r\nSubject: mixed\r\n\r\nbody")
	if err := a.sendQueueWithPersonalRoute(context.Background(), item, raw); err == nil || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("failure not redacted: %v", err)
	}
	for r, want := range map[string]string{"one@outlook.com": "smtp.first.example", "two@hotmail.com": "smtp.first.example", "three@rocketmail.com": "smtp.second.example", "four@yahoo.com": "smtp.first.example", "five@gmail.com": "postfix", "six@qq.com": "postfix"} {
		if hosts[r] != want {
			t.Errorf("%s host %s want %s", r, hosts[r], want)
		}
	}
	var cursor int
	if err := a.db.QueryRow(`SELECT cursor FROM smtp_pools WHERE scope='admin'`).Scan(&cursor); err != nil || cursor != 4 {
		t.Fatalf("cursor %d: %v", cursor, err)
	}
	fail = false
	if err := a.sendQueueWithPersonalRoute(context.Background(), item, raw); err != nil {
		t.Fatal(err)
	}
	for _, r := range item.Recipients {
		want := 1
		if r == "one@outlook.com" {
			want = 2
		}
		if counts[r] != want {
			t.Fatalf("duplicate %s %d", r, counts[r])
		}
	}
	if err := a.db.QueryRow(`SELECT cursor FROM smtp_pools WHERE scope='admin'`).Scan(&cursor); err != nil || cursor != 4 {
		t.Fatal("retry consumed another slot")
	}
	a.updateConfig(func(c *Config) { c.ExternalIMAPRestricted = true; c.ExternalIMAPUserIDs = "someone-else" })
	item.ID = "revoked"
	item.Recipients = []string{"denied@outlook.com", "allowed@gmail.com"}
	if err := a.sendQueueWithPersonalRoute(context.Background(), item, raw); err == nil {
		t.Fatal("revoked account submitted")
	}
	if counts["denied@outlook.com"] != 0 || counts["allowed@gmail.com"] != 1 {
		t.Fatal("policy not enforced")
	}
}
func TestSMTPPoolPersonalOverrideAndExactDomainMatching(t *testing.T) {
	a := newTestApp(t)
	owner, _ := defaultAdminUserAndMailbox(t, a)
	a.updateConfig(func(c *Config) { c.ExternalIMAPEnabled = true; c.ExternalIMAPSecretKey = "test-key" })
	putTestSMTPPool(t, a, "admin", testPool())
	p := testPool()
	p.Providers = []string{"qq"}
	p.Domains = []string{"company.example"}
	p = putTestSMTPPool(t, a, owner.ID, p)
	got, scope, exists, err := a.effectiveSMTPPool(context.Background(), owner.ID)
	if err != nil || !exists || scope != owner.ID {
		t.Fatal("personal override failed")
	}
	for r, want := range map[string]bool{"x@qq.com": true, "x@foxmail.com": true, "x@company.example": true, "x@qq.com.attacker.test": false, "x@outlook.com": false, "x@gmail.com": false} {
		if got.matches(r) != want {
			t.Errorf("wrong match %s", r)
		}
	}
	p.Strategy = "single"
	p.AccountID = p.Accounts[1].ID
	p = putTestSMTPPool(t, a, owner.ID, p)
	for i := 0; i < 4; i++ {
		id, err := a.assignSMTPPool(context.Background(), owner.ID, sendQueueItem{ID: "single"}, string(rune('a'+i))+"@qq.com")
		if err != nil || id != p.AccountID {
			t.Fatal("single-account selection failed", err)
		}
	}
	p.Source = "inherit"
	putTestSMTPPool(t, a, owner.ID, p)
	_, scope, _, err = a.effectiveSMTPPool(context.Background(), owner.ID)
	if err != nil || scope != "admin" {
		t.Fatal("inherit failed")
	}
	got.AllProviders = true
	for _, r := range []string{"x@gmail.com", "x@unlisted.example"} {
		if !got.matches(r) {
			t.Fatal("all providers missed", r)
		}
	}
}
func TestSMTPPoolAPISecretsAndOwnership(t *testing.T) {
	a := newTestApp(t)
	a.updateConfig(func(c *Config) { c.ExternalIMAPEnabled = true; c.ExternalIMAPSecretKey = "test-key" })
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}
	if admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, nil) != 200 {
		t.Fatal("login")
	}
	var response struct {
		Settings smtpPoolSettings `json:"settings"`
		Shared   map[string]any   `json:"shared"`
	}
	if status := admin.do("POST", "/api/admin/smtp-pool", testPool(), &response); status != 200 {
		t.Fatal("save", status)
	}
	for _, acc := range response.Settings.Accounts {
		if acc.Ciphertext != "" || acc.Password != "" || !acc.PasswordSet {
			t.Fatal("secret exposure")
		}
	}
	sharedIDs := response.Settings.Accounts
	other := createTestMailbox(t, admin, mustDefaultDomainID(t, a), "pool-other", "Other", "Password123!", nil)
	client := &testClient{t: t, server: ts}
	if client.do("POST", "/api/auth/login", map[string]string{"email": other.Address, "password": "Password123!"}, nil) != 200 {
		t.Fatal("login")
	}
	if status := client.do("GET", "/api/admin/smtp-pool", nil, nil); status != 403 {
		t.Fatal("ordinary user read admin pool", status)
	}
	if status := client.do("GET", "/api/me/smtp-pool", nil, &response); status != 200 {
		t.Fatal(status)
	}
	if len(response.Settings.Accounts) != 0 || response.Shared["accountCount"] != float64(2) {
		t.Fatal("shared identity leaked or summary missing")
	}
	p := testPool()
	p.Accounts = sharedIDs
	if status := client.do("POST", "/api/me/smtp-pool", p, nil); status != 400 {
		t.Fatal("shared ID accepted", status)
	}
	p = testPool()
	for i := range p.Accounts {
		p.Accounts[i].Password = ""
		p.Accounts[i].Ciphertext = "forged"
	}
	if status := client.do("POST", "/api/me/smtp-pool", p, nil); status != 400 {
		t.Fatal("browser ciphertext trusted", status)
	}
}
func TestSMTPPoolBlocksPrivateDestinationsAndIdentityChanges(t *testing.T) {
	a := newTestApp(t)
	a.updateConfig(func(c *Config) { c.ExternalIMAPSecretKey = "test-key"; c.ExternalIMAPAllowPrivateHosts = true })
	p := putTestSMTPPool(t, a, "admin", testPool())
	a.lookupIP = func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}
	err := a.sendSMTPPoolRecipient(context.Background(), p.Accounts[0], "receiver@example.test", []byte("From: sender@example.test\r\n\r\nbody"))
	if err == nil || !strings.Contains(err.Error(), "内网") {
		t.Fatal("SSRF guard failed", err)
	}
	old := p
	p.Accounts = append([]smtpPoolAccount(nil), p.Accounts...)
	p.Accounts[0].Host = "smtp.changed.example"
	p.Accounts[0].Password = ""
	if err := a.validateSMTPPool(context.Background(), &p, old); err == nil {
		t.Fatal("password reused for changed host")
	}
}
