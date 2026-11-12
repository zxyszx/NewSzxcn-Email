package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestExternalIMAPPerUserAccessAndRevocation(t *testing.T) {
	a := newTestApp(t)
	a.updateConfig(func(c *Config) {
		c.ExternalIMAPEnabled = true
		c.ExternalIMAPSecretKey = "test-key"
		c.ExternalIMAPRestricted = true
		c.ExternalIMAPGmailClientID = "test-client"
		c.ExternalIMAPGmailClientSecret = "test-client-secret"
	})
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, nil); code != http.StatusOK {
		t.Fatalf("login %d", code)
	}
	owner := createTestMailbox(t, admin, mustDefaultDomainID(t, a), "policy-owner", "Owner", "Password123!", nil)
	other := createTestMailbox(t, admin, mustDefaultDomainID(t, a), "policy-other", "Other", "Password123!", nil)
	a.updateConfig(func(c *Config) { c.ExternalIMAPUserIDs = owner.UserID })
	ownerClient := &testClient{t: t, server: ts}
	otherClient := &testClient{t: t, server: ts}
	for _, p := range []struct {
		c     *testClient
		email string
	}{{ownerClient, owner.Address}, {otherClient, other.Address}} {
		if code := p.c.do("POST", "/api/auth/login", map[string]string{"email": p.email, "password": "Password123!"}, nil); code != http.StatusOK {
			t.Fatalf("login %d", code)
		}
	}
	if code := ownerClient.do("GET", "/api/me/external-imap-accounts", nil, nil); code != http.StatusOK {
		t.Fatalf("allowed %d", code)
	}
	for _, c := range []*testClient{otherClient, admin} {
		if code := c.do("GET", "/api/me/external-imap-accounts", nil, nil); code != http.StatusForbidden {
			t.Fatalf("unselected user %d", code)
		}
	}
	var me struct {
		Allowed bool `json:"externalImapAllowed"`
	}
	if code := ownerClient.do("GET", "/api/me", nil, &me); code != http.StatusOK || !me.Allowed {
		t.Fatalf("capability %d %+v", code, me)
	}
	state, err := a.encryptExternalIMAPOAuthState(externalIMAPOAuthState{UserID: owner.UserID, MailboxID: owner.ID, Provider: "gmail", ExpiresAt: a.now().Unix() + 300})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.db.Exec(`INSERT INTO external_imap_accounts(id,user_id,mailbox_id,name,host,port,tls_mode,username,password_ciphertext,storage_mode,sync_read_state,enabled,last_status,created_at,updated_at) VALUES(?,?,?,?,?,993,'tls',?,'','local',0,1,'idle',?,?)`, "policy-account", owner.UserID, owner.ID, "test", "imap.example.test", "test@example.test", a.now(), a.now())
	if err != nil {
		t.Fatal(err)
	}
	a.updateConfig(func(c *Config) { c.ExternalIMAPUserIDs = "" })
	if code := ownerClient.do("GET", "/api/me/external-imap-accounts", nil, nil); code != http.StatusForbidden {
		t.Fatalf("revoked API %d", code)
	}
	if code := ownerClient.do("GET", "/api/external-imap-oauth/gmail/callback?code=unused&state="+url.QueryEscape(state), nil, nil); code != http.StatusForbidden {
		t.Fatalf("revoked callback %d", code)
	}
	if _, err := a.syncExternalIMAPAccount(context.Background(), "policy-account"); err == nil || err.Error() != "external imap access denied" {
		t.Fatalf("revoked sync %v", err)
	}
	if _, err := a.syncExternalIMAPAccountFolder(context.Background(), "policy-account", "INBOX"); err == nil || err.Error() != "external imap access denied" {
		t.Fatalf("revoked folder sync %v", err)
	}
	var count int
	if err := a.db.QueryRow(`SELECT count(*) FROM external_imap_sync_runs WHERE account_id='policy-account'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("denied sync created work %d %v", count, err)
	}
	a.updateConfig(func(c *Config) { c.ExternalIMAPRestricted = false })
	if code := otherClient.do("GET", "/api/me/external-imap-accounts", nil, nil); code != http.StatusOK {
		t.Fatalf("all mode %d", code)
	}
}

func TestExternalIMAPAccessPolicyPersistsAndOldClientsCannotReset(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, nil); code != 200 {
		t.Fatal(code)
	}
	owner, _ := defaultAdminUserAndMailbox(t, a)
	payload := systemSettingsPayload(a.systemSettingsSnapshot())
	payload["externalImapRestricted"] = true
	payload["externalImapUserIds"] = []string{owner.ID, owner.ID}
	var result SystemSettings
	if code := admin.do("POST", "/api/admin/settings", payload, &result); code != 200 || !result.ExternalIMAPRestricted || len(result.ExternalIMAPUserIDs) != 1 {
		t.Fatalf("save policy %d %+v", code, result)
	}
	a.updateConfig(func(c *Config) { c.ExternalIMAPRestricted = false; c.ExternalIMAPUserIDs = "" })
	if err := a.loadPersistedSystemSettings(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !a.config().ExternalIMAPRestricted || a.config().ExternalIMAPUserIDs != owner.ID {
		t.Fatal("policy not restored")
	}
	delete(payload, "externalImapRestricted")
	delete(payload, "externalImapUserIds")
	if code := admin.do("POST", "/api/admin/settings", payload, &result); code != 200 || !result.ExternalIMAPRestricted || len(result.ExternalIMAPUserIDs) != 1 {
		t.Fatalf("legacy client reset policy %d", code)
	}
	payload["externalImapUserIds"] = []string{"nonexistent"}
	if code := admin.do("POST", "/api/admin/settings", payload, nil); code != 400 {
		t.Fatalf("unknown user accepted %d", code)
	}
}
