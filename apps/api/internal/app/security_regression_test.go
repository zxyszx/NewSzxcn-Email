package app

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoginRateLimitByAccountExpires(t *testing.T) {
	a := newTestApp(t)
	now := time.Date(2026, time.October, 24, 10, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	server := httptest.NewServer(a.Router())
	defer server.Close()
	client := &testClient{t: t, server: server}
	var body map[string]any
	for attempt := 1; attempt <= 5; attempt++ {
		status := client.do(http.MethodPost, "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "wrong-password"}, &body)
		if attempt < 5 && status != http.StatusUnauthorized {
			t.Fatalf("attempt %d status=%d, want 401", attempt, status)
		}
		if attempt == 5 && status != http.StatusTooManyRequests {
			t.Fatalf("attempt %d status=%d, want 429", attempt, status)
		}
	}
	if status := client.do(http.MethodPost, "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &body); status != http.StatusTooManyRequests {
		t.Fatalf("blocked correct login status=%d, want 429", status)
	}
	now = now.Add(authBlockTime + time.Second)
	if status := client.do(http.MethodPost, "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &body); status != http.StatusOK {
		t.Fatalf("login after cooldown status=%d body=%v", status, body)
	}
}

func TestPasswordChangeRevokesEverySession(t *testing.T) {
	a := newTestApp(t)
	server := httptest.NewServer(a.Router())
	defer server.Close()
	first := &testClient{t: t, server: server}
	second := &testClient{t: t, server: server}
	for _, client := range []*testClient{first, second} {
		var login map[string]any
		if status := client.do(http.MethodPost, "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); status != http.StatusOK {
			t.Fatalf("login status=%d body=%v", status, login)
		}
	}
	var response map[string]any
	if status := first.do(http.MethodPost, "/api/me/password", map[string]string{"currentPassword": "ChangeMe123!", "newPassword": "ChangedPassword123!"}, &response); status != http.StatusOK {
		t.Fatalf("password change status=%d body=%v", status, response)
	}
	for index, client := range []*testClient{first, second} {
		if status := client.do(http.MethodGet, "/api/me", nil, &response); status != http.StatusUnauthorized {
			t.Fatalf("session %d remained valid: status=%d", index+1, status)
		}
	}
}

func TestAdministratorPasswordResetRevokesEverySession(t *testing.T) {
	a := newTestApp(t)
	server := httptest.NewServer(a.Router())
	defer server.Close()
	first := &testClient{t: t, server: server}
	second := &testClient{t: t, server: server}
	for _, client := range []*testClient{first, second} {
		var login map[string]any
		if status := client.do(http.MethodPost, "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); status != http.StatusOK {
			t.Fatalf("login status=%d body=%v", status, login)
		}
	}
	user, _ := defaultAdminUserAndMailbox(t, a)
	var response map[string]any
	if status := first.do(http.MethodPost, "/api/admin/users/"+user.ID+"/password", map[string]string{"password": "ResetPassword123!"}, &response); status != http.StatusOK {
		t.Fatalf("password reset status=%d body=%v", status, response)
	}
	for index, client := range []*testClient{first, second} {
		if status := client.do(http.MethodGet, "/api/me", nil, &response); status != http.StatusUnauthorized {
			t.Fatalf("session %d remained valid: status=%d", index+1, status)
		}
	}
}

func TestTwoFactorChallengeHasAttemptLimit(t *testing.T) {
	a := newTestApp(t)
	secret, err := newTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	user, _ := defaultAdminUserAndMailbox(t, a)
	if _, err := a.db.Exec(`UPDATE users SET two_factor_enabled=1,two_factor_secret=? WHERE id=?`, secret, user.ID); err != nil {
		t.Fatal(err)
	}
	token, err := a.createLoginChallenge(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(a.Router())
	defer server.Close()
	client := &testClient{t: t, server: server}
	var body map[string]any
	for attempt := 1; attempt <= 5; attempt++ {
		status := client.do(http.MethodPost, "/api/auth/login", map[string]string{"challengeToken": token, "twoFactorCode": "000000"}, &body)
		if attempt < 5 && status != http.StatusUnauthorized {
			t.Fatalf("attempt %d status=%d, want 401", attempt, status)
		}
		if attempt == 5 && status != http.StatusTooManyRequests {
			t.Fatalf("attempt %d status=%d, want 429", attempt, status)
		}
	}
	if _, err := a.loginChallengeByToken(context.Background(), token); err == nil {
		t.Fatal("challenge remained usable after reaching the attempt limit")
	}
}

func TestCORSUsesExactOriginsAndAllowsMutationMethods(t *testing.T) {
	a := newTestApp(t)
	handler := a.Router()
	for _, test := range []struct {
		origin  string
		allowed bool
	}{
		{origin: "http://localhost:5173", allowed: true},
		{origin: "http://127.0.0.1:4173", allowed: true},
		{origin: "http://localhost:5173.evil.example", allowed: false},
		{origin: "https://evil.example", allowed: false},
	} {
		req := httptest.NewRequest(http.MethodOptions, "/api/me/profile", nil)
		req.Header.Set("Origin", test.origin)
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)
		if got := resp.Header().Get("Access-Control-Allow-Origin"); (got != "") != test.allowed {
			t.Fatalf("origin %q allowed=%v header=%q", test.origin, test.allowed, got)
		}
		if test.allowed {
			methods := resp.Header().Get("Access-Control-Allow-Methods")
			if !strings.Contains(methods, "PUT") || !strings.Contains(methods, "PATCH") {
				t.Fatalf("mutation methods missing: %q", methods)
			}
		}
	}
}

func TestExternalIMAPRejectsMixedPublicAndPrivateResolution(t *testing.T) {
	a := newTestApp(t)
	a.lookupIP = func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8"), net.ParseIP("127.0.0.1")}, nil
	}
	if _, err := a.resolveExternalIMAPHost(context.Background(), "rebind.example.test"); err == nil {
		t.Fatal("mixed public/private DNS result was accepted")
	}
}

func TestExternalIMAPPinsResolvedAddressesAndRetriesWithoutAnotherLookup(t *testing.T) {
	a := newTestApp(t)
	a.updateConfig(func(cfg *Config) { cfg.ExternalIMAPAllowPrivateHosts = true })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	reached := make(chan struct{}, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		reached <- struct{}{}
	}()
	lookups := 0
	a.lookupIP = func(context.Context, string, string) ([]net.IP, error) {
		lookups++
		return []net.IP{net.ParseIP("127.0.0.2"), net.ParseIP("127.0.0.1")}, nil
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_, _ = a.openExternalIMAPClient(context.Background(), externalIMAPAccountRecord{ExternalIMAPAccount: ExternalIMAPAccount{Host: "pinned.example.test", Port: port, TLSMode: externalIMAPPlain, Username: "fixture", AuthMode: externalIMAPAuthPassword}})
	select {
	case <-reached:
	case <-time.After(2 * time.Second):
		t.Fatal("validated fallback address was not dialed")
	}
	if lookups != 1 {
		t.Fatalf("DNS lookup count=%d, want 1", lookups)
	}
}

func TestExternalIMAPRejectsPrivateIPv6(t *testing.T) {
	a := newTestApp(t)
	a.lookupIP = func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("::1")}, nil
	}
	if _, err := a.resolveExternalIMAPHost(context.Background(), "ipv6.example.test"); err == nil {
		t.Fatal("private IPv6 result was accepted")
	}
}

func TestInboxShareReportsUnavailableWithoutEncryptionKey(t *testing.T) {
	a := newTestApp(t)
	a.updateConfig(func(cfg *Config) {
		cfg.UpdateServiceToken = ""
		cfg.ExternalIMAPSecretKey = ""
	})
	server := httptest.NewServer(a.Router())
	defer server.Close()
	client := &testClient{t: t, server: server}
	var body map[string]any
	if status := client.do(http.MethodPost, "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &body); status != http.StatusOK {
		t.Fatalf("login status=%d", status)
	}
	_, mailbox := defaultAdminUserAndMailbox(t, a)
	var settings inboxShareSettingsResponse
	if status := client.do(http.MethodGet, "/api/me/mailboxes/"+mailbox.ID+"/inbox-share", nil, &settings); status != http.StatusOK || settings.Available {
		t.Fatalf("settings status=%d available=%v", status, settings.Available)
	}
	if status := client.do(http.MethodPost, "/api/me/mailboxes/"+mailbox.ID+"/inbox-share", map[string]any{"windowMinutes": 30, "folderIds": settings.FolderIDs}, &body); status != http.StatusServiceUnavailable {
		t.Fatalf("create status=%d body=%v", status, body)
	}
}

func TestNewInstallRequiresAdministratorPassword(t *testing.T) {
	dir := t.TempDir()
	_, err := New(Config{
		Addr:              "127.0.0.1:0",
		DBPath:            filepath.Join(dir, "lanqin.db"),
		DataDir:           filepath.Join(dir, "data"),
		CookieName:        "lanqin_test",
		SessionTTLHours:   24,
		AdminEmail:        "admin@example.test",
		AllowInsecureHTTP: true,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil || !strings.Contains(err.Error(), "LANQIN_ADMIN_PASSWORD must be set") {
		t.Fatalf("New() error=%v", err)
	}
}

func TestPasswordMustNotEqualLoginIdentity(t *testing.T) {
	if !passwordMatchesIdentity("User@Example.test", "user@example.test") {
		t.Fatal("case-insensitive identity password was accepted")
	}
	if passwordMatchesIdentity("a-distinct-password", "user@example.test") {
		t.Fatal("distinct password was rejected")
	}
}

func TestCreateUserRejectsEmailAsPassword(t *testing.T) {
	a := newTestApp(t)
	server := httptest.NewServer(a.Router())
	defer server.Close()
	client := &testClient{t: t, server: server}
	var body map[string]any
	if status := client.do(http.MethodPost, "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &body); status != http.StatusOK {
		t.Fatalf("login status=%d", status)
	}
	email := "public@example.test"
	status := client.do(http.MethodPost, "/api/admin/users", map[string]any{"email": email, "displayName": "Public", "role": "user", "password": email}, &body)
	if status != http.StatusBadRequest {
		t.Fatalf("create user status=%d body=%v", status, body)
	}
}
