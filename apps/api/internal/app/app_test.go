package app

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	netmail "net/mail"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-sasl"
	smtpclient "github.com/emersion/go-smtp"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	cfg := Config{
		Addr:              ":0",
		DBPath:            filepath.Join(dir, "lanqin.db"),
		DataDir:           filepath.Join(dir, "data"),
		CookieName:        "lanqin_test",
		SessionTTLHours:   24,
		AdminEmail:        "admin@lanqin.local",
		AdminPassword:     "ChangeMe123!",
		PublicHostname:    "mail.example.test",
		PublicBaseURL:     "http://localhost:5173",
		AllowInsecureHTTP: true,
	}
	return newTestAppWithConfig(t, cfg)
}

func newTestAppWithConfig(t *testing.T, cfg Config) *App {
	t.Helper()
	a, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func stopTestWorkers(a *App) {
	if a != nil && a.workerCancel != nil {
		a.workerCancel()
		a.workerWG.Wait()
	}
}

func defaultAdminUserAndMailbox(t *testing.T, a *App) (*User, *Mailbox) {
	t.Helper()
	ctx := context.Background()
	user, _, err := a.userByEmail(ctx, "admin@lanqin.local")
	if err != nil {
		t.Fatal(err)
	}
	mb, err := a.mailboxByAddress(ctx, "admin@lanqin.local")
	if err != nil {
		t.Fatal(err)
	}
	return user, mb
}

func writeTestCertificateFiles(t *testing.T, hostname string) (string, string) {
	t.Helper()
	if strings.TrimSpace(hostname) == "" {
		hostname = "localhost"
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: hostname,
		},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{hostname, "localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(t.TempDir(), "cert.pem")
	keyPath := filepath.Join(filepath.Dir(certPath), "key.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

func startFakeSMTP(t *testing.T) (string, string, <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan string, 1)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleFakeSMTPConn(conn, received)
		}
	}()
	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return host, port, received
}

func startCapturingSMTP(t *testing.T, capacity int) (string, string, <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan string, capacity)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleFakeSMTPConn(conn, received)
		}
	}()
	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return host, port, received
}

func handleFakeSMTPConn(conn net.Conn, received chan<- string) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	_, _ = io.WriteString(conn, "220 lanqin.test ESMTP\r\n")
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO") || strings.HasPrefix(cmd, "HELO"):
			_, _ = io.WriteString(conn, "250-lanqin.test\r\n250 OK\r\n")
		case strings.HasPrefix(cmd, "DATA"):
			_, _ = io.WriteString(conn, "354 End data with <CR><LF>.<CR><LF>\r\n")
			var data strings.Builder
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimRight(line, "\r\n") == "." {
					break
				}
				data.WriteString(line)
			}
			select {
			case received <- data.String():
			default:
			}
			_, _ = io.WriteString(conn, "250 OK\r\n")
		case strings.HasPrefix(cmd, "QUIT"):
			_, _ = io.WriteString(conn, "221 Bye\r\n")
			return
		default:
			_, _ = io.WriteString(conn, "250 OK\r\n")
		}
	}
}

type testMIMEHeader interface {
	Get(string) string
}

func extractForwardingVerificationToken(t *testing.T, raw string) string {
	t.Helper()
	msg, err := netmail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("read verification message: %v", err)
	}
	body := extractMIMETextForTest(t, msg.Header, msg.Body)
	marker := "/api/verify-email?token="
	idx := strings.Index(body, marker)
	if idx < 0 {
		t.Fatalf("verification link not found in body: %q", body)
	}
	token := body[idx+len(marker):]
	if end := strings.IndexAny(token, "\"'<>\r\n\t "); end >= 0 {
		token = token[:end]
	}
	token, _ = url.QueryUnescape(token)
	if token == "" {
		t.Fatalf("verification token empty in body: %q", body)
	}
	return token
}

func extractMIMETextForTest(t *testing.T, header testMIMEHeader, body io.Reader) string {
	t.Helper()
	contentType := header.Get("Content-Type")
	mediaType, params, _ := mime.ParseMediaType(contentType)
	if strings.HasPrefix(strings.ToLower(mediaType), "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			t.Fatalf("multipart message missing boundary: %s", contentType)
		}
		mr := multipart.NewReader(body, boundary)
		var out strings.Builder
		for {
			part, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatalf("read mime part: %v", err)
			}
			out.WriteString(extractMIMETextForTest(t, part.Header, part))
			out.WriteString("\n")
		}
		return out.String()
	}
	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read mime body: %v", err)
	}
	if strings.EqualFold(strings.TrimSpace(header.Get("Content-Transfer-Encoding")), "base64") {
		decoded, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(string(data)), ""))
		if err != nil {
			t.Fatalf("decode mime base64: %v", err)
		}
		data = decoded
	}
	return string(data)
}

type testClient struct {
	t      *testing.T
	server *httptest.Server
	cookie *http.Cookie
	bearer string
}

func (c *testClient) do(method, path string, body any, out any) int {
	return c.doWithHeaders(method, path, body, nil, out)
}

func (c *testClient) doWithHeaders(method, path string, body any, headers map[string]string, out any) int {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.server.URL+path, reader)
	if err != nil {
		c.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	for _, cookie := range resp.Cookies() {
		if strings.Contains(cookie.Name, "lanqin") && cookie.Value != "" {
			c.cookie = cookie
		}
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			c.t.Fatalf("decode %s %s: %v", method, path, err)
		}
	} else {
		_, _ = io.Copy(io.Discard, resp.Body)
	}
	return resp.StatusCode
}

func createTestDomain(t *testing.T, admin *testClient, name string) Domain {
	t.Helper()
	var domain Domain
	if code := admin.do("POST", "/api/admin/domains", map[string]string{"name": name}, &domain); code != http.StatusCreated {
		t.Fatalf("create domain %s code=%d domain=%+v", name, code, domain)
	}
	return domain
}

func createTestMailbox(t *testing.T, admin *testClient, domainID, localPart, displayName, password string, extra map[string]any) Mailbox {
	t.Helper()
	payload := map[string]any{"domainId": domainID, "localPart": localPart, "displayName": displayName, "password": password}
	for key, value := range extra {
		payload[key] = value
	}
	var mailbox Mailbox
	if code := admin.do("POST", "/api/admin/mailboxes", payload, &mailbox); code != http.StatusCreated {
		t.Fatalf("create mailbox %s code=%d mailbox=%+v", localPart, code, mailbox)
	}
	return mailbox
}

func TestAdminMailboxCreationUsesOwnerPasswordAndQuota(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}
	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d body=%v", code, login)
	}
	adminUser, adminMailbox := defaultAdminUserAndMailbox(t, a)
	adminDetails, err := a.adminUserByID(context.Background(), adminUser.ID)
	if err != nil {
		t.Fatal(err)
	}
	if adminDetails.StorageQuotaMB != defaultAdminStorageQuotaMB {
		t.Fatalf("administrator storage quota=%d, want %d", adminDetails.StorageQuotaMB, defaultAdminStorageQuotaMB)
	}
	domainID := mustDefaultDomainID(t, a)

	var secondary Mailbox
	if code := admin.do("POST", "/api/admin/mailboxes", map[string]any{
		"domainId":  domainID,
		"localPart": "admin-secondary",
		"userId":    adminUser.ID,
	}, &secondary); code != http.StatusCreated {
		t.Fatalf("create admin secondary mailbox code=%d", code)
	}
	if secondary.QuotaMB != 0 {
		t.Fatalf("admin secondary quota=%d, want unlimited", secondary.QuotaMB)
	}

	var primaryHash, secondaryHash string
	if err := a.db.QueryRow(`SELECT password_hash FROM mailboxes WHERE id=?`, adminMailbox.ID).Scan(&primaryHash); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow(`SELECT password_hash FROM mailboxes WHERE id=?`, secondary.ID).Scan(&secondaryHash); err != nil {
		t.Fatal(err)
	}
	if primaryHash != secondaryHash {
		t.Fatal("admin secondary mailbox did not inherit the owner password")
	}

	var regular AdminUser
	if code := admin.do("POST", "/api/admin/users", map[string]any{
		"email":       "owner@lanqin.local",
		"displayName": "Owner",
		"password":    "OwnerPassword123!",
		"role":        "user",
	}, &regular); code != http.StatusCreated {
		t.Fatalf("create regular owner code=%d", code)
	}
	if regular.MailboxCount != 1 {
		t.Fatalf("new account mailbox count=%d, want one protected primary mailbox", regular.MailboxCount)
	}
	if regular.StorageQuotaMB != defaultUserStorageQuotaMB {
		t.Fatalf("regular account storage quota=%d, want %d", regular.StorageQuotaMB, defaultUserStorageQuotaMB)
	}
	if _, err := a.mailboxByAddress(context.Background(), regular.Email); err != nil {
		t.Fatalf("new account primary mailbox missing: %v", err)
	}
	var primaryStatusErr map[string]any
	primaryMailbox, err := a.mailboxByAddress(context.Background(), regular.Email)
	if err != nil {
		t.Fatal(err)
	}
	if code := admin.do("POST", "/api/admin/mailboxes/"+primaryMailbox.ID, map[string]any{
		"userId": regular.ID, "displayName": primaryMailbox.DisplayName, "quotaMb": primaryMailbox.QuotaMB, "status": "disabled",
	}, &primaryStatusErr); code != http.StatusBadRequest {
		t.Fatalf("primary mailbox status update code=%d body=%v", code, primaryStatusErr)
	}
	var regularMailbox Mailbox
	if code := admin.do("POST", "/api/admin/mailboxes", map[string]any{
		"domainId":  domainID,
		"localPart": "owner-secondary",
		"userId":    regular.ID,
	}, &regularMailbox); code != http.StatusCreated {
		t.Fatalf("create regular secondary mailbox code=%d", code)
	}
	if regularMailbox.QuotaMB != defaultUserStorageQuotaMB {
		t.Fatalf("regular secondary quota=%d, want %d", regularMailbox.QuotaMB, defaultUserStorageQuotaMB)
	}
	var userHash, mailboxHash string
	if err := a.db.QueryRow(`SELECT password_hash FROM users WHERE id=?`, regular.ID).Scan(&userHash); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow(`SELECT password_hash FROM mailboxes WHERE id=?`, regularMailbox.ID).Scan(&mailboxHash); err != nil {
		t.Fatal(err)
	}
	if userHash != mailboxHash {
		t.Fatal("regular secondary mailbox did not inherit the owner password")
	}
}

func TestAdministratorAccountAndPrimaryMailboxesCannotBeDeleted(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}
	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d body=%v", code, login)
	}
	adminUser, adminMailbox := defaultAdminUserAndMailbox(t, a)
	var errBody map[string]any
	if code := admin.do("DELETE", "/api/admin/users/"+adminUser.ID, nil, &errBody); code != http.StatusBadRequest {
		t.Fatalf("administrator account delete code=%d body=%v", code, errBody)
	}
	if code := admin.do("DELETE", "/api/admin/mailboxes/"+adminMailbox.ID, nil, &errBody); code != http.StatusBadRequest {
		t.Fatalf("administrator primary mailbox delete code=%d body=%v", code, errBody)
	}
	if code := admin.do("POST", "/api/admin/users/"+adminUser.ID, map[string]any{
		"email": adminUser.Email, "displayName": adminUser.DisplayName, "role": "admin", "disabled": false, "storageQuotaMb": 99,
	}, &errBody); code != http.StatusBadRequest {
		t.Fatalf("storage quota below 100 MB code=%d body=%v", code, errBody)
	}
	var updatedAdmin AdminUser
	if code := admin.do("POST", "/api/admin/users/"+adminUser.ID, map[string]any{
		"email": adminUser.Email, "displayName": adminUser.DisplayName, "role": "admin", "disabled": false, "storageQuotaMb": 100,
	}, &updatedAdmin); code != http.StatusOK || updatedAdmin.StorageQuotaMB != 100 {
		t.Fatalf("administrator storage quota code=%d user=%+v", code, updatedAdmin)
	}

	var regular AdminUser
	if code := admin.do("POST", "/api/admin/users", map[string]any{
		"email": "protected-primary@lanqin.local", "displayName": "Protected Primary", "role": "user", "password": "Password123!",
	}, &regular); code != http.StatusCreated {
		t.Fatalf("create regular user code=%d user=%+v", code, regular)
	}
	primary, err := a.mailboxByAddress(context.Background(), regular.Email)
	if err != nil {
		t.Fatal(err)
	}
	if code := admin.do("DELETE", "/api/admin/mailboxes/"+primary.ID, nil, &errBody); code != http.StatusBadRequest {
		t.Fatalf("regular primary mailbox delete code=%d body=%v", code, errBody)
	}
	secondary := createTestMailbox(t, admin, primary.DomainID, "deletable-secondary", "Secondary", "", map[string]any{"userId": regular.ID})
	if code := admin.do("DELETE", "/api/admin/mailboxes/"+secondary.ID, nil, &errBody); code != http.StatusOK {
		t.Fatalf("secondary mailbox delete code=%d body=%v", code, errBody)
	}
}

func createTestAPIToken(t *testing.T, client *testClient, name string) string {
	return createTestAPITokenWithScopes(t, client, name, nil)
}

func createTestAPITokenWithScopes(t *testing.T, client *testClient, name string, scopes []string) string {
	t.Helper()
	var resp struct {
		Token string   `json:"token"`
		Item  APIToken `json:"item"`
	}
	payload := map[string]any{"name": name}
	if scopes != nil {
		payload["scopes"] = scopes
	}
	if code := client.do("POST", "/api/me/api-tokens", payload, &resp); code != http.StatusCreated {
		t.Fatalf("create api token code=%d resp=%+v", code, resp)
	}
	if resp.Token == "" || resp.Item.ID == "" || resp.Item.Name != name {
		t.Fatalf("api token response=%+v", resp)
	}
	return resp.Token
}

func updateRegularPermissionGroup(t *testing.T, admin *testClient, permissions []string) PermissionGroup {
	t.Helper()
	var group PermissionGroup
	if code := admin.do("POST", "/api/admin/permission-groups/"+PermissionGroupRegular, map[string]any{
		"name":        "Regular Users",
		"description": "Default permissions for regular users",
		"permissions": permissions,
	}, &group); code != http.StatusOK {
		t.Fatalf("update regular permission group code=%d group=%+v", code, group)
	}
	return group
}

func updateRegularPermissionGroupWithLimits(t *testing.T, admin *testClient, permissions []string, limits PermissionLimits) PermissionGroup {
	t.Helper()
	var group PermissionGroup
	if code := admin.do("POST", "/api/admin/permission-groups/"+PermissionGroupRegular, map[string]any{
		"name":        "Regular Users",
		"description": "Default permissions for regular users",
		"permissions": permissions,
		"limits":      limits,
	}, &group); code != http.StatusOK {
		t.Fatalf("update regular permission group limits code=%d group=%+v", code, group)
	}
	return group
}

func setRegularPermissionGroupForTest(t *testing.T, a *App, permissions []string, limits PermissionLimits) PermissionGroup {
	t.Helper()
	now := a.now().UTC().Format(time.RFC3339Nano)
	if _, err := a.db.ExecContext(context.Background(), `UPDATE permission_groups SET permissions_json=?, limits_json=?, updated_at=? WHERE id=?`,
		encodePermissions(permissions), encodePermissionLimits(limits), now, PermissionGroupRegular); err != nil {
		t.Fatalf("set regular permission group fixture: %v", err)
	}
	group, err := a.permissionGroupByID(context.Background(), PermissionGroupRegular)
	if err != nil {
		t.Fatalf("load regular permission group fixture: %v", err)
	}
	return *group
}

func systemSettingsPayload(settings SystemSettings) map[string]any {
	return map[string]any{
		"publicHostname":                  settings.PublicHostname,
		"publicBaseUrl":                   settings.PublicBaseURL,
		"smtpHost":                        settings.SMTPHost,
		"smtpPort":                        settings.SMTPPort,
		"smtpUsername":                    settings.SMTPUsername,
		"smtpPassword":                    "",
		"smtpRequireTls":                  settings.SMTPRequireTLS,
		"maildirRoot":                     settings.MaildirRoot,
		"maildirScanSeconds":              settings.MaildirScanSeconds,
		"sessionTtlHours":                 settings.SessionTTLHours,
		"allowInsecureHttp":               settings.AllowInsecureHTTP,
		"openRegistration":                settings.OpenRegistration,
		"twoFactorEnabled":                settings.TwoFactorEnabled,
		"turnstileEnabled":                settings.TurnstileEnabled,
		"turnstileSiteKey":                settings.TurnstileSiteKey,
		"turnstileSecretKey":              "",
		"catchAllEnabled":                 settings.CatchAllEnabled,
		"mailAutoRefresh":                 settings.MailAutoRefresh,
		"mailRefreshSeconds":              settings.MailRefreshSeconds,
		"userMailboxApplyEnabled":         settings.UserMailboxApplyEnabled,
		"userMailboxDomainIds":            settings.UserMailboxDomainIDs,
		"reservedMailboxPrefixes":         settings.ReservedMailboxPrefixes,
		"externalImapEnabled":             settings.ExternalIMAPEnabled,
		"externalImapSecretKey":           "",
		"externalImapSyncSeconds":         settings.ExternalIMAPSyncSeconds,
		"externalImapAllowPrivateHosts":   settings.ExternalIMAPAllowPrivateHosts,
		"externalImapGmailClientId":       settings.ExternalIMAPGmailClientID,
		"externalImapGmailClientSecret":   "",
		"externalImapOutlookClientId":     settings.ExternalIMAPOutlookClientID,
		"externalImapOutlookClientSecret": "",
		"telegramMailEnabled":             settings.TelegramMailEnabled,
		"telegramBotToken":                "",
		"telegramPrivateChatId":           settings.TelegramPrivateChatID,
		"telegramBodyMode":                settings.TelegramBodyMode,
		"telegramMailboxIds":              settings.TelegramMailboxIDs,
		"telegramIncludeUnregistered":     settings.TelegramIncludeUnregistered,
	}
}

func TestAuthAdminAndLocalDeliveryFlow(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d body=%v", code, login)
	}

	var domainList = struct {
		Items []Domain `json:"items"`
	}{}
	if code := admin.do("GET", "/api/admin/domains", nil, &domainList); code != http.StatusOK || len(domainList.Items) == 0 {
		t.Fatalf("list domains code=%d items=%+v", code, domainList.Items)
	}
	domainID := domainList.Items[0].ID

	mb1 := createTestMailbox(t, admin, domainID, "alice", "Alice", "Password123!", nil)
	mb2 := createTestMailbox(t, admin, domainID, "bob", "Bob", "Password123!", nil)

	var alias Alias
	if code := admin.do("POST", "/api/admin/aliases", map[string]any{"domainId": domainID, "source": "sales", "destination": mb1.Address}, &alias); code != http.StatusCreated {
		t.Fatalf("alias code=%d alias=%+v", code, alias)
	}

	alice := &testClient{t: t, server: ts}
	if code := alice.do("POST", "/api/auth/login", map[string]string{"email": mb1.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("alice login=%d", code)
	}
	payload := map[string]any{
		"to":          []string{mb2.Address},
		"subject":     "hello bob",
		"html":        "<p>Hello <strong>Bob</strong></p><script>alert(1)</script>",
		"attachments": []map[string]string{{"filename": "note.txt", "contentType": "text/plain", "contentBase64": base64.StdEncoding.EncodeToString([]byte("hi"))}},
	}
	var sent MailMessage
	if code := alice.do("POST", "/api/mail/send", payload, &sent); code != http.StatusCreated || !sent.HasAttachments {
		t.Fatalf("send code=%d msg=%+v", code, sent)
	}

	bob := &testClient{t: t, server: ts}
	if code := bob.do("POST", "/api/auth/login", map[string]string{"email": mb2.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("bob login=%d", code)
	}
	var list struct {
		Items      []MailMessage `json:"items"`
		NextCursor string        `json:"nextCursor"`
	}
	if code := bob.do("GET", "/api/mail/messages?folder=Inbox", nil, &list); code != http.StatusOK || len(list.Items) != 1 {
		t.Fatalf("bob inbox code=%d items=%d", code, len(list.Items))
	}
	if strings.Contains(list.Items[0].Snippet, "script") {
		t.Fatalf("message was not sanitized: %q", list.Items[0].Snippet)
	}

	var detail MailMessage
	if code := bob.do("GET", "/api/mail/messages/"+list.Items[0].ID, nil, &detail); code != http.StatusOK || len(detail.Attachments) != 1 || !detail.IsRead {
		t.Fatalf("detail code=%d detail=%+v", code, detail)
	}
	if strings.Contains(detail.BodyHTML, "script") {
		t.Fatalf("html was not sanitized: %s", detail.BodyHTML)
	}

	var ok map[string]any
	if code := bob.do("POST", "/api/mail/messages/"+detail.ID+"/star", map[string]bool{"starred": true}, &ok); code != http.StatusOK {
		t.Fatalf("star code=%d", code)
	}
	if code := bob.do("POST", "/api/mail/messages/"+detail.ID+"/move", map[string]string{"folder": "Archive"}, &ok); code != http.StatusOK {
		t.Fatalf("move code=%d", code)
	}
	var labelUpdate struct {
		Labels []MailLabel `json:"labels"`
	}
	if code := bob.do("POST", "/api/mail/messages/"+detail.ID+"/labels", map[string]string{"name": "重要"}, &labelUpdate); code != http.StatusOK || len(labelUpdate.Labels) != 1 {
		t.Fatalf("add label code=%d labels=%+v", code, labelUpdate.Labels)
	}
	var labels struct {
		Items []MailLabel `json:"items"`
	}
	if code := bob.do("GET", "/api/mail/labels?mailboxId="+mb2.ID, nil, &labels); code != http.StatusOK || len(labels.Items) != len(defaultMailLabelDefs()) {
		t.Fatalf("labels code=%d items=%+v", code, labels.Items)
	}
	var importantLabel MailLabel
	for _, label := range labels.Items {
		if label.Name == "重要" {
			importantLabel = label
			break
		}
	}
	if importantLabel.ID == "" || importantLabel.MessageCount != 1 || importantLabel.UnreadCount != 0 {
		t.Fatalf("important label missing or count is wrong: %+v", labels.Items)
	}
	var labeled struct {
		Items []MailMessage `json:"items"`
	}
	if code := bob.do("GET", "/api/mail/messages?mailboxId="+mb2.ID+"&labelId="+importantLabel.ID, nil, &labeled); code != http.StatusOK || len(labeled.Items) != 1 || labeled.Items[0].ID != detail.ID {
		t.Fatalf("labeled messages code=%d items=%+v", code, labeled.Items)
	}
	if code := bob.do("DELETE", "/api/mail/messages/"+detail.ID+"/labels/"+importantLabel.ID, nil, &labelUpdate); code != http.StatusOK || len(labelUpdate.Labels) != 0 {
		t.Fatalf("remove label code=%d labels=%+v", code, labelUpdate.Labels)
	}
	var starred struct {
		Items []MailMessage `json:"items"`
	}
	if code := bob.do("GET", "/api/mail/starred", nil, &starred); code != http.StatusOK || len(starred.Items) != 1 || starred.Items[0].ID != detail.ID || starred.Items[0].Folder != "Archive" {
		t.Fatalf("starred view code=%d items=%+v", code, starred.Items)
	}
	if code := bob.do("DELETE", "/api/mail/messages/"+detail.ID, nil, &ok); code != http.StatusOK {
		t.Fatalf("delete code=%d", code)
	}
}

func TestExternalIMAPAccountEncryptsPasswordAndDoesNotReturnSecret(t *testing.T) {
	dir := t.TempDir()
	a := newTestAppWithConfig(t, Config{
		Addr:                          ":0",
		DBPath:                        filepath.Join(dir, "lanqin.db"),
		DataDir:                       filepath.Join(dir, "data"),
		CookieName:                    "lanqin_test",
		SessionTTLHours:               24,
		AdminEmail:                    "admin@lanqin.local",
		AdminPassword:                 "ChangeMe123!",
		PublicHostname:                "mail.example.test",
		PublicBaseURL:                 "http://localhost:5173",
		AllowInsecureHTTP:             true,
		ExternalIMAPEnabled:           true,
		ExternalIMAPSecretKey:         "test-secret",
		ExternalIMAPAllowPrivateHosts: true,
	})
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, nil); code != http.StatusOK {
		t.Fatalf("login code=%d", code)
	}
	_, mb := defaultAdminUserAndMailbox(t, a)
	var created ExternalIMAPAccount
	payload := map[string]any{"mailboxId": mb.ID, "name": "Gmail", "host": "imap.gmail.com", "port": 993, "tlsMode": "tls", "username": "user@gmail.com", "password": "app-password", "storageMode": "remote", "syncReadState": true, "enabled": true}
	if code := admin.do("POST", "/api/me/external-imap-accounts", payload, &created); code != http.StatusCreated {
		t.Fatalf("create external imap code=%d account=%+v", code, created)
	}
	raw, err := json.Marshal(created)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "app-password") {
		t.Fatalf("external account response leaked password: %s", string(raw))
	}
	var ciphertext string
	if err := a.db.QueryRow(`SELECT password_ciphertext FROM external_imap_accounts WHERE id=?`, created.ID).Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	if ciphertext == "" || ciphertext == "app-password" {
		t.Fatalf("password was not encrypted: %q", ciphertext)
	}
	plain, err := a.decryptExternalIMAPPassword(ciphertext)
	if err != nil || plain != "app-password" {
		t.Fatalf("decrypt password=%q err=%v", plain, err)
	}
	var list struct {
		Items []map[string]any `json:"items"`
	}
	if code := admin.do("GET", "/api/me/external-imap-accounts?mailboxId="+mb.ID, nil, &list); code != http.StatusOK || len(list.Items) != 1 {
		t.Fatalf("list external imap code=%d items=%+v", code, list.Items)
	}
	if _, ok := list.Items[0]["password"]; ok {
		t.Fatalf("list response exposed password field: %+v", list.Items[0])
	}
}

func TestExternalIMAPDisabledByDefaultAndAdminSettings(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, nil); code != http.StatusOK {
		t.Fatalf("login code=%d", code)
	}
	_, mb := defaultAdminUserAndMailbox(t, a)
	payload := map[string]any{"mailboxId": mb.ID, "name": "Disabled", "host": "imap.example.com", "port": 993, "tlsMode": "tls", "username": "user@example.com", "password": "secret", "storageMode": "remote"}
	var body map[string]any
	if code := admin.do("POST", "/api/me/external-imap-accounts", payload, &body); code != http.StatusForbidden {
		t.Fatalf("external imap should be disabled by default code=%d body=%v", code, body)
	}
	var public PublicSettings
	if code := admin.do("GET", "/api/public/settings", nil, &public); code != http.StatusOK || public.ExternalIMAPEnabled {
		t.Fatalf("public settings should expose disabled external imap code=%d settings=%+v", code, public)
	}
	var settings SystemSettings
	if code := admin.do("GET", "/api/admin/settings", nil, &settings); code != http.StatusOK {
		t.Fatalf("get settings code=%d", code)
	}
	update := systemSettingsPayload(settings)
	update["externalImapEnabled"] = true
	if code := admin.do("POST", "/api/admin/settings", update, &body); code != http.StatusBadRequest {
		t.Fatalf("enable without secret should fail code=%d body=%v", code, body)
	}
	update["externalImapSecretKey"] = "test-secret"
	update["externalImapSyncSeconds"] = 120
	update["externalImapAllowPrivateHosts"] = true
	update["externalImapGmailClientId"] = "gmail-client"
	update["externalImapGmailClientSecret"] = "gmail-secret"
	update["externalImapOutlookClientId"] = "outlook-client"
	update["externalImapOutlookClientSecret"] = "outlook-secret"
	if code := admin.do("POST", "/api/admin/settings", update, &settings); code != http.StatusOK || !settings.ExternalIMAPEnabled || !settings.ExternalIMAPSecretSet || settings.ExternalIMAPSyncSeconds != 120 || !settings.ExternalIMAPAllowPrivateHosts || !settings.ExternalIMAPGmailClientSecretSet || !settings.ExternalIMAPOutlookClientSecretSet {
		t.Fatalf("enable external imap code=%d settings=%+v", code, settings)
	}
	if settings.ExternalIMAPGmailClientID != "gmail-client" || settings.ExternalIMAPOutlookClientID != "outlook-client" {
		t.Fatalf("oauth client ids not saved: %+v", settings)
	}
	if a.config().ExternalIMAPSecretKey != "test-secret" || a.config().ExternalIMAPGmailClientSecret != "gmail-secret" || a.config().ExternalIMAPOutlookClientSecret != "outlook-secret" {
		t.Fatalf("secret settings not persisted in config")
	}
	if code := admin.do("GET", "/api/public/settings", nil, &public); code != http.StatusOK || !public.ExternalIMAPEnabled {
		t.Fatalf("public settings should expose enabled external imap code=%d settings=%+v", code, public)
	}
}

func TestExternalIMAPRejectsPrivateHostsByDefault(t *testing.T) {
	a := newTestApp(t)
	a.updateConfig(func(cfg *Config) { cfg.ExternalIMAPEnabled = true })
	a.updateConfig(func(cfg *Config) { cfg.ExternalIMAPSecretKey = "test-secret" })
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, nil); code != http.StatusOK {
		t.Fatalf("login code=%d", code)
	}
	_, mb := defaultAdminUserAndMailbox(t, a)
	var out map[string]any
	payload := map[string]any{"mailboxId": mb.ID, "name": "Local", "host": "127.0.0.1", "port": 143, "tlsMode": "plain", "username": "local", "password": "secret", "storageMode": "remote"}
	if code := admin.do("POST", "/api/me/external-imap-accounts", payload, &out); code != http.StatusBadRequest {
		t.Fatalf("private host should be rejected code=%d body=%v", code, out)
	}
}

func TestExternalIMAPOAuthStateDoesNotDefaultToLocalMailbox(t *testing.T) {
	dir := t.TempDir()
	a := newTestAppWithConfig(t, Config{
		Addr:                            ":0",
		DBPath:                          filepath.Join(dir, "lanqin.db"),
		DataDir:                         filepath.Join(dir, "data"),
		CookieName:                      "lanqin_test",
		SessionTTLHours:                 24,
		AdminEmail:                      "admin@lanqin.local",
		AdminPassword:                   "ChangeMe123!",
		PublicHostname:                  "mail.example.test",
		PublicBaseURL:                   "http://localhost:5173",
		AllowInsecureHTTP:               true,
		ExternalIMAPEnabled:             true,
		ExternalIMAPSecretKey:           "test-secret",
		ExternalIMAPOutlookClientID:     "client-id",
		ExternalIMAPOutlookClientSecret: "client-secret",
	})
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, nil); code != http.StatusOK {
		t.Fatalf("login code=%d", code)
	}
	_, mb := defaultAdminUserAndMailbox(t, a)

	var start struct {
		URL string `json:"url"`
	}
	if code := admin.do("POST", "/api/me/external-imap-oauth/outlook/start", map[string]any{"mailboxId": mb.ID, "storageMode": "local", "syncReadState": true, "enabled": true}, &start); code != http.StatusOK {
		t.Fatalf("start oauth code=%d url=%q", code, start.URL)
	}
	stateValue := mustOAuthStateFromURL(t, start.URL)
	state, err := a.decryptExternalIMAPOAuthState(stateValue)
	if err != nil {
		t.Fatal(err)
	}
	if state.Email != "" {
		t.Fatalf("oauth state defaulted to local mailbox email: %q", state.Email)
	}

	if code := admin.do("POST", "/api/me/external-imap-oauth/outlook/start", map[string]any{"mailboxId": mb.ID, "email": "User@Example.COM", "storageMode": "remote", "syncReadState": true, "enabled": true}, &start); code != http.StatusOK {
		t.Fatalf("start oauth with email code=%d url=%q", code, start.URL)
	}
	stateValue = mustOAuthStateFromURL(t, start.URL)
	state, err = a.decryptExternalIMAPOAuthState(stateValue)
	if err != nil {
		t.Fatal(err)
	}
	if state.Email != "user@example.com" {
		t.Fatalf("oauth state did not preserve requested external email, got %q", state.Email)
	}
}

func TestExternalIMAPOAuthEmailFromIDToken(t *testing.T) {
	token := (&oauth2.Token{AccessToken: "access"}).WithExtra(map[string]any{
		"id_token": testIDToken(map[string]any{"preferred_username": "User@Example.COM"}),
	})
	email, err := externalIMAPOAuthEmail(externalIMAPOAuthOutlook, token)
	if err != nil {
		t.Fatal(err)
	}
	if email != "user@example.com" {
		t.Fatalf("unexpected outlook oauth email %q", email)
	}

	token = (&oauth2.Token{AccessToken: "access"}).WithExtra(map[string]any{
		"id_token": testIDToken(map[string]any{"email": "Person@Gmail.COM"}),
	})
	email, err = externalIMAPOAuthEmail(externalIMAPOAuthGmail, token)
	if err != nil {
		t.Fatal(err)
	}
	if email != "person@gmail.com" {
		t.Fatalf("unexpected gmail oauth email %q", email)
	}
}

func TestExternalIMAPXOAUTH2ClientFormat(t *testing.T) {
	client := newExternalIMAPXOAUTH2Client("user@example.com", "access-token")
	mech, initialResponse, err := client.Start()
	if err != nil {
		t.Fatal(err)
	}
	if mech != "XOAUTH2" {
		t.Fatalf("mechanism=%q, want XOAUTH2", mech)
	}
	want := "user=user@example.com\x01auth=Bearer access-token\x01\x01"
	if string(initialResponse) != want {
		t.Fatalf("initial response=%q, want %q", string(initialResponse), want)
	}
}

func mustOAuthStateFromURL(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	state := u.Query().Get("state")
	if state == "" {
		t.Fatalf("oauth url missing state: %s", rawURL)
	}
	return state
}

func testIDToken(claims map[string]any) string {
	header, _ := json.Marshal(map[string]any{"alg": "none"})
	payload, _ := json.Marshal(claims)
	return base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload) + "."
}

func TestExternalIMAPAccountOwnershipIsolation(t *testing.T) {
	dir := t.TempDir()
	a := newTestAppWithConfig(t, Config{
		Addr:                          ":0",
		DBPath:                        filepath.Join(dir, "lanqin.db"),
		DataDir:                       filepath.Join(dir, "data"),
		CookieName:                    "lanqin_test",
		SessionTTLHours:               24,
		AdminEmail:                    "admin@lanqin.local",
		AdminPassword:                 "ChangeMe123!",
		PublicHostname:                "mail.example.test",
		PublicBaseURL:                 "http://localhost:5173",
		AllowInsecureHTTP:             true,
		ExternalIMAPEnabled:           true,
		ExternalIMAPSecretKey:         "test-secret",
		ExternalIMAPAllowPrivateHosts: true,
	})
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, nil); code != http.StatusOK {
		t.Fatalf("login admin code=%d", code)
	}
	domainID := mustDefaultDomainID(t, a)
	owner := createTestMailbox(t, admin, domainID, "ximap-owner", "Owner", "Password123!", nil)
	other := createTestMailbox(t, admin, domainID, "ximap-other", "Other", "Password123!", nil)
	ownerClient := &testClient{t: t, server: ts}
	if code := ownerClient.do("POST", "/api/auth/login", map[string]string{"email": owner.Address, "password": "Password123!"}, nil); code != http.StatusOK {
		t.Fatalf("login owner code=%d", code)
	}
	otherClient := &testClient{t: t, server: ts}
	if code := otherClient.do("POST", "/api/auth/login", map[string]string{"email": other.Address, "password": "Password123!"}, nil); code != http.StatusOK {
		t.Fatalf("login other code=%d", code)
	}
	var created ExternalIMAPAccount
	payload := map[string]any{"mailboxId": owner.ID, "name": "Owner external", "host": "imap.example.com", "port": 993, "tlsMode": "tls", "username": "owner@example.com", "password": "secret", "storageMode": "remote"}
	if code := ownerClient.do("POST", "/api/me/external-imap-accounts", payload, &created); code != http.StatusCreated {
		t.Fatalf("create code=%d account=%+v", code, created)
	}
	var denied map[string]any
	if code := otherClient.do("POST", "/api/me/external-imap-accounts/"+created.ID, map[string]any{"mailboxId": other.ID, "name": "steal", "host": "imap.example.com", "port": 993, "tlsMode": "tls", "username": "other@example.com", "storageMode": "remote"}, &denied); code != http.StatusNotFound {
		t.Fatalf("cross-user update should be hidden code=%d body=%v", code, denied)
	}
	if code := otherClient.do("DELETE", "/api/me/external-imap-accounts/"+created.ID, nil, &denied); code != http.StatusNotFound {
		t.Fatalf("cross-user delete should be hidden code=%d body=%v", code, denied)
	}
}

func TestParseMailAuthenticationResults(t *testing.T) {
	header := textproto.MIMEHeader{}
	header.Add("Authentication-Results", "mx.example.test; spf=pass smtp.mailfrom=sender.example; dkim=fail (bad signature) header.d=sender.example; dmarc=none")
	header.Add("Received-SPF", "softfail (mx.example.test: transitioning domain) client-ip=192.0.2.10; envelope-from=sender@example.test")

	auth := parseMailAuthentication(header)
	if auth.SPF != "pass" || auth.DKIM != "fail" || auth.DMARC != "none" {
		t.Fatalf("unexpected auth summary: %+v", auth)
	}
	if !strings.Contains(auth.AuthenticationResults, "spf=pass") || !strings.Contains(auth.ReceivedSPF, "softfail") {
		t.Fatalf("raw auth headers not preserved: %+v", auth)
	}

	unknown := parseMailAuthentication(textproto.MIMEHeader{})
	if unknown.SPF != "unknown" || unknown.DKIM != "unknown" || unknown.DMARC != "unknown" {
		t.Fatalf("missing headers should be unknown, got %+v", unknown)
	}
}

func TestSnippetFromHTMLIgnoresStyleContent(t *testing.T) {
	html := `<html><head><style>body { margin: 0; padding: 24px; background: #f4f4f5; }</style><title>Hidden title</title></head><body><p>蓝钦AI 余额充值成功</p></body></html>`
	got := snippetFrom("", html)
	if strings.Contains(got, "body {") || strings.Contains(got, "margin:") || strings.Contains(got, "Hidden title") {
		t.Fatalf("snippet kept non-content html text: %q", got)
	}
	if !strings.Contains(got, "蓝钦AI 余额充值成功") {
		t.Fatalf("snippet missing body text: %q", got)
	}
}

func TestRebuildHTMLOnlyMessageSnippetsDropsStyleContent(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	_, mb := defaultAdminUserAndMailbox(t, a)
	folderID, err := a.ensureFolder(ctx, mb.ID, "Sent")
	if err != nil {
		t.Fatal(err)
	}
	now := a.now().UTC().Format(time.RFC3339Nano)
	bodyHTML := `<html><head><style>body { margin: 0; padding: 24px; background: #f4f4f5; }</style></head><body><p>hello readable body</p></body></html>`
	if _, err := a.db.ExecContext(ctx, `INSERT INTO messages(id,mailbox_id,folder_id,recipient_addr,message_uid,message_id,subject,from_addr,from_name,to_addrs,cc_addrs,bcc_addrs,sent_at,received_at,snippet,body_text,body_html,is_read,is_starred,has_attachments,size_bytes,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, "msg_css_snippet", mb.ID, folderID, "", newID("uid"), "<css-snippet@example.test>", "css", mb.Address, "", "[]", "[]", "[]", now, now, "body { margin: 0; padding: 24px; }", "", bodyHTML, 1, 0, 0, int64(len(bodyHTML)), now, now); err != nil {
		t.Fatal(err)
	}
	if err := a.rebuildHTMLOnlyMessageSnippets(ctx); err != nil {
		t.Fatal(err)
	}
	var snippet string
	if err := a.db.QueryRowContext(ctx, `SELECT snippet FROM messages WHERE id='msg_css_snippet'`).Scan(&snippet); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(snippet, "body {") || strings.Contains(snippet, "margin:") {
		t.Fatalf("snippet was not rebuilt: %q", snippet)
	}
	if snippet != "hello readable body" {
		t.Fatalf("snippet=%q, want body text", snippet)
	}
}

func TestMailRulesConditionGroupsAndActions(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("admin login code=%d", code)
	}
	domainID := mustDefaultDomainID(t, a)
	sender := createTestMailbox(t, admin, domainID, "rule-sender", "Rule Sender", "Password123!", nil)
	recipient := createTestMailbox(t, admin, domainID, "rule-recipient", "Rule Recipient", "Password123!", nil)

	rcpt := &testClient{t: t, server: ts}
	if code := rcpt.do("POST", "/api/auth/login", map[string]string{"email": recipient.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("recipient login=%d", code)
	}
	var rule MailRule
	rulePayload := map[string]any{
		"mailboxId":  recipient.ID,
		"name":       "priority archive",
		"matchMode":  "all",
		"conditions": []map[string]any{{"matchMode": "any", "conditions": []map[string]string{{"field": "from", "operator": "contains", "value": sender.Address}, {"field": "subject", "operator": "contains", "value": "urgent"}}}, {"field": "cc", "operator": "contains", "value": "lead@example.test"}, {"field": "attachment", "operator": "contains", "value": "plan.pdf"}, {"field": "size", "operator": "gte", "value": "10"}, {"field": "date", "operator": "after", "value": "2020-01-01"}},
		"actions":    []map[string]string{{"type": "label", "value": "Priority"}, {"type": "move", "value": "Archive"}, {"type": "mark-read"}, {"type": "star"}},
	}
	if code := rcpt.do("POST", "/api/me/rules", rulePayload, &rule); code != http.StatusCreated {
		t.Fatalf("create rule code=%d rule=%+v", code, rule)
	}

	senderClient := &testClient{t: t, server: ts}
	if code := senderClient.do("POST", "/api/auth/login", map[string]string{"email": sender.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("sender login=%d", code)
	}
	var sent MailMessage
	if code := senderClient.do("POST", "/api/mail/send", map[string]any{
		"to":          []string{recipient.Address},
		"cc":          []string{"lead@example.test"},
		"subject":     "quarterly update",
		"text":        "body",
		"attachments": []map[string]string{{"filename": "plan.pdf", "contentType": "application/pdf", "contentBase64": base64.StdEncoding.EncodeToString([]byte("rule attachment payload"))}},
	}, &sent); code != http.StatusCreated {
		t.Fatalf("send code=%d sent=%+v", code, sent)
	}

	var archived struct {
		Items []MailMessage `json:"items"`
	}
	if code := rcpt.do("GET", "/api/mail/messages?mailboxId="+recipient.ID+"&folder=Archive", nil, &archived); code != http.StatusOK || len(archived.Items) != 1 {
		t.Fatalf("archive list code=%d items=%+v", code, archived.Items)
	}
	msg := archived.Items[0]
	if !msg.IsRead || !msg.IsStarred {
		t.Fatalf("rule flags read=%v starred=%v", msg.IsRead, msg.IsStarred)
	}
	if len(msg.Labels) != 1 || msg.Labels[0].Name != "Priority" {
		t.Fatalf("labels=%+v, want Priority", msg.Labels)
	}
}

func TestMailRulesForwardingAction(t *testing.T) {
	a := newTestApp(t)
	stopTestWorkers(a)
	a.updateConfig(func(cfg *Config) { cfg.SMTPHost = "127.0.0.1" })
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("admin login code=%d", code)
	}
	domainID := mustDefaultDomainID(t, a)
	sender := createTestMailbox(t, admin, domainID, "rule-forward-sender", "Rule Forward Sender", "Password123!", nil)
	recipient := createTestMailbox(t, admin, domainID, "netflix", "Netflix", "Password123!", nil)

	now := a.now().UTC().Format(time.RFC3339Nano)
	for _, email := range []string{"driver-a@example.test", "driver-b@example.test"} {
		if _, err := a.db.ExecContext(context.Background(), `INSERT INTO forwarding_verified_emails(id,user_id,email,verified,verified_at,delivery_status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`,
			newID("fwd"), recipient.UserID, email, 1, now, "verified", now, now); err != nil {
			t.Fatal(err)
		}
	}

	rcpt := &testClient{t: t, server: ts}
	if code := rcpt.do("POST", "/api/auth/login", map[string]string{"email": recipient.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("recipient login=%d", code)
	}
	var rule MailRule
	rulePayload := map[string]any{
		"mailboxId":  recipient.ID,
		"name":       "Netflix 验证码转发",
		"matchMode":  "all",
		"conditions": []map[string]string{{"field": "subject", "operator": "contains", "value": "Netflix"}},
		"actions":    []map[string]string{{"type": "forward", "value": "driver-a@example.test, driver-b@example.test"}},
	}
	if code := rcpt.do("POST", "/api/me/rules", rulePayload, &rule); code != http.StatusCreated {
		t.Fatalf("create forwarding rule code=%d rule=%+v", code, rule)
	}
	if len(rule.Actions) != 1 || rule.Actions[0].Type != "forward" || !strings.Contains(rule.Actions[0].Value, "driver-a@example.test") || !strings.Contains(rule.Actions[0].Value, "driver-b@example.test") {
		t.Fatalf("rule forwarding action not normalized: %+v", rule.Actions)
	}

	senderClient := &testClient{t: t, server: ts}
	if code := senderClient.do("POST", "/api/auth/login", map[string]string{"email": sender.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("sender login=%d", code)
	}
	var sent MailMessage
	if code := senderClient.do("POST", "/api/mail/send", map[string]any{
		"to":      []string{recipient.Address},
		"subject": "Netflix 登录验证码",
		"text":    "验证码 123456",
	}, &sent); code != http.StatusCreated {
		t.Fatalf("send code=%d sent=%+v", code, sent)
	}

	var recipientsJSON, mailFrom string
	if err := a.db.QueryRow(`SELECT recipients_json,mail_from FROM send_queue WHERE source=?`, sendSourceRuleForwarding).Scan(&recipientsJSON, &mailFrom); err != nil {
		t.Fatal(err)
	}
	if mailFrom != recipient.Address || !strings.Contains(recipientsJSON, "driver-a@example.test") || !strings.Contains(recipientsJSON, "driver-b@example.test") {
		t.Fatalf("rule forwarding mail_from=%q recipients=%s", mailFrom, recipientsJSON)
	}
}

func TestMailRulesExactSenderCustomFolderAndStopProcessing(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("admin login code=%d", code)
	}
	domainID := mustDefaultDomainID(t, a)
	sender := createTestMailbox(t, admin, domainID, "rule-exact-sender", "Sender With Name", "Password123!", nil)
	recipient := createTestMailbox(t, admin, domainID, "rule-custom-target", "Rule Target", "Password123!", nil)

	rcpt := &testClient{t: t, server: ts}
	if code := rcpt.do("POST", "/api/auth/login", map[string]string{"email": recipient.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("recipient login=%d", code)
	}
	var bad map[string]any
	if code := rcpt.do("POST", "/api/me/rules", map[string]any{
		"mailboxId":  recipient.ID,
		"conditions": []map[string]string{{"field": "size", "operator": "contains", "value": "10"}},
		"actions":    []map[string]string{{"type": "archive"}},
	}, &bad); code != http.StatusBadRequest {
		t.Fatalf("invalid field operator should be rejected code=%d body=%v", code, bad)
	}

	createRule := func(name string, action map[string]string, stop bool) {
		t.Helper()
		var rule MailRule
		if code := rcpt.do("POST", "/api/me/rules", map[string]any{
			"mailboxId":      recipient.ID,
			"name":           name,
			"conditions":     []map[string]string{{"field": "from", "operator": "equals", "value": sender.Address}},
			"actions":        []map[string]string{action},
			"stopProcessing": stop,
		}, &rule); code != http.StatusCreated {
			t.Fatalf("create rule %s code=%d rule=%+v", name, code, rule)
		}
	}
	createRule("fallback archive", map[string]string{"type": "archive"}, false)
	createRule("Netflix folder", map[string]string{"type": "move", "value": "Netflix 验证码"}, true)

	senderClient := &testClient{t: t, server: ts}
	if code := senderClient.do("POST", "/api/auth/login", map[string]string{"email": sender.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("sender login=%d", code)
	}
	var sent MailMessage
	if code := senderClient.do("POST", "/api/mail/send", map[string]any{"to": []string{recipient.Address}, "subject": "Netflix code", "text": "123456"}, &sent); code != http.StatusCreated {
		t.Fatalf("send code=%d sent=%+v", code, sent)
	}
	var custom struct {
		Items []MailMessage `json:"items"`
	}
	if code := rcpt.do("GET", "/api/mail/messages?mailboxId="+recipient.ID+"&folder="+url.QueryEscape("Netflix 验证码"), nil, &custom); code != http.StatusOK || len(custom.Items) != 1 {
		t.Fatalf("custom rule folder code=%d items=%+v", code, custom.Items)
	}
	var archived struct {
		Items []MailMessage `json:"items"`
	}
	if code := rcpt.do("GET", "/api/mail/messages?mailboxId="+recipient.ID+"&folder=Archive", nil, &archived); code != http.StatusOK || len(archived.Items) != 0 {
		t.Fatalf("stop processing should prevent fallback archive code=%d items=%+v", code, archived.Items)
	}
	var icon string
	if err := a.db.QueryRow(`SELECT icon FROM folders WHERE mailbox_id=? AND name=?`, recipient.ID, "Netflix 验证码").Scan(&icon); err != nil || icon != "netflix" {
		t.Fatalf("rule-created folder icon=%q err=%v", icon, err)
	}
}

func TestFolderIconForName(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		want      string
	}{
		{name: "Netflix 验证码", requested: "auto", want: "netflix"},
		{name: "ChatGPT 通知", want: "chatgpt"},
		{name: "OpenAI 账单", want: "chatgpt"},
		{name: "项目归档", want: "briefcase"},
		{name: "其他", want: "folder"},
		{name: "Netflix", requested: "heart", want: "heart"},
		{name: "Netflix", requested: "unknown", want: "folder"},
		{name: "Custom", requested: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=", want: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="},
		{name: "Egypt archive", want: "folder"},
		{name: "Custom", requested: "data:image/svg+xml;base64,PHN2Zz4=", want: "folder"},
		{name: "Custom", requested: "data:image/png;base64,SGVsbG8=", want: "folder"},
	}
	for _, tt := range tests {
		t.Run(tt.name+"/"+tt.requested, func(t *testing.T) {
			if got := folderIconForName(tt.name, tt.requested); got != tt.want {
				t.Fatalf("folderIconForName(%q, %q)=%q want %q", tt.name, tt.requested, got, tt.want)
			}
		})
	}
}

func TestRuleFolderAutoIconPreservesManualSelection(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	admin := &testClient{t: t, server: httptest.NewServer(a.Router())}
	defer admin.server.Close()

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("admin login code=%d", code)
	}
	domainID := createTestDomain(t, admin, "manual-icon.test")
	mailbox := createTestMailbox(t, admin, domainID.ID, "rules", "Rules", "Password123!", nil)
	if _, err := a.ensureCustomFolder(ctx, mailbox.ID, "Netflix", "heart"); err != nil {
		t.Fatalf("create custom folder: %v", err)
	}
	if _, err := a.ensureCustomFolder(ctx, mailbox.ID, "Netflix", "auto"); err != nil {
		t.Fatalf("reuse custom folder: %v", err)
	}
	var icon string
	if err := a.db.QueryRowContext(ctx, `SELECT icon FROM folders WHERE mailbox_id=? AND name='Netflix'`, mailbox.ID).Scan(&icon); err != nil || icon != "heart" {
		t.Fatalf("manual icon should be preserved icon=%q err=%v", icon, err)
	}
}

func TestMailRuleApplyExistingWhenDisabledExcludesSent(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("admin login code=%d", code)
	}
	domainID := mustDefaultDomainID(t, a)
	sender := createTestMailbox(t, admin, domainID, "rule-existing-sender", "Existing Sender", "Password123!", nil)
	recipient := createTestMailbox(t, admin, domainID, "rule-existing-recipient", "Existing Recipient", "Password123!", nil)
	subject := "same inbound and sent subject"

	senderClient := &testClient{t: t, server: ts}
	if code := senderClient.do("POST", "/api/auth/login", map[string]string{"email": sender.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("sender login=%d", code)
	}
	var incomingSend MailMessage
	if code := senderClient.do("POST", "/api/mail/send", map[string]any{"to": []string{recipient.Address}, "subject": subject, "text": "incoming"}, &incomingSend); code != http.StatusCreated {
		t.Fatalf("incoming send code=%d", code)
	}

	rcpt := &testClient{t: t, server: ts}
	if code := rcpt.do("POST", "/api/auth/login", map[string]string{"email": recipient.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("recipient login=%d", code)
	}
	var outgoing MailMessage
	if code := rcpt.do("POST", "/api/mail/send", map[string]any{"to": []string{sender.Address}, "subject": subject, "text": "outgoing"}, &outgoing); code != http.StatusCreated {
		t.Fatalf("outgoing send code=%d", code)
	}
	var rule MailRule
	if code := rcpt.do("POST", "/api/me/rules", map[string]any{
		"mailboxId":       recipient.ID,
		"name":            "existing disabled",
		"conditions":      []map[string]string{{"field": "subject", "operator": "equals", "value": subject}},
		"actions":         []map[string]string{{"type": "star"}},
		"applyToExisting": true,
		"enabled":         false,
	}, &rule); code != http.StatusCreated || rule.AppliedExistingCount != 1 || rule.Enabled {
		t.Fatalf("create disabled existing rule code=%d rule=%+v", code, rule)
	}
	var inboundStarred, sentStarred int
	if err := a.db.QueryRow(`SELECT is_starred FROM messages WHERE mailbox_id=? AND subject=? AND folder_id IN (SELECT id FROM folders WHERE mailbox_id=? AND lower(name)='inbox')`, recipient.ID, subject, recipient.ID).Scan(&inboundStarred); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow(`SELECT is_starred FROM messages WHERE id=?`, outgoing.ID).Scan(&sentStarred); err != nil {
		t.Fatal(err)
	}
	if inboundStarred != 1 || sentStarred != 0 {
		t.Fatalf("existing rule starred inbound=%d sent=%d", inboundStarred, sentStarred)
	}
}

func TestRuleAttachmentConditionUsesFilenameOnly(t *testing.T) {
	msg := ruleMessage{AttachmentNames: "notes.txt"}
	if ruleConditionMatches(MailRuleCondition{Field: "attachment", Operator: "contains", Value: "pdf"}, msg) {
		t.Fatal("attachment condition must not match MIME type or unrelated extension")
	}
	if !ruleConditionMatches(MailRuleCondition{Field: "attachment", Operator: "ends-with", Value: ".txt"}, msg) {
		t.Fatal("attachment condition should match filename")
	}
}

func TestMailRulesMailboxIsolation(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("admin login code=%d", code)
	}
	domainID := mustDefaultDomainID(t, a)
	owner := createTestMailbox(t, admin, domainID, "rule-owner", "Rule Owner", "Password123!", nil)
	other := createTestMailbox(t, admin, domainID, "rule-other", "Rule Other", "Password123!", nil)
	sender := createTestMailbox(t, admin, domainID, "rule-outsider", "Rule Outsider", "Password123!", nil)

	ownerClient := &testClient{t: t, server: ts}
	if code := ownerClient.do("POST", "/api/auth/login", map[string]string{"email": owner.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("owner login=%d", code)
	}
	var denied map[string]any
	if code := ownerClient.do("POST", "/api/me/rules", map[string]any{"mailboxId": other.ID, "fromContains": sender.Address, "action": "archive"}, &denied); code != http.StatusNotFound {
		t.Fatalf("cross-mailbox rule create code=%d body=%v", code, denied)
	}
	var rule MailRule
	if code := ownerClient.do("POST", "/api/me/rules", map[string]any{"mailboxId": owner.ID, "fromContains": sender.Address, "action": "archive"}, &rule); code != http.StatusCreated {
		t.Fatalf("create owner rule code=%d rule=%+v", code, rule)
	}

	senderClient := &testClient{t: t, server: ts}
	if code := senderClient.do("POST", "/api/auth/login", map[string]string{"email": sender.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("sender login=%d", code)
	}
	var sent MailMessage
	if code := senderClient.do("POST", "/api/mail/send", map[string]any{"to": []string{other.Address}, "subject": "isolation", "text": "body"}, &sent); code != http.StatusCreated {
		t.Fatalf("send to other code=%d sent=%+v", code, sent)
	}

	otherClient := &testClient{t: t, server: ts}
	if code := otherClient.do("POST", "/api/auth/login", map[string]string{"email": other.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("other login=%d", code)
	}
	var inbox struct {
		Items []MailMessage `json:"items"`
	}
	if code := otherClient.do("GET", "/api/mail/messages?mailboxId="+other.ID+"&folder=Inbox", nil, &inbox); code != http.StatusOK || len(inbox.Items) != 1 {
		t.Fatalf("other inbox code=%d items=%+v", code, inbox.Items)
	}
	var archived struct {
		Items []MailMessage `json:"items"`
	}
	if code := otherClient.do("GET", "/api/mail/messages?mailboxId="+other.ID+"&folder=Archive", nil, &archived); code != http.StatusOK || len(archived.Items) != 0 {
		t.Fatalf("other archive code=%d items=%+v", code, archived.Items)
	}
}

func TestMailRuleManagementActions(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	client := &testClient{t: t, server: ts}

	var login map[string]any
	if code := client.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d", code)
	}
	_, mailbox := defaultAdminUserAndMailbox(t, a)
	create := func(name, value string) MailRule {
		t.Helper()
		var rule MailRule
		if code := client.do("POST", "/api/me/rules", map[string]any{
			"mailboxId": mailbox.ID,
			"name":      name,
			"matchMode": "all",
			"conditions": []map[string]string{{
				"field": "subject", "operator": "contains", "value": value,
			}},
			"actions": []map[string]string{{"type": "archive"}},
			"enabled": true,
		}, &rule); code != http.StatusCreated {
			t.Fatalf("create %s code=%d rule=%+v", name, code, rule)
		}
		return rule
	}
	first := create("first", "欢迎使用 NewSzxcn 邮箱")
	second := create("second", "two")

	var updated MailRule
	if code := client.do("POST", "/api/me/rules/"+first.ID, map[string]any{"name": "first updated", "enabled": false}, &updated); code != http.StatusOK {
		t.Fatalf("update code=%d rule=%+v", code, updated)
	}
	if updated.Name != "first updated" || updated.Enabled {
		t.Fatalf("updated rule=%+v", updated)
	}

	var applied struct {
		OK       bool  `json:"ok"`
		Affected int64 `json:"affected"`
	}
	if code := client.do("POST", "/api/me/rules/"+first.ID+"/apply", nil, &applied); code != http.StatusOK || !applied.OK || applied.Affected != 1 {
		t.Fatalf("apply code=%d body=%+v", code, applied)
	}

	var moved map[string]any
	if code := client.do("POST", "/api/me/rules/"+second.ID+"/move", map[string]string{"direction": "down"}, &moved); code != http.StatusOK {
		t.Fatalf("move code=%d body=%+v", code, moved)
	}
	var listed struct {
		Items []MailRule `json:"items"`
	}
	if code := client.do("GET", "/api/me/rules", nil, &listed); code != http.StatusOK || len(listed.Items) != 2 {
		t.Fatalf("list code=%d items=%+v", code, listed.Items)
	}
	if listed.Items[0].ID != first.ID || listed.Items[1].ID != second.ID {
		t.Fatalf("unexpected order after move: %+v", listed.Items)
	}

	var missing map[string]any
	if code := client.do("POST", "/api/me/rules/missing/apply", nil, &missing); code != http.StatusNotFound {
		t.Fatalf("missing apply code=%d body=%+v", code, missing)
	}
}

func TestBlockedSenderMovesInboundToSpamAndIsolatesUsers(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("admin login code=%d", code)
	}
	var domains struct {
		Items []Domain `json:"items"`
	}
	if code := admin.do("GET", "/api/admin/domains", nil, &domains); code != http.StatusOK || len(domains.Items) == 0 {
		t.Fatalf("domains code=%d items=%+v", code, domains.Items)
	}
	sender := createTestMailbox(t, admin, domains.Items[0].ID, "blocked-sender", "Blocked Sender", "Password123!", nil)
	recipient := createTestMailbox(t, admin, domains.Items[0].ID, "blocked-recipient", "Blocked Recipient", "Password123!", nil)
	other := createTestMailbox(t, admin, domains.Items[0].ID, "blocked-other", "Blocked Other", "Password123!", nil)

	recipientClient := &testClient{t: t, server: ts}
	if code := recipientClient.do("POST", "/api/auth/login", map[string]string{"email": recipient.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("recipient login code=%d", code)
	}
	var blocked BlockedSender
	if code := recipientClient.do("POST", "/api/me/blocked-senders", map[string]any{"mailboxId": recipient.ID, "email": sender.Address, "reason": "test"}, &blocked); code != http.StatusCreated {
		t.Fatalf("blocked sender code=%d body=%+v", code, blocked)
	}

	senderClient := &testClient{t: t, server: ts}
	if code := senderClient.do("POST", "/api/auth/login", map[string]string{"email": sender.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("sender login code=%d", code)
	}
	var sent MailMessage
	if code := senderClient.do("POST", "/api/mail/send", map[string]any{"to": []string{recipient.Address}, "subject": "blocked sender test", "text": "body"}, &sent); code != http.StatusCreated {
		t.Fatalf("send code=%d sent=%+v", code, sent)
	}

	var inbox struct {
		Items []MailMessage `json:"items"`
	}
	if code := recipientClient.do("GET", "/api/mail/messages?mailboxId="+recipient.ID+"&folder=Inbox&q=blocked%20sender", nil, &inbox); code != http.StatusOK || len(inbox.Items) != 0 {
		t.Fatalf("recipient inbox code=%d items=%+v", code, inbox.Items)
	}
	var spam struct {
		Items []MailMessage `json:"items"`
	}
	if code := recipientClient.do("GET", "/api/mail/messages?mailboxId="+recipient.ID+"&folder=Spam&q=blocked%20sender", nil, &spam); code != http.StatusOK || len(spam.Items) != 1 {
		t.Fatalf("recipient spam code=%d items=%+v", code, spam.Items)
	}

	otherClient := &testClient{t: t, server: ts}
	if code := otherClient.do("POST", "/api/auth/login", map[string]string{"email": other.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("other login code=%d", code)
	}
	var denied map[string]any
	if code := otherClient.do("GET", "/api/mail/messages/"+spam.Items[0].ID+"?markRead=0", nil, &denied); code != http.StatusNotFound {
		t.Fatalf("other user should not read spam message code=%d body=%v", code, denied)
	}
}

func TestScheduleSendQueuesFutureMessage(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("admin login code=%d", code)
	}
	var domains struct {
		Items []Domain `json:"items"`
	}
	if code := admin.do("GET", "/api/admin/domains", nil, &domains); code != http.StatusOK || len(domains.Items) == 0 {
		t.Fatalf("domains code=%d items=%+v", code, domains.Items)
	}
	sender := createTestMailbox(t, admin, domains.Items[0].ID, "later", "Later", "Password123!", nil)
	recipient := createTestMailbox(t, admin, domains.Items[0].ID, "later-bob", "Later Bob", "Password123!", nil)

	alice := &testClient{t: t, server: ts}
	if code := alice.do("POST", "/api/auth/login", map[string]string{"email": sender.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("sender login code=%d", code)
	}
	var scheduled ScheduledSend
	payload := map[string]any{
		"mailboxId": sender.ID,
		"to":        []string{recipient.Address},
		"subject":   "send later",
		"text":      "not yet",
		"html":      "<p>not yet</p>",
		"sendAt":    time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339Nano),
	}
	if code := alice.do("POST", "/api/mail/schedule-send", payload, &scheduled); code != http.StatusCreated || scheduled.Status != "pending" {
		t.Fatalf("schedule code=%d scheduled=%+v", code, scheduled)
	}
	if scheduled.Subject != "send later" || len(scheduled.To) != 1 || scheduled.To[0] != recipient.Address || scheduled.Snippet != "not yet" {
		t.Fatalf("scheduled preview not populated: %+v", scheduled)
	}
	var scheduledList struct {
		Items []ScheduledSend `json:"items"`
	}
	if code := alice.do("GET", "/api/mail/scheduled-sends?mailboxId="+sender.ID, nil, &scheduledList); code != http.StatusOK || len(scheduledList.Items) != 1 || scheduledList.Items[0].ID != scheduled.ID {
		t.Fatalf("scheduled list code=%d items=%+v", code, scheduledList.Items)
	}
	if scheduledList.Items[0].Subject != "send later" || scheduledList.Items[0].Snippet != "not yet" {
		t.Fatalf("scheduled list preview not populated: %+v", scheduledList.Items[0])
	}

	bob := &testClient{t: t, server: ts}
	if code := bob.do("POST", "/api/auth/login", map[string]string{"email": recipient.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("recipient login code=%d", code)
	}
	var inbox struct {
		Items []MailMessage `json:"items"`
	}
	if code := bob.do("GET", "/api/mail/messages?folder=Inbox", nil, &inbox); code != http.StatusOK || len(inbox.Items) != 0 {
		t.Fatalf("future scheduled mail should not be delivered immediately: code=%d items=%+v", code, inbox.Items)
	}
	if code := alice.do("DELETE", "/api/mail/schedule-send/"+scheduled.ID, nil, &map[string]any{}); code != http.StatusOK {
		t.Fatalf("cancel scheduled send code=%d", code)
	}
	if code := alice.do("GET", "/api/mail/scheduled-sends?mailboxId="+sender.ID, nil, &scheduledList); code != http.StatusOK || len(scheduledList.Items) != 0 {
		t.Fatalf("scheduled list after cancel code=%d items=%+v", code, scheduledList.Items)
	}
}

func TestPermissionGroupMailLimits(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("admin login code=%d body=%v", code, login)
	}
	setRegularPermissionGroupForTest(t, a, regularUserDefaultPermissions(), PermissionLimits{MaxAttachmentMB: 1, MaxMailboxCount: 9, SMTPDailyLimit: 10, SMTPMinuteLimit: 1, IMAPMinuteLimit: 1, POP3MinuteLimit: 1})

	domainID := mustDefaultDomainID(t, a)
	sender := createTestMailbox(t, admin, domainID, "limited-sender", "Limited Sender", "Password123!", nil)
	recipient := createTestMailbox(t, admin, domainID, "limited-recipient", "Limited Recipient", "Password123!", nil)

	user := &testClient{t: t, server: ts}
	if code := user.do("POST", "/api/auth/login", map[string]string{"email": sender.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("user login code=%d", code)
	}
	var me struct {
		User User `json:"user"`
	}
	if code := user.do("GET", "/api/me", nil, &me); code != http.StatusOK {
		t.Fatalf("me code=%d user=%+v", code, me.User)
	}
	if me.User.Limits.MaxAttachmentMB != 1 || me.User.Limits.MaxMailboxCount != 9 || me.User.Limits.SMTPMinuteLimit != 1 || me.User.Limits.IMAPMinuteLimit != 1 || me.User.Limits.POP3MinuteLimit != 1 {
		t.Fatalf("user limits not attached: %+v", me.User.Limits)
	}

	tooLargeAttachment := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("x"), 1024*1024+1))
	var errBody map[string]any
	if code := user.do("POST", "/api/mail/send", map[string]any{
		"mailboxId": sender.ID,
		"to":        []string{recipient.Address},
		"subject":   "too large",
		"text":      "body",
		"html":      "<p>body</p>",
		"attachments": []map[string]string{{
			"filename":      "large.bin",
			"contentType":   "application/octet-stream",
			"contentBase64": tooLargeAttachment,
		}},
	}, &errBody); code != http.StatusBadRequest {
		t.Fatalf("oversized attachment should be rejected code=%d body=%v", code, errBody)
	}

	var sent MailMessage
	payload := map[string]any{
		"mailboxId": sender.ID,
		"to":        []string{recipient.Address},
		"subject":   "first limited send",
		"text":      "body",
		"html":      "<p>body</p>",
	}
	if code := user.do("POST", "/api/mail/send", payload, &sent); code != http.StatusCreated {
		t.Fatalf("first send code=%d msg=%+v", code, sent)
	}
	payload["subject"] = "second limited send"
	if code := user.do("POST", "/api/mail/send", payload, &errBody); code != http.StatusTooManyRequests {
		t.Fatalf("smtp minute limit should reject second send code=%d body=%v", code, errBody)
	}
}

func TestOpenRegistrationAtomicallyCreatesLoginUserAndMailbox(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	client := &testClient{t: t, server: ts}

	var out map[string]any
	if code := client.do("POST", "/api/auth/register", map[string]string{"email": "newuser@example.com", "displayName": "New User", "password": "Password123!"}, &out); code != http.StatusForbidden {
		t.Fatalf("closed registration code=%d body=%v", code, out)
	}

	a.updateConfig(func(cfg *Config) { cfg.OpenRegistration = true })
	domainID := mustDefaultDomainID(t, a)
	registration := map[string]string{
		"email":       "newuser@lanqin.local",
		"displayName": "New User",
		"password":    "Password123!",
		"domainId":    domainID,
		"localPart":   "newuser",
	}
	var registered struct {
		User User `json:"user"`
	}
	if code := client.do("POST", "/api/auth/register", registration, &registered); code != http.StatusCreated || registered.User.Email != "newuser@lanqin.local" || registered.User.Role != "user" {
		t.Fatalf("register code=%d user=%+v", code, registered.User)
	}
	var storageQuotaMB int
	if err := a.db.QueryRow(`SELECT storage_quota_mb FROM users WHERE id=?`, registered.User.ID).Scan(&storageQuotaMB); err != nil {
		t.Fatal(err)
	}
	if storageQuotaMB != defaultUserStorageQuotaMB {
		t.Fatalf("registered account storage quota=%d, want %d", storageQuotaMB, defaultUserStorageQuotaMB)
	}
	var me struct {
		User User `json:"user"`
	}
	if code := client.do("GET", "/api/me", nil, &me); code != http.StatusOK || me.User.Email != "newuser@lanqin.local" {
		t.Fatalf("me code=%d user=%+v", code, me.User)
	}
	var mine struct {
		Items []Mailbox `json:"items"`
	}
	if code := client.do("GET", "/api/mail/mailboxes", nil, &mine); code != http.StatusOK || len(mine.Items) != 1 || mine.Items[0].Address != "newuser@lanqin.local" {
		t.Fatalf("registered user should get auto-created mailbox: code=%d items=%+v", code, mine.Items)
	}

	another := &testClient{t: t, server: ts}
	if code := another.do("POST", "/api/auth/login", map[string]string{"email": "newuser@lanqin.local", "password": "Password123!"}, &out); code != http.StatusOK {
		t.Fatalf("login registered user code=%d body=%v", code, out)
	}
}

func TestTurnstileRetainedForLoginAndRegistration(t *testing.T) {
	a := newTestApp(t)
	verifyCalls := 0
	verifyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		verifyCalls++
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("secret") != "secret-key" || r.Form.Get("response") == "" {
			t.Fatalf("turnstile form secret=%q response=%q", r.Form.Get("secret"), r.Form.Get("response"))
		}
		respondJSON(w, http.StatusOK, map[string]any{"success": r.Form.Get("response") == "valid-token"})
	}))
	defer verifyServer.Close()
	a.turnstileURL = verifyServer.URL
	a.updateConfig(func(cfg *Config) {
		cfg.OpenRegistration = true
		cfg.TurnstileEnabled = true
		cfg.TurnstileSiteKey = "site-key"
		cfg.TurnstileSecretKey = "secret-key"
	})
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	client := &testClient{t: t, server: ts}

	var public PublicSettings
	if code := client.do("GET", "/api/public/settings", nil, &public); code != http.StatusOK || !public.TurnstileEnabled || public.TurnstileSiteKey != "site-key" {
		t.Fatalf("public turnstile settings code=%d settings=%+v", code, public)
	}
	if code := client.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, nil); code != http.StatusUnauthorized {
		t.Fatalf("login without turnstile code=%d", code)
	}
	var login map[string]any
	if code := client.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!", "turnstileToken": "valid-token"}, &login); code != http.StatusOK {
		t.Fatalf("login with turnstile code=%d body=%v", code, login)
	}
	domainID := mustDefaultDomainID(t, a)
	registerClient := &testClient{t: t, server: ts}
	registerPayload := map[string]string{"email": "turnstile-user@lanqin.local", "displayName": "Turnstile User", "password": "Password123!", "domainId": domainID, "localPart": "turnstile-user"}
	if code := registerClient.do("POST", "/api/auth/register", registerPayload, nil); code != http.StatusUnauthorized {
		t.Fatalf("register without turnstile code=%d", code)
	}
	registerPayload["turnstileToken"] = "valid-token"
	var registered map[string]any
	if code := registerClient.do("POST", "/api/auth/register", registerPayload, &registered); code != http.StatusCreated {
		t.Fatalf("register with turnstile code=%d body=%v", code, registered)
	}
	if verifyCalls != 2 {
		t.Fatalf("turnstile verifier calls=%d, want 2", verifyCalls)
	}
}

func TestLegacyBootstrapMailboxMigrationKeepsAdminMailbox(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Addr:              ":0",
		DBPath:            filepath.Join(dir, "lanqin.db"),
		DataDir:           filepath.Join(dir, "data"),
		CookieName:        "lanqin_test",
		SessionTTLHours:   24,
		AdminEmail:        "lanqinnet@gmail.com",
		AdminPassword:     "ChangeMe123!",
		PublicHostname:    "mail.example.test",
		PublicBaseURL:     "http://localhost:5173",
		AllowInsecureHTTP: true,
	}
	a, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })

	ctx := context.Background()

	// seed() now creates user + domain gmail.com + mailbox lanqinnet@gmail.com
	// with display_name = admin email (not "LanQin Admin").
	// Modify the mailbox to look like the old legacy pattern so the migration can find it.
	if _, err := a.db.ExecContext(ctx, `UPDATE mailboxes SET display_name='LanQin Admin' WHERE address=?`, cfg.AdminEmail); err != nil {
		t.Fatal(err)
	}

	// Get the domain ID for the verification step
	var domainID string
	if err := a.db.QueryRowContext(ctx, `SELECT id FROM domains WHERE name=?`, "gmail.com").Scan(&domainID); err != nil {
		t.Fatal(err)
	}

	if err := a.migrateLegacyBootstrapMailbox(ctx); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE email=? AND role='admin'`, cfg.AdminEmail).Scan(&count); err != nil || count != 1 {
		t.Fatalf("admin user count=%d err=%v", count, err)
	}
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mailboxes WHERE address=?`, cfg.AdminEmail).Scan(&count); err != nil || count != 1 {
		t.Fatalf("admin mailbox count=%d err=%v", count, err)
	}
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM domains WHERE id=?`, domainID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("admin domain count=%d err=%v", count, err)
	}
}

func TestConfiguredAdminEmailCreatesMailboxAndRejectsUsernameLogin(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Addr:              ":0",
		DBPath:            filepath.Join(dir, "lanqin.db"),
		DataDir:           filepath.Join(dir, "data"),
		CookieName:        "lanqin_test",
		SessionTTLHours:   24,
		AdminUsername:     "admin",
		AdminEmail:        "root@example.test",
		AdminPassword:     "ChangeMe123!",
		PublicHostname:    "mail.example.test",
		PublicBaseURL:     "http://localhost:5173",
		AllowInsecureHTTP: true,
	}
	a := newTestAppWithConfig(t, cfg)

	var domains, mailboxes int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM domains`).Scan(&domains); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM mailboxes`).Scan(&mailboxes); err != nil {
		t.Fatal(err)
	}
	if domains != 1 || mailboxes != 1 {
		t.Fatalf("admin email bootstrap domains=%d mailboxes=%d", domains, mailboxes)
	}
	var welcomeFrom, welcomeMessageID string
	if err := a.db.QueryRow(`SELECT from_addr,message_id FROM messages ORDER BY created_at LIMIT 1`).Scan(&welcomeFrom, &welcomeMessageID); err != nil {
		t.Fatal(err)
	}
	if welcomeFrom != "system@example.test" || !strings.HasSuffix(welcomeMessageID, "@example.test>") {
		t.Fatalf("welcome message retained placeholder domain: from=%q messageId=%q", welcomeFrom, welcomeMessageID)
	}

	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}
	var login struct {
		User User `json:"user"`
	}
	if code := admin.do("POST", "/api/auth/login", map[string]string{"loginName": "admin", "password": "ChangeMe123!"}, nil); code != http.StatusUnauthorized {
		t.Fatalf("legacy username login code=%d", code)
	}
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "root@example.test", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("admin email login code=%d", code)
	}
	if code := admin.do("POST", "/api/admin/users/"+login.User.ID, map[string]any{
		"email":       "root@example.test",
		"displayName": "Administrator",
		"role":        "admin",
		"disabled":    false,
	}, nil); code != http.StatusOK {
		t.Fatalf("admin display update code=%d", code)
	}
	if code := admin.do("POST", "/api/admin/users/"+login.User.ID, map[string]any{
		"email":       "not-an-email",
		"displayName": "Administrator",
		"role":        "admin",
		"disabled":    false,
	}, nil); code != http.StatusBadRequest {
		t.Fatalf("invalid primary email update code=%d", code)
	}
}

func TestAdministratorPrimaryEmailPersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Addr:              ":0",
		DBPath:            filepath.Join(dir, "lanqin.db"),
		DataDir:           filepath.Join(dir, "data"),
		CookieName:        "lanqin_test",
		SessionTTLHours:   24,
		AdminEmail:        "root@example.test",
		AdminPassword:     "ChangeMe123!",
		PublicHostname:    "mail.example.test",
		PublicBaseURL:     "http://localhost:5173",
		AllowInsecureHTTP: true,
	}
	a, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(a.Router())
	admin := &testClient{t: t, server: ts}
	var login struct {
		User User `json:"user"`
	}
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "root@example.test", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("admin login code=%d", code)
	}
	if code := admin.do("POST", "/api/admin/users/"+login.User.ID, map[string]any{
		"email":       "owner@example.test",
		"displayName": "Administrator",
		"role":        "admin",
		"disabled":    false,
	}, nil); code != http.StatusOK {
		t.Fatalf("admin email update code=%d", code)
	}
	if a.config().AdminEmail != "owner@example.test" {
		t.Fatalf("runtime admin email=%q", a.config().AdminEmail)
	}
	ts.Close()
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	var email, loginName string
	if err := restarted.db.QueryRow(`SELECT email,login_name FROM users WHERE role='admin'`).Scan(&email, &loginName); err != nil {
		t.Fatal(err)
	}
	if email != "owner@example.test" || loginName != "owner@example.test" || restarted.config().AdminEmail != "owner@example.test" {
		t.Fatalf("administrator identity reverted after restart: email=%q login=%q config=%q", email, loginName, restarted.config().AdminEmail)
	}
}

func TestLegacyAdminIdentityMigrationKeepsEarliestAdminAndRecordsResult(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	a.updateConfig(func(cfg *Config) {
		cfg.AdminUsername = "admin"
		cfg.AdminEmail = "admin@example.test"
	})
	keeperHash, err := bcrypt.GenerateFromPassword([]byte("OriginalPass123!"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	demotedHash, err := bcrypt.GenerateFromPassword([]byte("OtherPass123!"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.ExecContext(ctx, `DROP INDEX IF EXISTS idx_users_single_admin`); err != nil {
		t.Fatal(err)
	}
	now := a.now().UTC()
	if _, err := a.db.ExecContext(ctx, `UPDATE users SET login_name='admin', email='admin', password_hash=?, two_factor_secret='legacy-secret', two_factor_enabled=1, created_at=?, updated_at=? WHERE role='admin'`,
		string(keeperHash), now.Add(-2*time.Hour).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.ExecContext(ctx, `INSERT INTO users(id,login_name,email,display_name,role,password_hash,disabled,created_at,updated_at)
		VALUES('usr_second_admin','second','second@example.test','Second Admin','admin',?,0,?,?)`, string(demotedHash), now.Add(-time.Hour).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}

	if err := a.migrateConfiguredAdministratorIdentity(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.enforceSingleAdministratorIndex(ctx); err != nil {
		t.Fatal(err)
	}

	var adminCount int
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role='admin'`).Scan(&adminCount); err != nil || adminCount != 1 {
		t.Fatalf("admin count=%d err=%v", adminCount, err)
	}
	var email, loginName, passwordHash, twoFactorSecret string
	var enabled int
	if err := a.db.QueryRowContext(ctx, `SELECT email,login_name,password_hash,two_factor_secret,two_factor_enabled FROM users WHERE role='admin'`).Scan(&email, &loginName, &passwordHash, &twoFactorSecret, &enabled); err != nil {
		t.Fatal(err)
	}
	if email != "admin@example.test" || loginName != "admin@example.test" || twoFactorSecret != "legacy-secret" || enabled != 1 {
		t.Fatalf("admin identity not migrated safely email=%q login=%q secret=%q enabled=%d", email, loginName, twoFactorSecret, enabled)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte("OriginalPass123!")); err != nil {
		t.Fatalf("admin password hash was not preserved: %v", err)
	}
	var secondRole string
	if err := a.db.QueryRowContext(ctx, `SELECT role FROM users WHERE id='usr_second_admin'`).Scan(&secondRole); err != nil || secondRole != "user" {
		t.Fatalf("second admin role=%q err=%v", secondRole, err)
	}
	var mailboxCount int
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mailboxes WHERE address='admin@example.test'`).Scan(&mailboxCount); err != nil || mailboxCount != 1 {
		t.Fatalf("admin mailbox count=%d err=%v", mailboxCount, err)
	}
	var rawResult string
	if err := a.db.QueryRowContext(ctx, `SELECT value FROM system_settings WHERE key='adminIdentityMigrationResult'`).Scan(&rawResult); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rawResult, `"adminEmail":"admin@example.test"`) || !strings.Contains(rawResult, `"id":"usr_second_admin"`) {
		t.Fatalf("migration result not recorded: %s", rawResult)
	}
}

func TestLegacyWebUpdateResolvesAdminEmailFromExistingMailbox(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Addr:              ":0",
		DBPath:            filepath.Join(dir, "lanqin.db"),
		DataDir:           filepath.Join(dir, "data"),
		CookieName:        "lanqin_test",
		SessionTTLHours:   24,
		AdminEmail:        "bootstrap@lanqin.local",
		AdminPassword:     "ChangeMe123!",
		PublicHostname:    "mail.example.test",
		PublicBaseURL:     "http://localhost:5173",
		AllowInsecureHTTP: true,
	}
	a, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	stopTestWorkers(a)
	ctx := context.Background()
	var adminID, passwordHash string
	if err := a.db.QueryRowContext(ctx, `SELECT id,password_hash FROM users WHERE role='admin'`).Scan(&adminID, &passwordHash); err != nil {
		t.Fatal(err)
	}
	domainID, err := a.createDomainTx(ctx, nil, "example.test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.createMailboxWithPasswordHash(ctx, adminID, domainID, "admin", "admin@example.test", passwordHash, 1024, "active"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.ExecContext(ctx, `DELETE FROM mailboxes WHERE address='bootstrap@lanqin.local'`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.ExecContext(ctx, `UPDATE users SET login_name='admin', email='admin' WHERE id=?`, adminID); err != nil {
		t.Fatal(err)
	}
	if err := a.db.Close(); err != nil {
		t.Fatal(err)
	}

	// Old installations had only LANQIN_ADMIN_USERNAME. A webpage update starts
	// the new image directly, without running the interactive installer first.
	cfg.AdminUsername = "admin"
	cfg.AdminEmail = ""
	cfg.MailDomain = ""
	updated, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = updated.Close() })
	var email, loginName string
	if err := updated.db.QueryRowContext(ctx, `SELECT email,login_name FROM users WHERE role='admin'`).Scan(&email, &loginName); err != nil {
		t.Fatal(err)
	}
	if email != "admin@example.test" || loginName != "admin@example.test" {
		t.Fatalf("legacy administrator resolved incorrectly: email=%q login=%q", email, loginName)
	}
	if updated.config().AdminEmail != "admin@example.test" {
		t.Fatalf("runtime administrator email was not synchronized: %q", updated.config().AdminEmail)
	}
	var wrongDomainCount int
	if err := updated.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE email LIKE '%@lanqin.local'`).Scan(&wrongDomainCount); err != nil {
		t.Fatal(err)
	}
	if wrongDomainCount != 0 {
		t.Fatalf("web update created a lanqin.local administrator: %d", wrongDomainCount)
	}
	var migrationResult string
	if err := updated.db.QueryRowContext(ctx, `SELECT value FROM system_settings WHERE key='adminIdentityMigrationResult'`).Scan(&migrationResult); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(migrationResult, `"emailSource":"existing_admin_mailbox"`) {
		t.Fatalf("unexpected administrator email source: %s", migrationResult)
	}
}

func TestOnlyPrimaryEmailCanLoginSecondaryMailboxCannot(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("admin login code=%d body=%v", code, login)
	}
	domainID := mustDefaultDomainID(t, a)
	primary := createTestMailbox(t, admin, domainID, "primary-login", "Primary Login", "Password123!", nil)
	secondary := createTestMailbox(t, admin, domainID, "secondary-login", "Secondary Login", "MailboxOnly123!", map[string]any{"ownerEmail": primary.Address})

	user := &testClient{t: t, server: ts}
	if code := user.do("POST", "/api/auth/login", map[string]string{"email": primary.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("primary email login code=%d", code)
	}
	secondaryLogin := &testClient{t: t, server: ts}
	if code := secondaryLogin.do("POST", "/api/auth/login", map[string]string{"email": secondary.Address, "password": "MailboxOnly123!"}, nil); code != http.StatusUnauthorized {
		t.Fatalf("secondary mailbox should not login code=%d", code)
	}
}

func TestAdminUserAPICannotCreateOrPromoteAdministrator(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("admin login code=%d body=%v", code, login)
	}
	var errBody map[string]any
	if code := admin.do("POST", "/api/admin/users", map[string]any{
		"email":       "new-admin@lanqin.local",
		"displayName": "New Admin",
		"role":        "admin",
		"password":    "Password123!",
		"disabled":    false,
	}, &errBody); code != http.StatusForbidden {
		t.Fatalf("create admin code=%d body=%v", code, errBody)
	}
	var user AdminUser
	if code := admin.do("POST", "/api/admin/users", map[string]any{
		"email":       "regular@lanqin.local",
		"displayName": "Regular",
		"role":        "user",
		"password":    "Password123!",
		"disabled":    false,
	}, &user); code != http.StatusCreated {
		t.Fatalf("create user code=%d user=%+v", code, user)
	}
	if code := admin.do("POST", "/api/admin/users/"+user.ID, map[string]any{
		"email":       user.Email,
		"displayName": user.DisplayName,
		"role":        "admin",
		"disabled":    false,
	}, &errBody); code != http.StatusForbidden {
		t.Fatalf("promote admin code=%d body=%v", code, errBody)
	}
}

func TestAdminUsersListOrdersAdministratorThenAZPrimaryEmail(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("admin login code=%d body=%v", code, login)
	}
	for _, user := range []struct {
		email       string
		displayName string
	}{
		{"zeta@lanqin.local", "Zeta"},
		{"Alpha@lanqin.local", "Alpha"},
		{"bravo@lanqin.local", "Bravo"},
	} {
		var created AdminUser
		if code := admin.do("POST", "/api/admin/users", map[string]any{
			"email":       user.email,
			"displayName": user.displayName,
			"role":        "user",
			"password":    "Password123!",
			"disabled":    false,
		}, &created); code != http.StatusCreated {
			t.Fatalf("create user %s code=%d user=%+v", user.email, code, created)
		}
	}

	var users struct {
		Items []AdminUser `json:"items"`
	}
	if code := admin.do("GET", "/api/admin/users", nil, &users); code != http.StatusOK {
		t.Fatalf("list users code=%d users=%+v", code, users.Items)
	}
	if len(users.Items) < 4 {
		t.Fatalf("expected at least 4 users, got %+v", users.Items)
	}
	got := []string{users.Items[0].Email, users.Items[1].Email, users.Items[2].Email, users.Items[3].Email}
	want := []string{"admin@lanqin.local", "alpha@lanqin.local", "bravo@lanqin.local", "zeta@lanqin.local"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("users order=%v want prefix=%v", got, want)
		}
	}
}

func TestUserMailboxApplicationUsesAllowedDomainsAndReservedPrefixes(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("admin login code=%d body=%v", code, login)
	}
	allowedDomain := createTestDomain(t, admin, "a.com")
	blockedDomain := createTestDomain(t, admin, "b.com")

	var created AdminUser
	if code := admin.do("POST", "/api/admin/users", map[string]any{"email": "person@example.net", "displayName": "Person", "role": "user", "password": "Password123!", "disabled": false}, &created); code != http.StatusCreated {
		t.Fatalf("create user code=%d user=%+v", code, created)
	}

	userClient := &testClient{t: t, server: ts}
	if code := userClient.do("POST", "/api/auth/login", map[string]string{"email": "person@example.net", "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("user login code=%d", code)
	}
	var options MailboxApplyOptions
	if code := userClient.do("GET", "/api/me/mailbox-apply-options", nil, &options); code != http.StatusOK || options.Enabled || len(options.Domains) != 0 {
		t.Fatalf("disabled options code=%d options=%+v", code, options)
	}

	var settings SystemSettings
	if code := admin.do("GET", "/api/admin/settings", nil, &settings); code != http.StatusOK {
		t.Fatalf("get settings code=%d", code)
	}
	update := systemSettingsPayload(settings)
	update["userMailboxApplyEnabled"] = true
	update["userMailboxDomainIds"] = []string{allowedDomain.ID}
	update["reservedMailboxPrefixes"] = "admin\nroot"
	if code := admin.do("POST", "/api/admin/settings", update, &settings); code != http.StatusOK || !settings.UserMailboxApplyEnabled || len(settings.UserMailboxDomainIDs) != 1 {
		t.Fatalf("enable apply code=%d settings=%+v", code, settings)
	}

	if code := userClient.do("GET", "/api/me/mailbox-apply-options", nil, &options); code != http.StatusOK || !options.Enabled || len(options.Domains) != 1 || options.Domains[0].ID != allowedDomain.ID {
		t.Fatalf("enabled options code=%d options=%+v", code, options)
	}
	var errBody map[string]any
	if code := userClient.do("POST", "/api/me/mailboxes/apply", map[string]string{"domainId": allowedDomain.ID, "localPart": "admin"}, &errBody); code != http.StatusForbidden {
		t.Fatalf("reserved prefix code=%d body=%v", code, errBody)
	}
	if code := userClient.do("POST", "/api/me/mailboxes/apply", map[string]string{"domainId": blockedDomain.ID, "localPart": "alice"}, &errBody); code != http.StatusForbidden {
		t.Fatalf("blocked domain code=%d body=%v", code, errBody)
	}
	var mailbox Mailbox
	if code := userClient.do("POST", "/api/me/mailboxes/apply", map[string]string{"domainId": allowedDomain.ID, "localPart": "alice", "displayName": "Alice"}, &mailbox); code != http.StatusCreated || mailbox.Address != "alice@a.com" || mailbox.UserID != created.ID {
		t.Fatalf("apply mailbox code=%d mailbox=%+v", code, mailbox)
	}
	var mine struct {
		Items []Mailbox `json:"items"`
	}
	if code := userClient.do("GET", "/api/mail/mailboxes", nil, &mine); code != http.StatusOK || len(mine.Items) != 1 || mine.Items[0].Address != "alice@a.com" {
		t.Fatalf("mine code=%d items=%+v", code, mine.Items)
	}
	if code := userClient.do("POST", "/api/me/mailboxes/apply", map[string]string{"domainId": allowedDomain.ID, "localPart": "alice"}, &errBody); code != http.StatusConflict {
		t.Fatalf("duplicate apply code=%d body=%v", code, errBody)
	}
	limits := defaultPermissionLimits()
	limits.MaxMailboxCount = 1
	setRegularPermissionGroupForTest(t, a, regularUserDefaultPermissions(), limits)
	if code := userClient.do("POST", "/api/me/mailboxes/apply", map[string]string{"domainId": allowedDomain.ID, "localPart": "bob", "displayName": "Bob"}, &errBody); code != http.StatusForbidden {
		t.Fatalf("mailbox count limit code=%d body=%v", code, errBody)
	}
	var updated AdminUser
	if code := admin.do("POST", "/api/admin/users/"+created.ID, map[string]any{
		"displayName":          created.DisplayName,
		"role":                 "user",
		"disabled":             false,
		"mailboxLimitOverride": 2,
		"permissionGroupIds":   []string{},
	}, &updated); code != http.StatusOK {
		t.Fatalf("update user mailbox limit override code=%d user=%+v", code, updated)
	}
	if updated.MailboxLimitOverride == nil || *updated.MailboxLimitOverride != 2 || updated.Limits.MaxMailboxCount != 2 {
		t.Fatalf("user mailbox limit override not attached: %+v", updated.User)
	}
	if code := userClient.do("POST", "/api/me/mailboxes/apply", map[string]string{"domainId": allowedDomain.ID, "localPart": "bob", "displayName": "Bob"}, &mailbox); code != http.StatusCreated || mailbox.Address != "bob@a.com" {
		t.Fatalf("per-user mailbox limit apply code=%d mailbox=%+v", code, mailbox)
	}
	if code := userClient.do("POST", "/api/me/mailboxes/apply", map[string]string{"domainId": allowedDomain.ID, "localPart": "carol", "displayName": "Carol"}, &errBody); code != http.StatusForbidden {
		t.Fatalf("per-user mailbox limit code=%d body=%v", code, errBody)
	}
}

func TestUserCanSelectMultipleMailboxes(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d body=%v", code, login)
	}

	// seed() already created domain lanqin.local and mailbox admin@lanqin.local
	var domainList = struct {
		Items []Domain `json:"items"`
	}{}
	if code := admin.do("GET", "/api/admin/domains", nil, &domainList); code != http.StatusOK || len(domainList.Items) == 0 {
		t.Fatalf("list domains code=%d items=%+v", code, domainList.Items)
	}
	domainID := domainList.Items[0].ID

	primary := createTestMailbox(t, admin, domainID, "multi", "Multi", "Password123!", nil)
	secondary := createTestMailbox(t, admin, domainID, "multi-work", "Multi Work", "Password456!", map[string]any{"ownerEmail": primary.Address})
	if primary.UserID != secondary.UserID {
		t.Fatalf("mailboxes were not bound to one user: primary=%s secondary=%s", primary.UserID, secondary.UserID)
	}

	ctx := context.Background()
	now := a.now().UTC().Format(time.RFC3339Nano)
	primaryInboxID, err := a.ensureFolder(ctx, primary.ID, "Inbox")
	if err != nil {
		t.Fatal(err)
	}
	primaryArchiveID, err := a.ensureFolder(ctx, primary.ID, "Archive")
	if err != nil {
		t.Fatal(err)
	}
	secondaryInboxID, err := a.ensureFolder(ctx, secondary.ID, "Inbox")
	if err != nil {
		t.Fatal(err)
	}
	insertMessage := func(id, mailboxID, folderID, subject string, read int) {
		t.Helper()
		if _, err := a.db.ExecContext(ctx, `INSERT INTO messages(id,mailbox_id,folder_id,recipient_addr,message_uid,message_id,subject,from_addr,from_name,to_addrs,cc_addrs,bcc_addrs,sent_at,received_at,snippet,body_text,body_html,is_read,is_starred,has_attachments,size_bytes,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			id, mailboxID, folderID, "", id+"-uid", "<"+id+"@example.test>", subject, "sender@example.test", "", jsonEncode([]string{"recipient@example.test"}), "[]", "[]", now, now, subject, "", "", read, 0, 0, 0, now, now); err != nil {
			t.Fatal(err)
		}
	}
	insertMessage("msg_multi_primary_unread_1", primary.ID, primaryInboxID, "primary unread one", 0)
	insertMessage("msg_multi_primary_unread_2", primary.ID, primaryInboxID, "primary unread two", 0)
	insertMessage("msg_multi_primary_read", primary.ID, primaryInboxID, "primary read", 1)
	insertMessage("msg_multi_primary_archived", primary.ID, primaryArchiveID, "primary archived unread", 0)
	insertMessage("msg_multi_secondary_unread", secondary.ID, secondaryInboxID, "secondary unread", 0)
	var primaryImportantID, secondaryImportantID string
	if err := a.db.QueryRowContext(ctx, `SELECT id FROM mail_labels WHERE mailbox_id=? AND name='重要'`, primary.ID).Scan(&primaryImportantID); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRowContext(ctx, `SELECT id FROM mail_labels WHERE mailbox_id=? AND name='重要'`, secondary.ID).Scan(&secondaryImportantID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.ExecContext(ctx, `INSERT INTO message_labels(message_id,label_id,created_at) VALUES(?,?,?),(?,?,?)`, "msg_multi_primary_unread_1", primaryImportantID, now, "msg_multi_secondary_unread", secondaryImportantID, now); err != nil {
		t.Fatal(err)
	}

	userClient := &testClient{t: t, server: ts}
	if code := userClient.do("POST", "/api/auth/login", map[string]string{"email": primary.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("user login=%d", code)
	}
	var mine struct {
		Items []Mailbox `json:"items"`
	}
	if code := userClient.do("GET", "/api/mail/mailboxes", nil, &mine); code != http.StatusOK || len(mine.Items) != 2 {
		t.Fatalf("my mailboxes code=%d items=%d", code, len(mine.Items))
	}
	var allLabels struct {
		Items []MailLabel `json:"items"`
	}
	if code := userClient.do("GET", "/api/mail/labels?mailboxId=all", nil, &allLabels); code != http.StatusOK || len(allLabels.Items) != len(defaultMailLabelDefs()) {
		t.Fatalf("all labels code=%d items=%+v", code, allLabels.Items)
	}
	var allImportant MailLabel
	for _, label := range allLabels.Items {
		if label.Name == "重要" {
			allImportant = label
			break
		}
	}
	if allImportant.ID == "" || allImportant.MailboxID != "" || allImportant.MessageCount != 2 || allImportant.UnreadCount != 2 {
		t.Fatalf("aggregated important label=%+v", allImportant)
	}
	var importantMessages struct {
		Items []MailMessage `json:"items"`
	}
	if code := userClient.do("GET", "/api/mail/messages?mailboxId=all&labelId="+url.QueryEscape(allImportant.ID), nil, &importantMessages); code != http.StatusOK || len(importantMessages.Items) != 2 {
		t.Fatalf("all important messages code=%d items=%+v", code, importantMessages.Items)
	}
	unreadByAddress := map[string]int{}
	for _, item := range mine.Items {
		unreadByAddress[item.Address] = item.UnreadCount
	}
	if unreadByAddress[primary.Address] != 2 || unreadByAddress[secondary.Address] != 1 {
		t.Fatalf("mailbox unread counts=%+v, want %s=2 %s=1", unreadByAddress, primary.Address, secondary.Address)
	}
	if code := userClient.do("GET", "/api/mail/folders?mailboxId="+secondary.ID, nil, nil); code != http.StatusOK {
		t.Fatalf("folders for selected mailbox code=%d", code)
	}

	var sharedFolder MailFolder
	if code := userClient.do("POST", "/api/mail/folders?mailboxId=all", map[string]string{"name": "Shared Project"}, &sharedFolder); code != http.StatusCreated {
		t.Fatalf("create shared folder code=%d folder=%+v", code, sharedFolder)
	}
	renamePath := "/api/mail/folders/" + url.PathEscape(sharedFolder.ID) + "?mailboxId=all&folderName=" + url.QueryEscape(sharedFolder.Name)
	if code := userClient.do("PATCH", renamePath, map[string]string{"name": "Shared Archive"}, &sharedFolder); code != http.StatusOK || sharedFolder.Name != "Shared Archive" {
		t.Fatalf("rename shared folders code=%d folder=%+v", code, sharedFolder)
	}
	var primarySharedID, secondarySharedID string
	if err := a.db.QueryRowContext(ctx, `SELECT id FROM folders WHERE mailbox_id=? AND name=?`, primary.ID, "Shared Archive").Scan(&primarySharedID); err != nil {
		t.Fatalf("primary shared folder: %v", err)
	}
	if err := a.db.QueryRowContext(ctx, `SELECT id FROM folders WHERE mailbox_id=? AND name=?`, secondary.ID, "Shared Archive").Scan(&secondarySharedID); err != nil {
		t.Fatalf("secondary shared folder: %v", err)
	}
	if _, err := a.db.ExecContext(ctx, `UPDATE messages SET folder_id=? WHERE id=?`, primarySharedID, "msg_multi_primary_read"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.ExecContext(ctx, `UPDATE messages SET folder_id=? WHERE id=?`, secondarySharedID, "msg_multi_secondary_unread"); err != nil {
		t.Fatal(err)
	}
	var deleted struct {
		Moved int `json:"moved"`
	}
	deletePath := "/api/mail/folders/" + url.PathEscape(sharedFolder.ID) + "?mailboxId=all&folderName=" + url.QueryEscape(sharedFolder.Name)
	if code := userClient.do("DELETE", deletePath, nil, &deleted); code != http.StatusOK || deleted.Moved != 2 {
		t.Fatalf("delete shared folders code=%d moved=%d", code, deleted.Moved)
	}
	var sharedCount int
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM folders WHERE mailbox_id IN (?,?) AND name=?`, primary.ID, secondary.ID, "Shared Archive").Scan(&sharedCount); err != nil || sharedCount != 0 {
		t.Fatalf("shared folders remaining=%d err=%v", sharedCount, err)
	}
	var restoredPrimaryFolder, restoredSecondaryFolder string
	if err := a.db.QueryRowContext(ctx, `SELECT folder_id FROM messages WHERE id=?`, "msg_multi_primary_read").Scan(&restoredPrimaryFolder); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRowContext(ctx, `SELECT folder_id FROM messages WHERE id=?`, "msg_multi_secondary_unread").Scan(&restoredSecondaryFolder); err != nil {
		t.Fatal(err)
	}
	if restoredPrimaryFolder != primaryInboxID || restoredSecondaryFolder != secondaryInboxID {
		t.Fatalf("restored folders primary=%s secondary=%s", restoredPrimaryFolder, restoredSecondaryFolder)
	}

	var sent MailMessage
	payload := map[string]any{
		"mailboxId": secondary.ID,
		"to":        []string{"admin@lanqin.local"},
		"subject":   "selected mailbox sender",
		"text":      "hello from selected mailbox",
	}
	if code := userClient.do("POST", "/api/mail/send", payload, &sent); code != http.StatusCreated || sent.From != secondary.Address {
		t.Fatalf("send with selected mailbox code=%d from=%q want=%q", code, sent.From, secondary.Address)
	}
	var adminInbox struct {
		Items []MailMessage `json:"items"`
	}
	if code := admin.do("GET", "/api/mail/messages?folder=Inbox&q=selected%20mailbox%20sender", nil, &adminInbox); code != http.StatusOK || len(adminInbox.Items) != 1 || adminInbox.Items[0].From != secondary.Address {
		t.Fatalf("admin inbox code=%d items=%d first=%+v", code, len(adminInbox.Items), adminInbox.Items)
	}
}

func TestAllMailboxBulkMoveToCustomFolderKeepsMailboxIsolation(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("admin login code=%d body=%v", code, login)
	}

	var domainList struct {
		Items []Domain `json:"items"`
	}
	if code := admin.do("GET", "/api/admin/domains", nil, &domainList); code != http.StatusOK || len(domainList.Items) == 0 {
		t.Fatalf("list domains code=%d items=%+v", code, domainList.Items)
	}
	domainID := domainList.Items[0].ID
	primary := createTestMailbox(t, admin, domainID, "bulk-primary", "Bulk Primary", "Password123!", nil)
	secondary := createTestMailbox(t, admin, domainID, "bulk-secondary", "Bulk Secondary", "Password456!", map[string]any{"ownerEmail": primary.Address})
	otherUserMailbox := createTestMailbox(t, admin, domainID, "bulk-other", "Bulk Other", "Password789!", nil)
	if primary.UserID != secondary.UserID {
		t.Fatalf("primary and secondary should share owner: primary=%s secondary=%s", primary.UserID, secondary.UserID)
	}
	if primary.UserID == otherUserMailbox.UserID {
		t.Fatalf("other mailbox should belong to a different user")
	}

	ctx := context.Background()
	primaryInboxID, err := a.ensureFolder(ctx, primary.ID, "Inbox")
	if err != nil {
		t.Fatal(err)
	}
	secondaryInboxID, err := a.ensureFolder(ctx, secondary.ID, "Inbox")
	if err != nil {
		t.Fatal(err)
	}
	otherInboxID, err := a.ensureFolder(ctx, otherUserMailbox.ID, "Inbox")
	if err != nil {
		t.Fatal(err)
	}
	now := a.now().UTC().Format(time.RFC3339Nano)
	insertMessage := func(id, mailboxID, folderID, subject string) {
		t.Helper()
		if _, err := a.db.ExecContext(ctx, `INSERT INTO messages(id,mailbox_id,folder_id,recipient_addr,message_uid,message_id,subject,from_addr,from_name,to_addrs,cc_addrs,bcc_addrs,sent_at,received_at,snippet,body_text,body_html,is_read,is_starred,has_attachments,size_bytes,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			id, mailboxID, folderID, "", id+"-uid", "<"+id+"@example.test>", subject, "sender@example.test", "", jsonEncode([]string{"recipient@example.test"}), "[]", "[]", now, now, subject, "", "", 0, 0, 0, 0, now, now); err != nil {
			t.Fatal(err)
		}
	}
	insertMessage("msg_bulk_primary_move", primary.ID, primaryInboxID, "bulk move primary")
	insertMessage("msg_bulk_secondary_move", secondary.ID, secondaryInboxID, "bulk move secondary")
	insertMessage("msg_bulk_other_stays", otherUserMailbox.ID, otherInboxID, "bulk move other")

	userClient := &testClient{t: t, server: ts}
	if code := userClient.do("POST", "/api/auth/login", map[string]string{"email": primary.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("user login code=%d", code)
	}

	var allInbox struct {
		Items []MailMessage `json:"items"`
	}
	if code := userClient.do("GET", "/api/mail/messages?mailboxId=all&folder=Inbox&q=bulk%20move", nil, &allInbox); code != http.StatusOK || len(allInbox.Items) != 2 {
		t.Fatalf("all inbox code=%d items=%+v", code, allInbox.Items)
	}
	messageIDs := make([]string, 0, len(allInbox.Items)+1)
	for _, item := range allInbox.Items {
		messageIDs = append(messageIDs, item.ID)
	}
	messageIDs = append(messageIDs, "msg_bulk_other_stays")
	var moved struct {
		OK      bool `json:"ok"`
		Moved   int  `json:"moved"`
		Failed  int  `json:"failed"`
		Message string
		Items   []struct {
			ID        string `json:"id"`
			MailboxID string `json:"mailboxId"`
			OK        bool   `json:"ok"`
			Message   string `json:"message"`
		} `json:"items"`
	}
	if code := userClient.do("POST", "/api/mail/messages/bulk-move", map[string]any{"ids": messageIDs, "folder": "跨邮箱项目"}, &moved); code != http.StatusOK {
		t.Fatalf("bulk move code=%d body=%+v", code, moved)
	}
	if moved.OK || moved.Moved != 2 || moved.Failed != 1 || !strings.Contains(moved.Message, "已移动 2 封邮件，1 封失败") || len(moved.Items) != 3 {
		t.Fatalf("bulk move summary=%+v", moved)
	}

	var primaryTargetID, secondaryTargetID string
	if err := a.db.QueryRowContext(ctx, `SELECT id FROM folders WHERE mailbox_id=? AND name=?`, primary.ID, "跨邮箱项目").Scan(&primaryTargetID); err != nil {
		t.Fatalf("primary target folder: %v", err)
	}
	if err := a.db.QueryRowContext(ctx, `SELECT id FROM folders WHERE mailbox_id=? AND name=?`, secondary.ID, "跨邮箱项目").Scan(&secondaryTargetID); err != nil {
		t.Fatalf("secondary target folder: %v", err)
	}
	var primaryFolderID, secondaryFolderID, otherFolderID string
	if err := a.db.QueryRowContext(ctx, `SELECT folder_id FROM messages WHERE id=?`, "msg_bulk_primary_move").Scan(&primaryFolderID); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRowContext(ctx, `SELECT folder_id FROM messages WHERE id=?`, "msg_bulk_secondary_move").Scan(&secondaryFolderID); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRowContext(ctx, `SELECT folder_id FROM messages WHERE id=?`, "msg_bulk_other_stays").Scan(&otherFolderID); err != nil {
		t.Fatal(err)
	}
	if primaryFolderID != primaryTargetID || secondaryFolderID != secondaryTargetID {
		t.Fatalf("messages moved to wrong folders primary=%s want=%s secondary=%s want=%s", primaryFolderID, primaryTargetID, secondaryFolderID, secondaryTargetID)
	}
	if otherFolderID != otherInboxID {
		t.Fatalf("other user's message moved: folder=%s want=%s", otherFolderID, otherInboxID)
	}

	otherClient := &testClient{t: t, server: ts}
	if code := otherClient.do("POST", "/api/auth/login", map[string]string{"email": otherUserMailbox.Address, "password": "Password789!"}, &login); code != http.StatusOK {
		t.Fatalf("other login code=%d", code)
	}
	var forbidden struct {
		OK     bool `json:"ok"`
		Moved  int  `json:"moved"`
		Failed int  `json:"failed"`
		Items  []struct {
			ID      string `json:"id"`
			OK      bool   `json:"ok"`
			Message string `json:"message"`
		} `json:"items"`
	}
	if code := otherClient.do("POST", "/api/mail/messages/bulk-move", map[string]any{"ids": []string{"msg_bulk_primary_move"}, "folder": "Inbox"}, &forbidden); code != http.StatusOK || forbidden.OK || forbidden.Moved != 0 || forbidden.Failed != 1 || len(forbidden.Items) != 1 || forbidden.Items[0].Message != "邮件不存在或无权访问" {
		t.Fatalf("other user bulk move primary message code=%d body=%+v", code, forbidden)
	}
}

func TestCustomMailFoldersCreateAndMove(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d body=%v", code, login)
	}

	var bad map[string]any
	if code := admin.do("POST", "/api/mail/folders", map[string]string{"name": "Inbox"}, &bad); code != http.StatusBadRequest {
		t.Fatalf("system folder create should be rejected code=%d body=%v", code, bad)
	}
	if code := admin.do("POST", "/api/mail/folders", map[string]string{"name": "../bad"}, &bad); code != http.StatusBadRequest {
		t.Fatalf("invalid folder create should be rejected code=%d body=%v", code, bad)
	}

	var custom MailFolder
	if code := admin.do("POST", "/api/mail/folders", map[string]string{"name": "客户归档", "icon": "netflix"}, &custom); code != http.StatusCreated || custom.Name != "客户归档" || custom.Role != "客户归档" || custom.Icon != "netflix" {
		t.Fatalf("custom folder create code=%d folder=%+v", code, custom)
	}
	var folders struct {
		Items []MailFolder `json:"items"`
	}
	if code := admin.do("GET", "/api/mail/folders", nil, &folders); code != http.StatusOK || !folderListContains(folders.Items, "客户归档") {
		t.Fatalf("folder list code=%d items=%+v", code, folders.Items)
	}
	foundIcon := ""
	for _, folder := range folders.Items {
		if folder.Name == "客户归档" {
			foundIcon = folder.Icon
		}
	}
	if foundIcon != "netflix" {
		t.Fatalf("folder icon=%q, want netflix", foundIcon)
	}

	var sent MailMessage
	if code := admin.do("POST", "/api/mail/send", map[string]any{"to": []string{"person@example.test"}, "subject": "custom folder", "text": "body"}, &sent); code != http.StatusCreated {
		t.Fatalf("send code=%d msg=%+v", code, sent)
	}
	var ok map[string]any
	if code := admin.do("POST", "/api/mail/messages/"+sent.ID+"/move", map[string]string{"folder": "客户归档"}, &ok); code != http.StatusOK {
		t.Fatalf("move to custom folder code=%d body=%v", code, ok)
	}
	var list struct {
		Items []MailMessage `json:"items"`
	}
	if code := admin.do("GET", "/api/mail/messages?folder="+url.QueryEscape("客户归档"), nil, &list); code != http.StatusOK || len(list.Items) != 1 || list.Items[0].ID != sent.ID {
		t.Fatalf("custom folder messages code=%d items=%+v", code, list.Items)
	}
	if code := admin.do("POST", "/api/mail/messages/"+sent.ID+"/mark-read", map[string]bool{"read": false}, &ok); code != http.StatusOK {
		t.Fatalf("mark custom folder message unread code=%d body=%v", code, ok)
	}
	if code := admin.do("GET", "/api/mail/folders", nil, &folders); code != http.StatusOK {
		t.Fatalf("folder list after unread code=%d items=%+v", code, folders.Items)
	}
	customUnread := -1
	for _, folder := range folders.Items {
		if folder.Name == "客户归档" {
			customUnread = folder.UnreadCount
			break
		}
	}
	if customUnread != 1 {
		t.Fatalf("custom folder unread count=%d, want 1", customUnread)
	}
	if code := admin.do("PATCH", "/api/mail/folders/"+url.PathEscape(custom.ID), map[string]string{"name": "客户资料"}, &custom); code != http.StatusOK || custom.Name != "客户资料" || custom.Icon != "netflix" {
		t.Fatalf("rename custom folder code=%d folder=%+v", code, custom)
	}
	if code := admin.do("PATCH", "/api/mail/folders/"+url.PathEscape(custom.ID), map[string]string{"name": "Inbox"}, &bad); code != http.StatusBadRequest {
		t.Fatalf("rename custom folder to system name should be rejected code=%d body=%v", code, bad)
	}
	if code := admin.do("GET", "/api/mail/messages?folder="+url.QueryEscape("客户资料"), nil, &list); code != http.StatusOK || len(list.Items) != 1 || list.Items[0].ID != sent.ID {
		t.Fatalf("renamed custom folder messages code=%d items=%+v", code, list.Items)
	}
	if code := admin.do("POST", "/api/mail/messages/"+sent.ID+"/move", map[string]string{"folder": "bad/name"}, &bad); code != http.StatusBadRequest {
		t.Fatalf("invalid move folder should be rejected code=%d body=%v", code, bad)
	}
}

func TestCustomMailFoldersDeleteMovesMessagesToInbox(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d body=%v", code, login)
	}
	var custom MailFolder
	if code := admin.do("POST", "/api/mail/folders", map[string]string{"name": "临时项目"}, &custom); code != http.StatusCreated {
		t.Fatalf("create custom folder code=%d folder=%+v", code, custom)
	}
	var sent MailMessage
	if code := admin.do("POST", "/api/mail/send", map[string]any{"to": []string{"person@example.test"}, "subject": "delete folder keeps message", "text": "body"}, &sent); code != http.StatusCreated {
		t.Fatalf("send code=%d msg=%+v", code, sent)
	}
	var ok map[string]any
	if code := admin.do("POST", "/api/mail/messages/"+sent.ID+"/move", map[string]string{"folder": "临时项目"}, &ok); code != http.StatusOK {
		t.Fatalf("move to custom folder code=%d body=%v", code, ok)
	}
	if code := admin.do("DELETE", "/api/mail/folders/"+custom.ID, nil, &ok); code != http.StatusOK {
		t.Fatalf("delete custom folder code=%d body=%v", code, ok)
	}
	var folders struct {
		Items []MailFolder `json:"items"`
	}
	if code := admin.do("GET", "/api/mail/folders", nil, &folders); code != http.StatusOK || folderListContains(folders.Items, "临时项目") {
		t.Fatalf("folder should be deleted code=%d items=%+v", code, folders.Items)
	}
	var inbox struct {
		Items []MailMessage `json:"items"`
	}
	if code := admin.do("GET", "/api/mail/messages?folder=Inbox&q="+url.QueryEscape("delete folder keeps message"), nil, &inbox); code != http.StatusOK || len(inbox.Items) == 0 || inbox.Items[0].ID != sent.ID {
		t.Fatalf("message should be moved to inbox code=%d items=%+v", code, inbox.Items)
	}
	var bad map[string]any
	var inboxID string
	for _, item := range folders.Items {
		if item.Name == "Inbox" {
			inboxID = item.ID
			break
		}
	}
	if inboxID == "" {
		t.Fatalf("inbox id not found")
	}
	if code := admin.do("DELETE", "/api/mail/folders/"+inboxID, nil, &bad); code != http.StatusBadRequest {
		t.Fatalf("delete system folder should be rejected code=%d body=%v", code, bad)
	}
}

func TestCustomMailFoldersReorder(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d body=%v", code, login)
	}

	createFolder := func(name string) MailFolder {
		t.Helper()
		var folder MailFolder
		if code := admin.do("POST", "/api/mail/folders", map[string]string{"name": name}, &folder); code != http.StatusCreated {
			t.Fatalf("create folder %s code=%d folder=%+v", name, code, folder)
		}
		return folder
	}
	customer := createFolder("客户")
	bills := createFolder("账单")
	project := createFolder("项目")

	var ok map[string]any
	if code := admin.do("POST", "/api/mail/folders/reorder", map[string]any{"folderIds": []string{project.ID, customer.ID, bills.ID}}, &ok); code != http.StatusOK {
		t.Fatalf("reorder code=%d body=%v", code, ok)
	}
	if code := admin.do("POST", "/api/mail/folders/reorder", map[string]any{"folders": []map[string]any{
		{"id": customer.ID, "sortOrder": 500},
		{"id": project.ID, "sortOrder": 2500},
		{"id": bills.ID, "sortOrder": 3500},
	}}, &ok); code != http.StatusOK {
		t.Fatalf("reorder with explicit sort order code=%d body=%v", code, ok)
	}
	var folders struct {
		Items []MailFolder `json:"items"`
	}
	if code := admin.do("GET", "/api/mail/folders", nil, &folders); code != http.StatusOK {
		t.Fatalf("list folders code=%d items=%+v", code, folders.Items)
	}
	got := customFolderNames(folders.Items)
	want := []string{"客户", "项目", "账单"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("custom folder order=%v want=%v", got, want)
	}
	if folders.Items[0].ID != customer.ID {
		t.Fatalf("customer folder should be before inbox after explicit sort order, first=%+v", folders.Items[0])
	}
	var inboxID string
	for _, item := range folders.Items {
		if item.Name == "Inbox" {
			inboxID = item.ID
			break
		}
	}
	if inboxID == "" {
		t.Fatalf("inbox not found in folders: %+v", folders.Items)
	}
	var bad map[string]any
	if code := admin.do("POST", "/api/mail/folders/reorder", map[string]any{"folderIds": []string{project.ID, inboxID, customer.ID, bills.ID}}, &bad); code != http.StatusBadRequest {
		t.Fatalf("reorder with system folder should be rejected code=%d body=%v", code, bad)
	}

	domain := createTestDomain(t, admin, "folders.test")
	other := createTestMailbox(t, admin, domain.ID, "other", "Other", "Password123!", nil)
	otherFolderID, err := a.ensureFolder(context.Background(), other.ID, "其他")
	if err != nil {
		t.Fatal(err)
	}
	if code := admin.do("POST", "/api/mail/folders/reorder", map[string]any{"folderIds": []string{project.ID, customer.ID, otherFolderID}}, &bad); code != http.StatusBadRequest {
		t.Fatalf("reorder with other mailbox folder should be rejected code=%d body=%v", code, bad)
	}
}

func TestCatchAllStoresUnregisteredMailForAdminOnly(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d body=%v", code, login)
	}
	// seed() already created domain lanqin.local and mailbox admin@lanqin.local
	var domainList = struct {
		Items []Domain `json:"items"`
	}{}
	if code := admin.do("GET", "/api/admin/domains", nil, &domainList); code != http.StatusOK || len(domainList.Items) == 0 {
		t.Fatalf("list domains code=%d items=%+v", code, domainList.Items)
	}
	payload := map[string]any{
		"to":      []string{"ghost@lanqin.local"},
		"subject": "should be rejected by default",
		"text":    "default disabled",
	}
	var sent MailMessage
	if code := admin.do("POST", "/api/mail/send", payload, &sent); code != http.StatusCreated {
		t.Fatalf("send disabled catch-all code=%d", code)
	}
	var list struct {
		Items []MailMessage `json:"items"`
	}
	if code := admin.do("GET", "/api/admin/messages?mailboxId=unregistered&q=should%20be%20rejected", nil, &list); code != http.StatusOK || len(list.Items) != 0 {
		t.Fatalf("disabled catch-all should not store unregistered mail: code=%d items=%+v", code, list.Items)
	}

	var settings SystemSettings
	if code := admin.do("GET", "/api/admin/settings", nil, &settings); code != http.StatusOK {
		t.Fatalf("get settings code=%d", code)
	}
	update := systemSettingsPayload(settings)
	update["catchAllEnabled"] = true
	if code := admin.do("POST", "/api/admin/settings", update, &settings); code != http.StatusOK || !settings.CatchAllEnabled {
		t.Fatalf("enable catch-all code=%d settings=%+v", code, settings)
	}

	payload = map[string]any{
		"to":      []string{"ghost@lanqin.local"},
		"subject": "stored for admin only",
		"text":    "unregistered mailbox content",
	}
	if code := admin.do("POST", "/api/mail/send", payload, &sent); code != http.StatusCreated {
		t.Fatalf("send enabled catch-all code=%d", code)
	}
	if code := admin.do("GET", "/api/admin/messages?mailboxId=unregistered&q=stored%20for%20admin", nil, &list); code != http.StatusOK || len(list.Items) != 1 {
		t.Fatalf("enabled catch-all admin list code=%d items=%+v", code, list.Items)
	}
	if got := list.Items[0].RecipientAddr; got != "ghost@lanqin.local" {
		t.Fatalf("recipientAddress=%q", got)
	}
	unregisteredMessageID := list.Items[0].ID

	var auditGroup PermissionGroup
	if code := admin.do("POST", "/api/admin/permission-groups", map[string]any{
		"name":        "Catch-all Message Auditors",
		"description": "Test group for registered-message audit access",
		"permissions": []string{PermissionMessagesView, PermissionMessagesRead, PermissionMessagesAttachment},
	}, &auditGroup); code != http.StatusCreated {
		t.Fatalf("create message audit group code=%d group=%+v", code, auditGroup)
	}
	var auditor AdminUser
	if code := admin.do("POST", "/api/admin/users", map[string]any{
		"email":              "message-auditor@lanqin.local",
		"displayName":        "Message Auditor",
		"role":               "user",
		"password":           "Password123!",
		"disabled":           false,
		"permissionGroupIds": []string{auditGroup.ID},
	}, &auditor); code != http.StatusCreated {
		t.Fatalf("create message auditor code=%d user=%+v", code, auditor)
	}
	auditorClient := &testClient{t: t, server: ts}
	if code := auditorClient.do("POST", "/api/auth/login", map[string]string{"email": "message-auditor@lanqin.local", "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("auditor login code=%d body=%v", code, login)
	}
	var errBody map[string]any
	if code := auditorClient.do("GET", "/api/admin/messages?mailboxId=unregistered", nil, &errBody); code != http.StatusForbidden {
		t.Fatalf("message auditor unregistered list code=%d body=%v", code, errBody)
	}
	list.Items = nil
	if code := auditorClient.do("GET", "/api/admin/messages?q=stored%20for%20admin", nil, &list); code != http.StatusOK {
		t.Fatalf("message auditor all-mail query code=%d items=%+v", code, list.Items)
	}
	for _, item := range list.Items {
		if item.MailboxID == "" {
			t.Fatalf("message auditor all-mail query exposed unregistered mail: %+v", item)
		}
	}
	if code := auditorClient.do("GET", "/api/admin/messages/"+unregisteredMessageID, nil, &errBody); code != http.StatusForbidden {
		t.Fatalf("message auditor unregistered detail code=%d body=%v", code, errBody)
	}
}

func TestHTMLPolicyPreservesEmailLayoutStyles(t *testing.T) {
	policy := NewHTMLPolicy()
	out := policy.Sanitize(`<html><head><style type="text/css">.card{max-width:600px;margin:0 auto;background:linear-gradient(135deg,#667eea,#764ba2);box-shadow:0 8px 24px rgba(0,0,0,.12);color:#fff}.content{text-align:center;padding:24px}</style></head><body>
		<div class="card" style="max-width:600px;margin:0 auto;background:linear-gradient(135deg,#667eea,#764ba2);box-shadow:0 8px 24px rgba(0,0,0,.12);color:#fff" onclick="alert(1)">
		<table width="100%" cellpadding="0" cellspacing="0" style="border-collapse:collapse"><tr><td align="center" style="padding:24px;text-align:center;background-color:#f8fafc">
		<a href="javascript:alert(1)">bad</a><img src="x" onerror="alert(1)"><script>alert(1)</script>hello
		</td></tr></table>
	</div></body></html>`)
	for _, want := range []string{"<style type=\"text/css\">", ".card{", "class=\"card\"", "max-width: 600px", "margin: 0 auto", "background: linear-gradient", "box-shadow:", "cellpadding=\"0\"", "cellspacing=\"0\"", "align=\"center\"", "text-align: center"} {
		if !strings.Contains(out, want) {
			t.Fatalf("sanitized html missing %q: %s", want, out)
		}
	}
	blockedOut := policy.Sanitize(`<style>.bad{background:url(https://tracker.example/x);color:red}</style><p>ok</p>`)
	if strings.Contains(blockedOut, "<style") {
		t.Fatalf("unsafe css block should be removed: %s", blockedOut)
	}
	for _, blocked := range []string{"onclick", "onerror", "javascript:", "<script"} {
		if strings.Contains(strings.ToLower(out), blocked) {
			t.Fatalf("sanitized html kept unsafe %q: %s", blocked, out)
		}
	}
}

func TestMailSendQueuesSMTPFailureForRetry(t *testing.T) {
	a := newTestApp(t)
	a.updateConfig(func(cfg *Config) { cfg.SMTPHost = "127.0.0.1" })
	a.updateConfig(func(cfg *Config) { cfg.SMTPPort = "1" })
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d body=%v", code, login)
	}
	payload := map[string]any{
		"to":      []string{"person@example.com"},
		"subject": "smtp failure should surface",
		"text":    "hello",
	}
	var sent MailMessage
	if code := admin.do("POST", "/api/mail/send", payload, &sent); code != http.StatusCreated {
		t.Fatalf("smtp queued send code=%d body=%+v", code, sent)
	}
	if err := a.processDueSendQueue(context.Background()); err != nil {
		t.Fatal(err)
	}
	var status, lastError string
	if err := a.db.QueryRow(`SELECT status,last_error FROM send_queue WHERE sent_message_id=?`, sent.ID).Scan(&status, &lastError); err != nil {
		t.Fatal(err)
	}
	if status != sendQueueStatusFailed || lastError == "" {
		t.Fatalf("queue status=%q lastError=%q", status, lastError)
	}
	var auditCount int
	if err := a.db.QueryRow(`SELECT COUNT(1) FROM send_audit_events WHERE sent_message_id=? AND event=?`, sent.ID, sendAuditRetry).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("retry audit count=%d, want 1", auditCount)
	}
}

func TestForwardingVerificationPageDoesNotLinkToMailbox(t *testing.T) {
	a := newTestApp(t)
	recorder := httptest.NewRecorder()

	a.renderForwardingVerificationPage(recorder, http.StatusOK, true, "friend@example.test", "该邮箱已通过转发验证")
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d", recorder.Code)
	}
	for _, forbidden := range []string{`href="/"`, "返回邮箱", "登录"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("verification page contains forbidden navigation %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(body, "可以关闭此页面") {
		t.Fatalf("verification page is missing close guidance: %s", body)
	}
}

func TestInboundForwardingSettingsAndDelivery(t *testing.T) {
	a := newTestApp(t)
	stopTestWorkers(a)
	host, port, received := startCapturingSMTP(t, 8)
	a.updateConfig(func(cfg *Config) { cfg.SMTPHost = host })
	a.updateConfig(func(cfg *Config) { cfg.SMTPPort = port })
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d body=%v", code, login)
	}
	_, mb := defaultAdminUserAndMailbox(t, a)
	ctx := context.Background()
	verifyTarget := func(email string) {
		t.Helper()
		var settings ForwardingSettings
		if code := admin.do("POST", "/api/me/forwarding/verified-emails", map[string]string{"email": email}, &settings); code != http.StatusCreated {
			t.Fatalf("add forwarding target %s code=%d settings=%+v", email, code, settings)
		}
		if len(settings.VerifiedEmails) == 0 || settings.VerifiedEmails[0].Verified {
			t.Fatalf("target should start pending: %+v", settings.VerifiedEmails)
		}
		if err := a.processDueSendQueue(ctx); err != nil {
			t.Fatal(err)
		}
		var verificationBody string
		select {
		case verificationBody = <-received:
		case <-time.After(2 * time.Second):
			t.Fatal("verification email was not relayed")
		}
		token := extractForwardingVerificationToken(t, verificationBody)
		if code := admin.do("GET", "/api/verify-email?token="+url.QueryEscape(token), nil, nil); code != http.StatusOK {
			t.Fatalf("verify email code=%d", code)
		}
		if code := admin.do("GET", "/api/verify-email?token="+url.QueryEscape(token), nil, nil); code != http.StatusOK {
			t.Fatalf("reopen verified email link code=%d", code)
		}
		if code := admin.do("GET", "/api/me/forwarding", nil, &settings); code != http.StatusOK {
			t.Fatalf("reload forwarding settings code=%d", code)
		}
		found := false
		for _, item := range settings.VerifiedEmails {
			if item.Email == email {
				found = item.Verified
			}
		}
		if !found {
			t.Fatalf("target %s was not marked verified: %+v", email, settings.VerifiedEmails)
		}
	}

	var settings ForwardingSettings
	if code := admin.do("POST", "/api/me/forwarding/verified-emails", map[string]string{"email": "account-forward@example.test"}, &settings); code != http.StatusCreated {
		t.Fatalf("add account forwarding target code=%d settings=%+v", code, settings)
	}
	if code := admin.do("POST", "/api/me/forwarding/account", map[string]string{"targetEmail": "account-forward@example.test"}, &settings); code != http.StatusBadRequest {
		t.Fatalf("unverified account forwarding should be rejected, code=%d settings=%+v", code, settings)
	}
	if err := a.processDueSendQueue(ctx); err != nil {
		t.Fatal(err)
	}
	var verificationBody string
	select {
	case verificationBody = <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("account verification email was not relayed")
	}
	token := extractForwardingVerificationToken(t, verificationBody)
	if code := admin.do("GET", "/api/verify-email?token="+url.QueryEscape(token), nil, nil); code != http.StatusOK {
		t.Fatalf("verify account target code=%d", code)
	}
	if code := admin.do("GET", "/api/verify-email?token="+url.QueryEscape(token), nil, nil); code != http.StatusOK {
		t.Fatalf("reopen account verification link code=%d", code)
	}
	verifyTarget("account-forward-two@example.test")
	if code := admin.do("POST", "/api/me/forwarding/account", map[string]any{"targetEmails": []string{"account-forward@example.test", "account-forward-two@example.test"}}, &settings); code != http.StatusOK || settings.AccountTargetEmail != "account-forward@example.test" || len(settings.AccountTargetEmails) != 2 {
		t.Fatalf("save account forwarding code=%d settings=%+v", code, settings)
	}

	inboxID, err := a.ensureFolder(ctx, mb.ID, "Inbox")
	if err != nil {
		t.Fatal(err)
	}
	insertInbound := func(messageID, subject string, raw []byte) string {
		t.Helper()
		now := a.now().UTC()
		id, err := a.insertMessage(ctx, storedMessage{
			MailboxID:     mb.ID,
			FolderID:      inboxID,
			MessageUID:    newID("uid"),
			MessageID:     messageID,
			Subject:       subject,
			From:          "sender@example.test",
			To:            []string{mb.Address},
			SentAt:        now,
			ReceivedAt:    now,
			Snippet:       "body",
			BodyText:      "body",
			IsRead:        false,
			RecipientAddr: mb.Address,
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		a.processInboundForwarding(ctx, id, mb.ID, raw)
		return id
	}

	raw := []byte("From: sender@example.test\r\nTo: admin@lanqin.local\r\nSubject: account forward\r\nMessage-ID: <account-forward@example.test>\r\n\r\nbody")
	firstID := insertInbound("<account-forward@example.test>", "account forward", raw)
	var recipientsJSON string
	if err := a.db.QueryRow(`SELECT recipients_json FROM send_queue WHERE source=? AND sent_message_id=?`, sendSourceForwarding, firstID).Scan(&recipientsJSON); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(recipientsJSON, "account-forward@example.test") || !strings.Contains(recipientsJSON, "account-forward-two@example.test") {
		t.Fatalf("account forwarding recipients=%s", recipientsJSON)
	}
	if err := a.processDueSendQueue(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case body := <-received:
		if !strings.Contains(body, forwardingHeaderName+": mail.example.test") || !strings.Contains(body, "X-LanQin-Forwarded-For: admin@lanqin.local") || strings.Contains(body, "\r\n\r\n\r\nbody") {
			t.Fatalf("unexpected forwarded body: %q", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("account forwarding mail was not relayed")
	}

	verifyTarget("mailbox-forward@example.test")
	verifyTarget("mailbox-forward-two@example.test")
	if code := admin.do("POST", "/api/me/mailboxes/"+mb.ID+"/forwarding", map[string]any{"targetEmails": []string{"mailbox-forward@example.test", "mailbox-forward-two@example.test"}}, &settings); code != http.StatusOK {
		t.Fatalf("save mailbox forwarding code=%d settings=%+v", code, settings)
	}
	raw = []byte("From: sender@example.test\r\nTo: admin@lanqin.local\r\nSubject: mailbox forward\r\nMessage-ID: <mailbox-forward@example.test>\r\n\r\nbody")
	secondID := insertInbound("<mailbox-forward@example.test>", "mailbox forward", raw)
	if err := a.db.QueryRow(`SELECT recipients_json FROM send_queue WHERE source=? AND sent_message_id=?`, sendSourceForwarding, secondID).Scan(&recipientsJSON); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(recipientsJSON, "account-forward@example.test") || !strings.Contains(recipientsJSON, "account-forward-two@example.test") || !strings.Contains(recipientsJSON, "mailbox-forward@example.test") || !strings.Contains(recipientsJSON, "mailbox-forward-two@example.test") {
		t.Fatalf("mailbox forwarding should include account and mailbox targets, recipients=%s", recipientsJSON)
	}

	if code := admin.do("POST", "/api/me/forwarding/account", map[string]any{"targetEmails": []string{"account-forward-two@example.test"}}, &settings); code != http.StatusOK {
		t.Fatalf("update account forwarding after mailbox forwarding code=%d settings=%+v", code, settings)
	}
	raw = []byte("From: sender@example.test\r\nTo: admin@lanqin.local\r\nSubject: account changed\r\nMessage-ID: <account-changed@example.test>\r\n\r\nbody")
	thirdID := insertInbound("<account-changed@example.test>", "account changed", raw)
	if err := a.db.QueryRow(`SELECT recipients_json FROM send_queue WHERE source=? AND sent_message_id=?`, sendSourceForwarding, thirdID).Scan(&recipientsJSON); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(recipientsJSON, "account-forward@example.test") || !strings.Contains(recipientsJSON, "account-forward-two@example.test") || !strings.Contains(recipientsJSON, "mailbox-forward@example.test") || !strings.Contains(recipientsJSON, "mailbox-forward-two@example.test") {
		t.Fatalf("changing account forwarding should preserve mailbox targets, recipients=%s", recipientsJSON)
	}

	loopRaw := []byte("From: sender@example.test\r\nTo: admin@lanqin.local\r\nSubject: loop\r\n" + forwardingHeaderName + ": mail.example.test\r\nMessage-ID: <forward-loop@example.test>\r\n\r\nbody")
	insertInbound("<forward-loop@example.test>", "loop", loopRaw)
	var queueCount int
	if err := a.db.QueryRow(`SELECT COUNT(1) FROM send_queue WHERE source=?`, sendSourceForwarding).Scan(&queueCount); err != nil {
		t.Fatal(err)
	}
	if queueCount != 3 {
		t.Fatalf("forwarding queue count=%d, want 3", queueCount)
	}
}

func TestMailSendRejectsUnauthorizedFrom(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d body=%v", code, login)
	}
	var errBody map[string]any
	if code := admin.do("POST", "/api/mail/send", map[string]any{
		"from":    "attacker@example.com",
		"to":      []string{"person@example.com"},
		"subject": "bad from",
		"text":    "hello",
	}, &errBody); code != http.StatusForbidden {
		t.Fatalf("unauthorized from code=%d body=%v", code, errBody)
	}
}

func TestMailSendRollsBackSentCopyWhenQueueInsertFails(t *testing.T) {
	a := newTestApp(t)
	a.updateConfig(func(cfg *Config) { cfg.SMTPHost = "postfix" })
	a.updateConfig(func(cfg *Config) { cfg.SMTPPort = "25" })
	user, mb := defaultAdminUserAndMailbox(t, a)
	if _, err := a.db.ExecContext(context.Background(), `DROP TABLE send_queue`); err != nil {
		t.Fatal(err)
	}
	_, err := a.sendMailNow(context.Background(), user, mb, mailComposeInput{
		To:      []string{"person@example.com"},
		Subject: "queue insert failure",
		Text:    "hello",
	})
	if err == nil || !strings.Contains(err.Error(), "failed to enqueue delivery") {
		t.Fatalf("sendMailNow error=%v, want enqueue failure", err)
	}
	var count int
	if err := a.db.QueryRow(`SELECT COUNT(1) FROM messages WHERE mailbox_id=? AND subject=?`, mb.ID, "queue insert failure").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("sent copy should be removed after enqueue failure, count=%d", count)
	}
}

func TestAPITokenManagementStoresHashAndRevokes(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, nil); code != http.StatusOK {
		t.Fatalf("login code=%d", code)
	}
	var created struct {
		Token string   `json:"token"`
		Item  APIToken `json:"item"`
	}
	if code := admin.do("POST", "/api/me/api-tokens", map[string]string{"name": "integration-test"}, &created); code != http.StatusCreated {
		t.Fatalf("create api token code=%d resp=%+v", code, created)
	}
	nullScopes := bytes.NewBufferString(`{"name":"null-scopes","scopes":null}`)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/me/api-tokens", nullScopes)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin.cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("null scopes create code=%d", resp.StatusCode)
	}
	if !strings.HasPrefix(created.Token, "lq_") || created.Item.ID == "" || created.Item.Name != "integration-test" || created.Item.ExpiresAt == nil {
		t.Fatalf("created token response=%+v", created)
	}
	if remaining := time.Until(*created.Item.ExpiresAt); remaining < 89*24*time.Hour || remaining > 91*24*time.Hour {
		t.Fatalf("created token default expiry=%s, remaining=%s", created.Item.ExpiresAt, remaining)
	}
	var storedHash string
	if err := a.db.QueryRow(`SELECT token_hash FROM api_tokens WHERE id=?`, created.Item.ID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if storedHash == created.Token || storedHash != hashToken(created.Token) {
		t.Fatalf("stored token hash=%q token=%q", storedHash, created.Token)
	}

	openAdmin := &testClient{t: t, server: ts, bearer: created.Token}
	var domains struct {
		Items []Domain `json:"items"`
	}
	if code := openAdmin.do("GET", "/api/open/domains", nil, &domains); code != http.StatusOK {
		t.Fatalf("open api with bearer token code=%d", code)
	}
	if _, err := a.db.Exec(`UPDATE api_tokens SET expires_at=? WHERE id=?`, a.now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), created.Item.ID); err != nil {
		t.Fatal(err)
	}
	if code := openAdmin.do("GET", "/api/open/domains", nil, &map[string]any{}); code != http.StatusUnauthorized {
		t.Fatalf("expired bearer token code=%d", code)
	}
	if _, err := a.db.Exec(`UPDATE api_tokens SET expires_at=? WHERE id=?`, created.Item.ExpiresAt.UTC().Format(time.RFC3339Nano), created.Item.ID); err != nil {
		t.Fatal(err)
	}
	var listed struct {
		Items []APIToken `json:"items"`
	}
	if code := admin.do("GET", "/api/me/api-tokens", nil, &listed); code != http.StatusOK {
		t.Fatalf("list api tokens code=%d", code)
	}
	if len(listed.Items) != 1 || listed.Items[0].ID != created.Item.ID || listed.Items[0].LastUsedAt == nil {
		t.Fatalf("listed tokens=%+v", listed.Items)
	}

	if code := admin.do("POST", "/api/me/api-tokens/"+created.Item.ID, map[string]any{"expiresAt": ""}, &map[string]any{}); code != http.StatusBadRequest {
		t.Fatalf("empty api token expiry update code=%d", code)
	}

	disabled := true
	var updated APIToken
	if code := admin.do("POST", "/api/me/api-tokens/"+created.Item.ID, map[string]any{"disabled": disabled}, &updated); code != http.StatusOK {
		t.Fatalf("disable api token code=%d item=%+v", code, updated)
	}
	if !updated.Disabled {
		t.Fatalf("updated token should be disabled: %+v", updated)
	}
	if code := openAdmin.do("GET", "/api/open/domains", nil, &map[string]any{}); code != http.StatusUnauthorized {
		t.Fatalf("disabled bearer token code=%d", code)
	}
	if code := admin.do("DELETE", "/api/me/api-tokens/"+created.Item.ID, nil, &map[string]any{}); code != http.StatusOK {
		t.Fatalf("delete api token code=%d", code)
	}
}

func TestOpenAPIDomainAndMailboxCRUD(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d body=%v", code, login)
	}
	adminToken := createTestAPIToken(t, admin, "admin-open-api")
	openAdmin := &testClient{t: t, server: ts, bearer: adminToken}

	var authErr map[string]any
	if code := admin.do("GET", "/api/open/domains", nil, &authErr); code != http.StatusUnauthorized {
		t.Fatalf("cookie-only open api code=%d body=%v", code, authErr)
	}

	var domain Domain
	if code := openAdmin.do("POST", "/api/open/domains", map[string]string{"name": "api.example.test"}, &domain); code != http.StatusCreated {
		t.Fatalf("create open api domain code=%d domain=%+v", code, domain)
	}
	if domain.Name != "api.example.test" || domain.DKIMPublicKey == "" {
		t.Fatalf("domain=%+v", domain)
	}
	var domains struct {
		Items []Domain `json:"items"`
	}
	if code := openAdmin.do("GET", "/api/open/domains", nil, &domains); code != http.StatusOK {
		t.Fatalf("list open api domains code=%d", code)
	}
	if len(domains.Items) < 2 {
		t.Fatalf("domains=%+v", domains.Items)
	}
	var disabled Domain
	if code := openAdmin.do("POST", "/api/open/domains/"+domain.ID, map[string]string{"status": "disabled"}, &disabled); code != http.StatusOK {
		t.Fatalf("update open api domain code=%d domain=%+v", code, disabled)
	}
	if disabled.Status != "disabled" {
		t.Fatalf("domain status=%q", disabled.Status)
	}
	if code := openAdmin.do("POST", "/api/open/domains/"+domain.ID, map[string]string{"status": "active"}, &domain); code != http.StatusOK {
		t.Fatalf("reactivate open api domain code=%d domain=%+v", code, domain)
	}
	var owner AdminUser
	if code := admin.do("POST", "/api/admin/users", map[string]any{
		"email": "open-api-owner@lanqin.local", "displayName": "Open API Owner", "role": "user", "password": "Password123!",
	}, &owner); code != http.StatusCreated {
		t.Fatalf("create open api mailbox owner code=%d owner=%+v", code, owner)
	}

	var mailbox Mailbox
	if code := openAdmin.do("POST", "/api/open/mailboxes", map[string]any{
		"domainId":    domain.ID,
		"localPart":   "api-user",
		"displayName": "API User",
		"password":    "DifferentPassword123!",
		"quotaMb":     256,
		"userId":      owner.ID,
	}, &mailbox); code != http.StatusCreated {
		t.Fatalf("create open api mailbox code=%d mailbox=%+v", code, mailbox)
	}
	ownerPrimary, err := a.mailboxByAddress(context.Background(), owner.Email)
	if err != nil {
		t.Fatal(err)
	}
	var protectedMailboxErr map[string]any
	if code := openAdmin.do("POST", "/api/open/mailboxes/"+ownerPrimary.ID, map[string]any{"status": "disabled"}, &protectedMailboxErr); code != http.StatusBadRequest {
		t.Fatalf("open api primary mailbox status update code=%d body=%v", code, protectedMailboxErr)
	}
	if mailbox.Address != "api-user@api.example.test" || mailbox.QuotaMB != 256 {
		t.Fatalf("mailbox=%+v", mailbox)
	}
	var ownerPasswordHash, mailboxPasswordHash string
	if err := a.db.QueryRowContext(context.Background(), `SELECT password_hash FROM users WHERE id=?`, owner.ID).Scan(&ownerPasswordHash); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRowContext(context.Background(), `SELECT password_hash FROM mailboxes WHERE id=?`, mailbox.ID).Scan(&mailboxPasswordHash); err != nil {
		t.Fatal(err)
	}
	if mailboxPasswordHash != ownerPasswordHash {
		t.Fatal("open api mailbox did not inherit the owner password")
	}
	var mailboxes struct {
		Items []Mailbox `json:"items"`
	}
	if code := openAdmin.do("GET", "/api/open/mailboxes", nil, &mailboxes); code != http.StatusOK {
		t.Fatalf("list open api mailboxes code=%d", code)
	}
	if len(mailboxes.Items) < 2 {
		t.Fatalf("mailboxes=%+v", mailboxes.Items)
	}
	var updated Mailbox
	if code := openAdmin.do("POST", "/api/open/mailboxes/"+mailbox.ID, map[string]any{"displayName": "Renamed API User", "quotaMb": 512, "status": "disabled"}, &updated); code != http.StatusOK {
		t.Fatalf("update open api mailbox code=%d mailbox=%+v", code, updated)
	}
	if updated.DisplayName != "Renamed API User" || updated.QuotaMB != 512 || updated.Status != "disabled" {
		t.Fatalf("updated mailbox=%+v", updated)
	}
	if code := openAdmin.do("POST", "/api/open/mailboxes/"+mailbox.ID, map[string]any{"status": "active"}, &updated); code != http.StatusOK || updated.QuotaMB != 512 {
		t.Fatalf("open api mailbox omitted quota should preserve 512 MB: code=%d mailbox=%+v", code, updated)
	}
	var ok map[string]any
	if code := openAdmin.do("DELETE", "/api/open/mailboxes/"+mailbox.ID, nil, &ok); code != http.StatusOK {
		t.Fatalf("delete open api mailbox code=%d body=%v", code, ok)
	}
	if code := openAdmin.do("DELETE", "/api/open/domains/"+domain.ID, nil, &ok); code != http.StatusOK {
		t.Fatalf("delete open api domain code=%d body=%v", code, ok)
	}
}

func TestOpenAPISendStatusAndMailboxMessages(t *testing.T) {
	a := newTestApp(t)
	stopTestWorkers(a)
	a.updateConfig(func(cfg *Config) { cfg.SMTPHost = "127.0.0.1" })
	a.updateConfig(func(cfg *Config) { cfg.SMTPPort = "25" })
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("admin login code=%d body=%v", code, login)
	}
	domainID := mustDefaultDomainID(t, a)
	sender := createTestMailbox(t, admin, domainID, "open-sender", "Open API Sender", "Password123!", nil)
	recipient := createTestMailbox(t, admin, domainID, "open-recipient", "Open API Recipient", "Password123!", nil)
	other := createTestMailbox(t, admin, domainID, "open-other", "Open API Other", "Password123!", nil)

	senderClient := &testClient{t: t, server: ts}
	if code := senderClient.do("POST", "/api/auth/login", map[string]string{"email": sender.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("sender login code=%d body=%v", code, login)
	}
	senderToken := createTestAPIToken(t, senderClient, "sender-open-api")
	senderOpen := &testClient{t: t, server: ts, bearer: senderToken}
	if code := senderClient.do("POST", "/api/open/send", map[string]any{
		"mailboxId": sender.ID,
		"to":        []string{recipient.Address},
		"subject":   "cookie-only open api send",
		"text":      "this should not authenticate",
	}, &map[string]any{}); code != http.StatusUnauthorized {
		t.Fatalf("cookie-only open api send code=%d", code)
	}
	var sent struct {
		ID             string    `json:"id"`
		QueueID        string    `json:"queueId"`
		Status         string    `json:"status"`
		MessageID      string    `json:"messageId"`
		RFCMessageID   string    `json:"rfcMessageId"`
		MailboxID      string    `json:"mailboxId"`
		MailboxAddress string    `json:"mailboxAddress"`
		Subject        string    `json:"subject"`
		CreatedAt      time.Time `json:"createdAt"`
	}
	if code := senderOpen.do("POST", "/api/open/send", map[string]any{
		"mailboxId": sender.ID,
		"to":        []string{recipient.Address},
		"subject":   "open api send",
		"text":      "hello from open api",
	}, &sent); code != http.StatusCreated {
		t.Fatalf("open api send code=%d body=%+v", code, sent)
	}
	if sent.ID == "" || sent.QueueID == "" || sent.Status != sendQueueStatusQueued || sent.MessageID == "" || sent.MailboxAddress != sender.Address {
		t.Fatalf("sent response=%+v", sent)
	}
	var sendSource string
	if err := a.db.QueryRow(`SELECT source FROM send_audit_events WHERE sent_message_id=? AND event=?`, sent.MessageID, sendAuditAccepted).Scan(&sendSource); err != nil {
		t.Fatal(err)
	}
	if sendSource != sendSourceOpenAPI {
		t.Fatalf("open api send audit source=%q, want %q", sendSource, sendSourceOpenAPI)
	}
	if err := a.db.QueryRow(`SELECT source FROM send_queue WHERE id=?`, sent.QueueID).Scan(&sendSource); err != nil {
		t.Fatal(err)
	}
	if sendSource != sendSourceOpenAPI {
		t.Fatalf("open api send queue source=%q, want %q", sendSource, sendSourceOpenAPI)
	}

	var status struct {
		ID             string `json:"id"`
		QueueID        string `json:"queueId"`
		Status         string `json:"status"`
		MessageID      string `json:"messageId"`
		RFCMessageID   string `json:"rfcMessageId"`
		MailboxID      string `json:"mailboxId"`
		MailboxAddress string `json:"mailboxAddress"`
		Subject        string `json:"subject"`
	}
	if code := senderOpen.do("GET", "/api/open/send/"+sent.ID, nil, &status); code != http.StatusOK {
		t.Fatalf("open api send status code=%d status=%+v", code, status)
	}
	if status.ID != sent.MessageID || status.QueueID != sent.QueueID || status.MessageID != sent.MessageID || status.Status != sendQueueStatusQueued {
		t.Fatalf("status=%+v sent=%+v", status, sent)
	}

	recipientClient := &testClient{t: t, server: ts}
	if code := recipientClient.do("POST", "/api/auth/login", map[string]string{"email": recipient.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("recipient login code=%d body=%v", code, login)
	}
	recipientToken := createTestAPIToken(t, recipientClient, "recipient-open-api")
	recipientOpen := &testClient{t: t, server: ts, bearer: recipientToken}
	var inbox struct {
		Items      []MailMessage `json:"items"`
		NextCursor string        `json:"nextCursor"`
	}
	if code := recipientOpen.do("GET", "/api/open/mailboxes/"+recipient.ID+"/messages?folder=Inbox", nil, &inbox); code != http.StatusOK {
		t.Fatalf("open api mailbox messages code=%d inbox=%+v", code, inbox)
	}
	if len(inbox.Items) != 1 || inbox.Items[0].Subject != "open api send" || inbox.Items[0].From != sender.Address {
		t.Fatalf("inbox=%+v", inbox.Items)
	}
	if code := recipientOpen.do("GET", "/api/open/mailboxes/"+other.ID+"/messages?folder=Inbox", nil, &map[string]any{}); code != http.StatusNotFound {
		t.Fatalf("cross-user mailbox read code=%d", code)
	}
	if code := recipientOpen.do("GET", "/api/open/send/"+sent.ID, nil, &map[string]any{}); code != http.StatusNotFound {
		t.Fatalf("cross-user send status code=%d", code)
	}
}

func TestOpenAPIV1ScopesIdempotencyAndDeliveryEvents(t *testing.T) {
	a := newTestApp(t)
	stopTestWorkers(a)
	a.updateConfig(func(cfg *Config) { cfg.SMTPHost = "127.0.0.1" })
	a.updateConfig(func(cfg *Config) { cfg.SMTPPort = "25" })
	a.updateConfig(func(cfg *Config) { cfg.DeliveryWebhookSecret = "delivery-test-secret" })
	ts := httptest.NewServer(a.Router())
	defer ts.Close()

	admin := &testClient{t: t, server: ts}
	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("admin login code=%d", code)
	}
	domainID := mustDefaultDomainID(t, a)
	sender := createTestMailbox(t, admin, domainID, "v1-sender", "V1 Sender", "Password123!", nil)
	recipient := createTestMailbox(t, admin, domainID, "v1-recipient", "V1 Recipient", "Password123!", nil)

	adminReadToken := createTestAPITokenWithScopes(t, admin, "domain-reader", []string{"domains:read"})
	adminRead := &testClient{t: t, server: ts, bearer: adminReadToken}
	if code := adminRead.do("GET", "/api/open/v1/domains", nil, &map[string]any{}); code != http.StatusOK {
		t.Fatalf("v1 scoped domain list code=%d", code)
	}
	if code := adminRead.do("POST", "/api/open/v1/domains", map[string]string{"name": "scope-denied.example"}, &map[string]any{}); code != http.StatusForbidden {
		t.Fatalf("read-only token domain create code=%d", code)
	}

	senderClient := &testClient{t: t, server: ts}
	if code := senderClient.do("POST", "/api/auth/login", map[string]string{"email": sender.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("sender login code=%d", code)
	}
	sendToken := createTestAPITokenWithScopes(t, senderClient, "send-only", []string{"messages:send"})
	sendClient := &testClient{t: t, server: ts, bearer: sendToken}
	payload := map[string]any{"mailboxId": sender.ID, "to": []string{recipient.Address}, "subject": "idempotent send", "text": "one delivery"}
	headers := map[string]string{"Idempotency-Key": "invoice-42"}
	var first openAPISendStatus
	if code := sendClient.doWithHeaders("POST", "/api/open/v1/send", payload, headers, &first); code != http.StatusCreated {
		t.Fatalf("first idempotent send code=%d body=%+v", code, first)
	}
	if first.ID == "" || first.ID != first.MessageID || first.QueueID == "" || first.ID == first.QueueID {
		t.Fatalf("stable send identifiers=%+v", first)
	}
	var replay openAPISendStatus
	if code := sendClient.doWithHeaders("POST", "/api/open/v1/send", payload, headers, &replay); code != http.StatusOK {
		t.Fatalf("idempotent replay code=%d body=%+v", code, replay)
	}
	if replay.ID != first.ID || replay.QueueID != first.QueueID {
		t.Fatalf("replay=%+v first=%+v", replay, first)
	}
	var queueCount int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM send_queue WHERE source=? AND sent_message_id=?`, sendSourceOpenAPI, first.MessageID).Scan(&queueCount); err != nil || queueCount != 1 {
		t.Fatalf("idempotent queue count=%d err=%v", queueCount, err)
	}
	changed := map[string]any{"mailboxId": sender.ID, "to": []string{recipient.Address}, "subject": "changed", "text": "different"}
	if code := sendClient.doWithHeaders("POST", "/api/open/v1/send", changed, headers, &map[string]any{}); code != http.StatusConflict {
		t.Fatalf("changed idempotency payload code=%d", code)
	}
	if code := sendClient.do("GET", "/api/open/v1/send/"+first.ID, nil, &map[string]any{}); code != http.StatusForbidden {
		t.Fatalf("send-only token read code=%d", code)
	}

	readToken := createTestAPITokenWithScopes(t, senderClient, "read-only", []string{"messages:read"})
	readClient := &testClient{t: t, server: ts, bearer: readToken}
	var queued openAPISendStatus
	if code := readClient.do("GET", "/api/open/v1/send/"+first.ID, nil, &queued); code != http.StatusOK || queued.Status != sendQueueStatusQueued {
		t.Fatalf("read status code=%d body=%+v", code, queued)
	}
	if _, err := a.db.Exec(`UPDATE send_queue SET status=?,updated_at=? WHERE id=?`, sendQueueStatusSending, a.now().UTC().Format(time.RFC3339Nano), first.QueueID); err != nil {
		t.Fatal(err)
	}
	var sending openAPISendStatus
	if code := readClient.do("GET", "/api/open/v1/send/"+first.ID, nil, &sending); code != http.StatusOK || sending.Status != sendQueueStatusSending {
		t.Fatalf("sending status code=%d body=%+v", code, sending)
	}
	if _, err := a.db.Exec(`UPDATE send_queue SET status=?,delivered_at=?,updated_at=? WHERE id=?`, sendQueueStatusDelivered, a.now().UTC().Format(time.RFC3339Nano), a.now().UTC().Format(time.RFC3339Nano), first.QueueID); err != nil {
		t.Fatal(err)
	}
	var relayed openAPISendStatus
	if code := readClient.do("GET", "/api/open/v1/send/"+first.ID, nil, &relayed); code != http.StatusOK || relayed.Status != "relayed" || relayed.QueueStatus != sendQueueStatusDelivered {
		t.Fatalf("relayed status code=%d body=%+v", code, relayed)
	}

	eventPayload := struct {
		Events []deliveryWebhookEvent `json:"events"`
	}{Events: []deliveryWebhookEvent{{ID: "provider-event-1", Provider: "test-provider", MessageID: first.MessageID, Recipient: recipient.Address, Status: "bounced", Reason: "550 mailbox unavailable", OccurredAt: a.now().UTC().Format(time.RFC3339Nano)}}}
	body, _ := json.Marshal(eventPayload)
	timestamp := strconv.FormatInt(a.now().UTC().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(a.config().DeliveryWebhookSecret))
	_, _ = mac.Write([]byte(timestamp + "."))
	_, _ = mac.Write(body)
	webhookHeaders := map[string]string{"X-LanQin-Timestamp": timestamp, "X-LanQin-Signature": "sha256=" + hex.EncodeToString(mac.Sum(nil))}
	badSignatureHeaders := map[string]string{"X-LanQin-Timestamp": timestamp, "X-LanQin-Signature": "sha256=" + strings.Repeat("0", 64)}
	if code := admin.doWithHeaders("POST", "/api/open/v1/delivery-events", eventPayload, badSignatureHeaders, &map[string]any{}); code != http.StatusUnauthorized {
		t.Fatalf("invalid delivery webhook signature code=%d", code)
	}
	oldTimestamp := strconv.FormatInt(a.now().UTC().Add(-10*time.Minute).Unix(), 10)
	oldMAC := hmac.New(sha256.New, []byte(a.config().DeliveryWebhookSecret))
	_, _ = oldMAC.Write([]byte(oldTimestamp + "."))
	_, _ = oldMAC.Write(body)
	oldHeaders := map[string]string{"X-LanQin-Timestamp": oldTimestamp, "X-LanQin-Signature": "sha256=" + hex.EncodeToString(oldMAC.Sum(nil))}
	if code := admin.doWithHeaders("POST", "/api/open/v1/delivery-events", eventPayload, oldHeaders, &map[string]any{}); code != http.StatusUnauthorized {
		t.Fatalf("expired delivery webhook signature code=%d", code)
	}
	var webhookResult struct {
		Accepted   int `json:"accepted"`
		Duplicates int `json:"duplicates"`
	}
	if code := admin.doWithHeaders("POST", "/api/open/v1/delivery-events", eventPayload, webhookHeaders, &webhookResult); code != http.StatusOK || webhookResult.Accepted != 1 {
		t.Fatalf("delivery webhook code=%d body=%+v", code, webhookResult)
	}
	if code := admin.doWithHeaders("POST", "/api/open/v1/delivery-events", eventPayload, webhookHeaders, &webhookResult); code != http.StatusOK || webhookResult.Duplicates != 1 {
		t.Fatalf("delivery webhook duplicate code=%d body=%+v", code, webhookResult)
	}
	var bounced openAPISendStatus
	if code := readClient.do("GET", "/api/open/v1/send/"+first.ID, nil, &bounced); code != http.StatusOK || bounced.Status != "bounced" || len(bounced.RecipientStatuses) != 1 {
		t.Fatalf("bounced status code=%d body=%+v", code, bounced)
	}
	var events struct {
		DeliveryEvents []DeliveryEvent `json:"deliveryEvents"`
	}
	if code := readClient.do("GET", "/api/open/v1/send/"+first.ID+"/events", nil, &events); code != http.StatusOK || len(events.DeliveryEvents) != 1 {
		t.Fatalf("delivery events code=%d body=%+v", code, events)
	}

	manageToken := createTestAPITokenWithScopes(t, senderClient, "send-manager", []string{"messages:read", "messages:manage"})
	manageClient := &testClient{t: t, server: ts, bearer: manageToken}
	if _, err := a.db.Exec(`UPDATE send_queue SET status=?,attempt_count=max_attempts,last_error='test failure',updated_at=? WHERE id=?`, sendQueueStatusFailed, a.now().UTC().Format(time.RFC3339Nano), first.QueueID); err != nil {
		t.Fatal(err)
	}
	var failed openAPISendStatus
	if code := manageClient.do("GET", "/api/open/v1/send/"+first.ID, nil, &failed); code != http.StatusOK || failed.QueueStatus != sendQueueStatusFailed {
		t.Fatalf("failed status code=%d body=%+v", code, failed)
	}
	var retried openAPISendStatus
	if code := manageClient.do("POST", "/api/open/v1/send/"+first.ID+"/retry", nil, &retried); code != http.StatusOK || retried.QueueStatus != sendQueueStatusQueued {
		t.Fatalf("retry code=%d body=%+v", code, retried)
	}
	var canceled openAPISendStatus
	if code := manageClient.do("POST", "/api/open/v1/send/"+first.ID+"/cancel", nil, &canceled); code != http.StatusOK || canceled.QueueStatus != sendQueueStatusCanceled {
		t.Fatalf("cancel code=%d body=%+v", code, canceled)
	}
}

func TestOpenAPIPaginationAndMailboxCreateRollback(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}
	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d", code)
	}
	token := createTestAPITokenWithScopes(t, admin, "admin-v1", []string{"domains:read", "mailboxes:write"})
	openAdmin := &testClient{t: t, server: ts, bearer: token}
	createTestDomain(t, admin, "pagination-one.test")
	createTestDomain(t, admin, "pagination-two.test")
	var firstPage struct {
		Items      []Domain `json:"items"`
		NextCursor string   `json:"nextCursor"`
	}
	if code := openAdmin.do("GET", "/api/open/v1/domains?limit=1", nil, &firstPage); code != http.StatusOK || len(firstPage.Items) != 1 || firstPage.NextCursor == "" {
		t.Fatalf("first domain page code=%d body=%+v", code, firstPage)
	}
	var secondPage struct {
		Items []Domain `json:"items"`
	}
	if code := openAdmin.do("GET", "/api/open/v1/domains?limit=1&cursor="+url.QueryEscape(firstPage.NextCursor), nil, &secondPage); code != http.StatusOK || len(secondPage.Items) != 1 || secondPage.Items[0].ID == firstPage.Items[0].ID {
		t.Fatalf("second domain page code=%d body=%+v", code, secondPage)
	}

	domainID := mustDefaultDomainID(t, a)
	createTestMailbox(t, admin, domainID, "rollback-address", "Existing", "Password123!", nil)
	payload := map[string]any{"domainId": domainID, "localPart": "rollback-address", "displayName": "Should Rollback", "password": "Password123!", "ownerEmail": "orphan-owner@example.test"}
	if code := openAdmin.do("POST", "/api/open/v1/mailboxes", payload, &map[string]any{}); code != http.StatusBadRequest {
		t.Fatalf("duplicate mailbox create code=%d", code)
	}
	var orphanCount int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM users WHERE email=?`, "orphan-owner@example.test").Scan(&orphanCount); err != nil || orphanCount != 0 {
		t.Fatalf("orphan user count=%d err=%v", orphanCount, err)
	}
}

func TestOpenAPIContractCoversV1Routes(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "docs", "openapi.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		OpenAPI    string                                `json:"openapi"`
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			SecuritySchemes map[string]json.RawMessage `json:"securitySchemes"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("parse openapi contract: %v", err)
	}
	if document.OpenAPI != "3.1.0" || document.Components.SecuritySchemes["bearerAuth"] == nil {
		t.Fatalf("invalid openapi metadata: version=%q security=%v", document.OpenAPI, document.Components.SecuritySchemes)
	}
	routes := map[string][]string{
		"/domains": {"get", "post"}, "/domains/{id}": {"get", "post", "delete"},
		"/domains/{id}/dns-records": {"get"}, "/domains/{id}/dns-check": {"post"},
		"/mailboxes": {"get", "post"}, "/mailboxes/{id}": {"get", "post", "delete"},
		"/mailboxes/{id}/password": {"post"}, "/mailboxes/{id}/messages": {"get"},
		"/messages/{id}": {"get"}, "/attachments/{id}": {"get"},
		"/send": {"get", "post"}, "/send/{id}": {"get"}, "/send/{id}/events": {"get"},
		"/send/{id}/retry": {"post"}, "/send/{id}/cancel": {"post"},
		"/aliases": {"get", "post"}, "/aliases/{id}": {"get", "post", "delete"},
		"/delivery-events": {"post"},
	}
	for route, methods := range routes {
		pathItem := document.Paths[route]
		if pathItem == nil {
			t.Errorf("openapi missing path %s", route)
			continue
		}
		for _, method := range methods {
			if pathItem[method] == nil {
				t.Errorf("openapi missing operation %s %s", strings.ToUpper(method), route)
			}
		}
		if strings.Contains(route, "{id}") {
			var raw map[string]any
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatal(err)
			}
			paths, _ := raw["paths"].(map[string]any)
			item, _ := paths[route].(map[string]any)
			parameters, _ := item["parameters"].([]any)
			foundID := false
			for _, value := range parameters {
				parameter, _ := value.(map[string]any)
				if parameter["$ref"] == "#/components/parameters/ResourceId" || parameter["name"] == "id" {
					foundID = true
				}
			}
			if !foundID {
				t.Errorf("openapi path %s does not declare id parameter", route)
			}
		}
	}
}

func TestStatusWebhookOutboxDeliveryRetryAndSSRFProtection(t *testing.T) {
	a := newTestApp(t)
	stopTestWorkers(a)
	accept := false
	requests := 0
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		timestamp := r.Header.Get("X-LanQin-Timestamp")
		mac := hmac.New(sha256.New, []byte("outbound-test-secret"))
		_, _ = mac.Write([]byte(timestamp + "."))
		_, _ = mac.Write(body)
		if r.Header.Get("X-LanQin-Webhook-Id") == "" || r.Header.Get("X-LanQin-Signature") != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
			t.Error("invalid outbound webhook signature headers")
		}
		var envelope statusWebhookEnvelope
		if err := json.Unmarshal(body, &envelope); err != nil || envelope.Type != "send.failed" {
			t.Errorf("invalid outbound webhook payload: err=%v payload=%s", err, body)
		}
		if !accept {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	a.updateConfig(func(cfg *Config) { cfg.StatusWebhookURL = receiver.URL })
	a.updateConfig(func(cfg *Config) { cfg.StatusWebhookSecret = "outbound-test-secret" })
	a.updateConfig(func(cfg *Config) { cfg.StatusWebhookAllowPrivateHosts = true })

	user, mb := defaultAdminUserAndMailbox(t, a)
	a.recordSendAudit(context.Background(), sendAuditFailed, sendQueueStatusFailed, sendAuditInput{QueueID: "snd_test", UserID: user.ID, MailboxID: mb.ID, SentMessageID: "mail_test", Source: sendSourceOpenAPI, MailFrom: mb.Address, Recipients: []string{"recipient@example.test"}, Error: "test failure"})
	var outboxID string
	if err := a.db.QueryRow(`SELECT id FROM status_webhook_outbox WHERE event_type='send.failed'`).Scan(&outboxID); err != nil {
		t.Fatal(err)
	}
	if err := a.processDueStatusWebhooks(context.Background()); err != nil {
		t.Fatal(err)
	}
	var attempts int
	var deliveredAt sql.NullString
	if err := a.db.QueryRow(`SELECT attempt_count,delivered_at FROM status_webhook_outbox WHERE id=?`, outboxID).Scan(&attempts, &deliveredAt); err != nil || attempts != 1 || deliveredAt.Valid {
		t.Fatalf("failed delivery outbox attempts=%d delivered=%v err=%v", attempts, deliveredAt, err)
	}
	accept = true
	if _, err := a.db.Exec(`UPDATE status_webhook_outbox SET next_attempt_at=? WHERE id=?`, a.now().UTC().Format(time.RFC3339Nano), outboxID); err != nil {
		t.Fatal(err)
	}
	if err := a.processDueStatusWebhooks(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow(`SELECT attempt_count,delivered_at FROM status_webhook_outbox WHERE id=?`, outboxID).Scan(&attempts, &deliveredAt); err != nil || attempts != 2 || !deliveredAt.Valid || requests != 2 {
		t.Fatalf("successful retry attempts=%d delivered=%v requests=%d err=%v", attempts, deliveredAt, requests, err)
	}
	if _, err := a.db.Exec(`DELETE FROM mailboxes WHERE id=?`, mb.ID); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM status_webhook_outbox WHERE id=?`, outboxID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("mailbox deletion should remove webhook outbox, remaining=%d err=%v", remaining, err)
	}

	privateTLS := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer privateTLS.Close()
	a.updateConfig(func(cfg *Config) { cfg.StatusWebhookURL = privateTLS.URL })
	a.updateConfig(func(cfg *Config) { cfg.StatusWebhookAllowPrivateHosts = false })
	if _, err := a.validatedStatusWebhookURL(context.Background()); err == nil || !strings.Contains(err.Error(), "private or local") {
		t.Fatalf("private webhook target should be rejected, err=%v", err)
	}
}

func TestSendQueueRecoversStaleSendingItems(t *testing.T) {
	a := newTestApp(t)
	stopTestWorkers(a)
	host, port, received := startCapturingSMTP(t, 1)
	a.updateConfig(func(cfg *Config) { cfg.SMTPHost = host })
	a.updateConfig(func(cfg *Config) { cfg.SMTPPort = port })
	user, mb := defaultAdminUserAndMailbox(t, a)
	now := a.now().UTC()
	mimeBytes := []byte("From: admin@lanqin.local\r\nTo: person@example.com\r\nSubject: stale\r\n\r\nbody")
	queueID, err := a.enqueueSend(context.Background(), sendQueueInput{
		UserID:     user.ID,
		MailboxID:  mb.ID,
		Source:     sendSourceWebmail,
		MailFrom:   mb.Address,
		HeaderFrom: mb.Address,
		Recipients: []string{"person@example.com"},
		MIMEBytes:  mimeBytes,
		Now:        now,
	})
	if err != nil {
		t.Fatal(err)
	}
	staleAt := now.Add(-sendQueueStaleAfter - time.Minute).Format(time.RFC3339Nano)
	if _, err := a.db.Exec(`UPDATE send_queue SET status=?,attempt_count=1,updated_at=? WHERE id=?`, sendQueueStatusSending, staleAt, queueID); err != nil {
		t.Fatal(err)
	}
	if err := a.processDueSendQueue(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("recovered queue item was not relayed")
	}
	var status string
	if err := a.db.QueryRow(`SELECT status FROM send_queue WHERE id=?`, queueID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != sendQueueStatusDelivered {
		t.Fatalf("queue status=%q, want delivered", status)
	}
	var mimeBase64 string
	if err := a.db.QueryRow(`SELECT mime_base64 FROM send_queue WHERE id=?`, queueID).Scan(&mimeBase64); err != nil {
		t.Fatal(err)
	}
	if mimeBase64 != "" {
		t.Fatal("delivered queue item should not retain raw MIME")
	}
}

func TestSendQueueStaleDeliveredMarkerDoesNotRedeliver(t *testing.T) {
	a := newTestApp(t)
	stopTestWorkers(a)
	host, port, received := startCapturingSMTP(t, 1)
	a.updateConfig(func(cfg *Config) { cfg.SMTPHost = host })
	a.updateConfig(func(cfg *Config) { cfg.SMTPPort = port })
	user, mb := defaultAdminUserAndMailbox(t, a)
	now := a.now().UTC()
	mimeBytes := []byte("From: admin@lanqin.local\r\nTo: person@example.com\r\nSubject: marker\r\n\r\nbody")
	queueID, err := a.enqueueSend(context.Background(), sendQueueInput{
		UserID:     user.ID,
		MailboxID:  mb.ID,
		Source:     sendSourceWebmail,
		MailFrom:   mb.Address,
		HeaderFrom: mb.Address,
		Recipients: []string{"person@example.com"},
		MIMEBytes:  mimeBytes,
		Now:        now,
	})
	if err != nil {
		t.Fatal(err)
	}
	staleAt := now.Add(-sendQueueStaleAfter - time.Minute).Format(time.RFC3339Nano)
	if _, err := a.db.Exec(`UPDATE send_queue SET status=?,attempt_count=1,updated_at=? WHERE id=?`, sendQueueStatusSending, staleAt, queueID); err != nil {
		t.Fatal(err)
	}
	if err := a.writeSendQueueDeliveredMarker(queueID); err != nil {
		t.Fatal(err)
	}
	if err := a.processDueSendQueue(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case body := <-received:
		t.Fatalf("stale delivered marker should not redeliver, got %q", body)
	case <-time.After(200 * time.Millisecond):
	}
	var status, mimeBase64 string
	if err := a.db.QueryRow(`SELECT status,mime_base64 FROM send_queue WHERE id=?`, queueID).Scan(&status, &mimeBase64); err != nil {
		t.Fatal(err)
	}
	if status != sendQueueStatusDelivered {
		t.Fatalf("queue status=%q, want delivered", status)
	}
	if mimeBase64 != "" {
		t.Fatal("delivered marker recovery should clear raw MIME")
	}
	delivered, err := a.hasSendQueueDeliveredMarker(queueID)
	if err != nil {
		t.Fatal(err)
	}
	if delivered {
		t.Fatal("delivered marker should be removed after database state is repaired")
	}
}

func TestSendQueueAPIPermissionIsolation(t *testing.T) {
	a := newTestApp(t)
	a.updateConfig(func(cfg *Config) { cfg.SMTPHost = "127.0.0.1" })
	a.updateConfig(func(cfg *Config) { cfg.SMTPPort = "25" })
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("admin login code=%d", code)
	}
	domainID := mustDefaultDomainID(t, a)
	aliceMB := createTestMailbox(t, admin, domainID, "queue-alice", "Queue Alice", "Password123!", nil)
	bobMB := createTestMailbox(t, admin, domainID, "queue-bob", "Queue Bob", "Password123!", nil)
	aliceUser, _, err := a.userByEmail(context.Background(), aliceMB.Address)
	if err != nil {
		t.Fatal(err)
	}
	bobUser, _, err := a.userByEmail(context.Background(), bobMB.Address)
	if err != nil {
		t.Fatal(err)
	}
	now := a.now().UTC()
	aliceQueueID, err := a.enqueueSend(context.Background(), sendQueueInput{
		UserID:     aliceUser.ID,
		MailboxID:  aliceMB.ID,
		MessageID:  "<alice-queue@example.test>",
		Source:     sendSourceWebmail,
		MailFrom:   aliceMB.Address,
		HeaderFrom: aliceMB.Address,
		Recipients: []string{"person@example.test"},
		MIMEBytes:  []byte("From: queue-alice@example.test\r\nTo: person@example.test\r\nSubject: alice\r\n\r\nbody"),
		Now:        now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.enqueueSend(context.Background(), sendQueueInput{
		UserID:     bobUser.ID,
		MailboxID:  bobMB.ID,
		MessageID:  "<bob-queue@example.test>",
		Source:     sendSourceWebmail,
		MailFrom:   bobMB.Address,
		HeaderFrom: bobMB.Address,
		Recipients: []string{"person@example.test"},
		MIMEBytes:  []byte("From: queue-bob@example.test\r\nTo: person@example.test\r\nSubject: bob\r\n\r\nbody"),
		Now:        now,
	}); err != nil {
		t.Fatal(err)
	}

	alice := &testClient{t: t, server: ts}
	if code := alice.do("POST", "/api/auth/login", map[string]string{"email": aliceMB.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("alice login code=%d", code)
	}
	var list struct {
		Items []SendQueueEntry `json:"items"`
	}
	if code := alice.do("GET", "/api/mail/send-queue?mailboxId="+aliceMB.ID, nil, &list); code != http.StatusOK {
		t.Fatalf("list own queue code=%d items=%+v", code, list.Items)
	}
	if len(list.Items) != 1 || list.Items[0].ID != aliceQueueID || list.Items[0].MailboxID != aliceMB.ID {
		t.Fatalf("own queue isolation failed: %+v", list.Items)
	}
	if code := alice.do("GET", "/api/mail/send-queue?mailboxId="+bobMB.ID, nil, &map[string]any{}); code != http.StatusNotFound {
		t.Fatalf("listing another mailbox should be hidden, code=%d", code)
	}
	if code := alice.do("GET", "/api/mail/send-queue/"+list.Items[0].ID+"/audit", nil, &struct {
		Items []SendAuditEvent `json:"items"`
	}{}); code != http.StatusOK {
		t.Fatalf("own audit code=%d", code)
	}
}

func TestSendQueueAPIFiltersStableCursorAndMessageDetailLink(t *testing.T) {
	a := newTestApp(t)
	a.updateConfig(func(cfg *Config) { cfg.SMTPHost = "127.0.0.1" })
	a.updateConfig(func(cfg *Config) { cfg.SMTPPort = "25" })
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	client := &testClient{t: t, server: ts}

	var login map[string]any
	if code := client.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d", code)
	}
	user, mb := defaultAdminUserAndMailbox(t, a)
	now := a.now().UTC()
	sentFolderID, err := a.ensureFolder(context.Background(), mb.ID, "Sent")
	if err != nil {
		t.Fatal(err)
	}
	sentMsg := storedMessage{
		MailboxID:  mb.ID,
		FolderID:   sentFolderID,
		MessageUID: "uid-queue-detail",
		MessageID:  "<queue-detail@example.test>",
		Subject:    "queue detail",
		From:       mb.Address,
		To:         []string{"detail@example.test"},
		SentAt:     now,
		ReceivedAt: now,
		Snippet:    "detail",
		BodyText:   "detail",
		IsRead:     true,
	}
	sentID, err := a.insertMessage(context.Background(), sentMsg, nil)
	if err != nil {
		t.Fatal(err)
	}
	firstID, err := a.enqueueSend(context.Background(), sendQueueInput{
		UserID:        user.ID,
		MailboxID:     mb.ID,
		SentMessageID: sentID,
		MessageID:     "<queue-detail@example.test>",
		Source:        sendSourceWebmail,
		MailFrom:      mb.Address,
		HeaderFrom:    mb.Address,
		Recipients:    []string{"detail@example.test"},
		MIMEBytes:     []byte("Subject: detail\r\n\r\nbody"),
		Now:           now.Add(-2 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	secondID, err := a.enqueueSend(context.Background(), sendQueueInput{
		UserID:     user.ID,
		MailboxID:  mb.ID,
		MessageID:  "<queue-other@example.test>",
		Source:     sendSourceWebmail,
		MailFrom:   mb.Address,
		HeaderFrom: mb.Address,
		Recipients: []string{"other@example.test"},
		MIMEBytes:  []byte("Subject: other\r\n\r\nbody"),
		Now:        now.Add(-1 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	thirdID, err := a.enqueueSend(context.Background(), sendQueueInput{
		UserID:     user.ID,
		MailboxID:  mb.ID,
		MessageID:  "<queue-latest@example.test>",
		Source:     sendSourceWebmail,
		MailFrom:   mb.Address,
		HeaderFrom: mb.Address,
		Recipients: []string{"latest@example.test"},
		MIMEBytes:  []byte("Subject: latest\r\n\r\nbody"),
		Now:        now,
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 28; i++ {
		if _, err := a.enqueueSend(context.Background(), sendQueueInput{
			UserID:     user.ID,
			MailboxID:  mb.ID,
			MessageID:  fmt.Sprintf("<queue-extra-%02d@example.test>", i),
			Source:     sendSourceWebmail,
			MailFrom:   mb.Address,
			HeaderFrom: mb.Address,
			Recipients: []string{fmt.Sprintf("extra-%02d@example.test", i)},
			MIMEBytes:  []byte("Subject: extra\r\n\r\nbody"),
			Now:        now.Add(time.Duration(-24-i) * time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}

	var byMessage struct {
		Items []SendQueueEntry `json:"items"`
	}
	if code := client.do("GET", "/api/mail/send-queue?messageId="+url.QueryEscape("<queue-detail@example.test>"), nil, &byMessage); code != http.StatusOK || len(byMessage.Items) != 1 || byMessage.Items[0].ID != firstID {
		t.Fatalf("message filter code=%d items=%+v", code, byMessage.Items)
	}
	var byRecipient struct {
		Items []SendQueueEntry `json:"items"`
	}
	if code := client.do("GET", "/api/mail/send-queue?recipient="+url.QueryEscape("other@example.test"), nil, &byRecipient); code != http.StatusOK || len(byRecipient.Items) != 1 || byRecipient.Items[0].ID != secondID {
		t.Fatalf("recipient filter code=%d items=%+v", code, byRecipient.Items)
	}
	var byTime struct {
		Items []SendQueueEntry `json:"items"`
	}
	from := now.Add(-90 * time.Minute).Format(time.RFC3339Nano)
	to := now.Add(30 * time.Minute).Format(time.RFC3339Nano)
	if code := client.do("GET", "/api/mail/send-queue?from="+url.QueryEscape(from)+"&to="+url.QueryEscape(to), nil, &byTime); code != http.StatusOK || len(byTime.Items) != 2 || byTime.Items[0].ID != thirdID || byTime.Items[1].ID != secondID {
		t.Fatalf("time filter code=%d items=%+v", code, byTime.Items)
	}
	var firstPage struct {
		Items      []SendQueueEntry `json:"items"`
		NextCursor string           `json:"nextCursor"`
	}
	if code := client.do("GET", "/api/mail/send-queue?cursor=0", nil, &firstPage); code != http.StatusOK || len(firstPage.Items) != 30 || firstPage.NextCursor == "" {
		t.Fatalf("first page code=%d cursor=%q items=%+v", code, firstPage.NextCursor, firstPage.Items)
	}
	if _, _, _, err := parseSendQueueCursor(firstPage.NextCursor); err != nil {
		t.Fatalf("next cursor is not stable cursor: %q err=%v", firstPage.NextCursor, err)
	}
	var detail MailMessage
	if code := client.do("GET", "/api/mail/messages/"+sentID+"?markRead=0", nil, &detail); code != http.StatusOK || detail.SendQueueID != firstID || detail.SendQueueStatus == "" {
		t.Fatalf("message detail queue link code=%d detail=%+v", code, detail)
	}
}

func TestSendQueueAPIRetryAndCancel(t *testing.T) {
	a := newTestApp(t)
	host, port, received := startCapturingSMTP(t, 1)
	a.updateConfig(func(cfg *Config) { cfg.SMTPHost = host })
	a.updateConfig(func(cfg *Config) { cfg.SMTPPort = port })
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	client := &testClient{t: t, server: ts}

	var login map[string]any
	if code := client.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d", code)
	}
	user, mb := defaultAdminUserAndMailbox(t, a)
	now := a.now().UTC()
	failedID, err := a.enqueueSend(context.Background(), sendQueueInput{
		UserID:     user.ID,
		MailboxID:  mb.ID,
		MessageID:  "<failed-retry-api@example.test>",
		Source:     sendSourceWebmail,
		MailFrom:   mb.Address,
		HeaderFrom: mb.Address,
		Recipients: []string{"person@example.test"},
		MIMEBytes:  []byte("From: admin@lanqin.local\r\nTo: person@example.test\r\nSubject: retry\r\n\r\nbody"),
		Now:        now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`UPDATE send_queue SET status=?,attempt_count=3,last_error='temporary failure',next_attempt_at=? WHERE id=?`, sendQueueStatusFailed, now.Add(time.Hour).Format(time.RFC3339Nano), failedID); err != nil {
		t.Fatal(err)
	}
	var retried SendQueueEntry
	if code := client.do("POST", "/api/mail/send-queue/"+failedID+"/retry", nil, &retried); code != http.StatusOK {
		t.Fatalf("retry failed queue code=%d item=%+v", code, retried)
	}
	if retried.Status != sendQueueStatusQueued || retried.AttemptCount != 0 || retried.LastError != "" {
		t.Fatalf("retry did not reset queue item: %+v", retried)
	}
	if err := a.processDueSendQueue(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case body := <-received:
		if !strings.Contains(body, "Subject: retry") {
			t.Fatalf("unexpected retried body: %q", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("retried queue item was not relayed")
	}

	deliveredID, err := a.enqueueSend(context.Background(), sendQueueInput{
		UserID:     user.ID,
		MailboxID:  mb.ID,
		MessageID:  "<delivered-retry-api@example.test>",
		Source:     sendSourceWebmail,
		MailFrom:   mb.Address,
		HeaderFrom: mb.Address,
		Recipients: []string{"person@example.test"},
		MIMEBytes:  []byte("From: admin@lanqin.local\r\nTo: person@example.test\r\nSubject: delivered\r\n\r\nbody"),
		Now:        now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`UPDATE send_queue SET status=?,delivered_at=?,mime_base64='' WHERE id=?`, sendQueueStatusDelivered, now.Format(time.RFC3339Nano), deliveredID); err != nil {
		t.Fatal(err)
	}
	if code := client.do("POST", "/api/mail/send-queue/"+deliveredID+"/retry", nil, &map[string]any{}); code != http.StatusBadRequest {
		t.Fatalf("delivered retry should be rejected, code=%d", code)
	}

	cancelID, err := a.enqueueSend(context.Background(), sendQueueInput{
		UserID:     user.ID,
		MailboxID:  mb.ID,
		MessageID:  "<cancel-api@example.test>",
		Source:     sendSourceWebmail,
		MailFrom:   mb.Address,
		HeaderFrom: mb.Address,
		Recipients: []string{"person@example.test"},
		MIMEBytes:  []byte("From: admin@lanqin.local\r\nTo: person@example.test\r\nSubject: cancel\r\n\r\nbody"),
		Now:        now,
	})
	if err != nil {
		t.Fatal(err)
	}
	var canceled SendQueueEntry
	if code := client.do("DELETE", "/api/mail/send-queue/"+cancelID, nil, &canceled); code != http.StatusOK {
		t.Fatalf("cancel queued item code=%d item=%+v", code, canceled)
	}
	if canceled.Status != sendQueueStatusCanceled {
		t.Fatalf("canceled status=%q", canceled.Status)
	}
	if err := a.processDueSendQueue(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case body := <-received:
		if strings.Contains(body, "Subject: cancel") {
			t.Fatalf("canceled queue item was relayed: %q", body)
		}
	case <-time.After(200 * time.Millisecond):
	}
	var status string
	if err := a.db.QueryRow(`SELECT status FROM send_queue WHERE id=?`, cancelID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != sendQueueStatusCanceled {
		t.Fatalf("cancel status after worker=%q", status)
	}
}

func TestAdminSendAuditAccessAndFilters(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("admin login code=%d", code)
	}
	user, mb := defaultAdminUserAndMailbox(t, a)
	domainID := mustDefaultDomainID(t, a)
	otherMB := createTestMailbox(t, admin, domainID, "audit-other", "Audit Other", "Password123!", nil)
	now := a.now().UTC()
	ctx := context.Background()
	sentFolderID, err := a.ensureFolder(ctx, mb.ID, "Sent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.ExecContext(ctx, `INSERT INTO messages(id,mailbox_id,folder_id,recipient_addr,message_uid,message_id,subject,from_addr,from_name,to_addrs,cc_addrs,bcc_addrs,sent_at,received_at,snippet,body_text,body_html,is_read,is_starred,has_attachments,size_bytes,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"msg_audit_one", mb.ID, sentFolderID, "", "uid-audit-one", "<audit-one@example.test>", "audit one", mb.Address, "", jsonEncode([]string{"one@example.test"}), "[]", "[]", now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), "audit", "", "", 1, 0, 0, 0, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.ExecContext(ctx, `INSERT INTO send_queue(id,user_id,mailbox_id,sent_message_id,message_id,source,mail_from,header_from,recipients_json,mime_base64,status,next_attempt_at,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"snd_audit_one", user.ID, mb.ID, "msg_audit_one", "<audit-one@example.test>", sendSourceWebmail, mb.Address, mb.Address, jsonEncode([]string{"one@example.test"}), "", sendQueueStatusQueued, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	events := []struct {
		id            string
		queueID       string
		mailboxID     string
		sentMessageID string
		event         string
		status        string
		recipients    []string
		errorText     string
		createdAt     time.Time
	}{
		{"audit_one", "snd_audit_one", mb.ID, "msg_audit_one", sendAuditQueued, sendQueueStatusQueued, []string{"one@example.test"}, "", now.Add(-2 * time.Hour)},
		{"audit_two", "snd_audit_one", mb.ID, "msg_audit_one", sendAuditFailed, sendQueueStatusFailed, []string{"one@example.test"}, "temporary failure", now.Add(-1 * time.Hour)},
		{"audit_other", "snd_audit_other", otherMB.ID, "", sendAuditDelivered, sendQueueStatusDelivered, []string{"two@example.test"}, "", now},
	}
	for _, item := range events {
		if _, err := a.db.ExecContext(ctx, `INSERT INTO send_audit_events(id,queue_id,user_id,mailbox_id,sent_message_id,source,event,status,mail_from,header_from,recipients_json,error,created_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, item.id, item.queueID, user.ID, item.mailboxID, item.sentMessageID, sendSourceWebmail, item.event, item.status, mb.Address, mb.Address, jsonEncode(item.recipients), item.errorText, item.createdAt.Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
	}

	if code := (&testClient{t: t, server: ts}).do("GET", "/api/admin/send-audit", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated send audit code=%d", code)
	}
	regular := &testClient{t: t, server: ts}
	if code := regular.do("POST", "/api/auth/login", map[string]string{"email": otherMB.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("regular login code=%d", code)
	}
	if code := regular.do("GET", "/api/admin/send-audit", nil, nil); code != http.StatusForbidden {
		t.Fatalf("regular send audit code=%d", code)
	}
	setRegularPermissionGroupForTest(t, a, []string{PermissionAdminOverview}, defaultPermissionLimits())
	if code := regular.do("GET", "/api/admin/send-audit", nil, nil); code != http.StatusForbidden {
		t.Fatalf("admin access without messages permission code=%d", code)
	}

	var all struct {
		Items []SendAuditEvent `json:"items"`
	}
	if code := admin.do("GET", "/api/admin/send-audit", nil, &all); code != http.StatusOK || len(all.Items) != 3 {
		t.Fatalf("admin all audit code=%d items=%+v", code, all.Items)
	}
	var byMailbox struct {
		Items []SendAuditEvent `json:"items"`
	}
	if code := admin.do("GET", "/api/admin/send-audit?mailboxId="+mb.ID, nil, &byMailbox); code != http.StatusOK || len(byMailbox.Items) != 2 {
		t.Fatalf("mailbox filter code=%d items=%+v", code, byMailbox.Items)
	}
	var byEvent struct {
		Items []SendAuditEvent `json:"items"`
	}
	if code := admin.do("GET", "/api/admin/send-audit?event=failed", nil, &byEvent); code != http.StatusOK || len(byEvent.Items) != 1 || byEvent.Items[0].Error != "temporary failure" {
		t.Fatalf("event filter code=%d items=%+v", code, byEvent.Items)
	}
	var byMessage struct {
		Items []SendAuditEvent `json:"items"`
	}
	if code := admin.do("GET", "/api/admin/send-audit?messageId="+url.QueryEscape("<audit-one@example.test>"), nil, &byMessage); code != http.StatusOK || len(byMessage.Items) != 2 {
		t.Fatalf("message filter code=%d items=%+v", code, byMessage.Items)
	}
	var byTime struct {
		Items []SendAuditEvent `json:"items"`
	}
	from := now.Add(-90 * time.Minute).Format(time.RFC3339Nano)
	if code := admin.do("GET", "/api/admin/send-audit?from="+url.QueryEscape(from), nil, &byTime); code != http.StatusOK || len(byTime.Items) != 2 {
		t.Fatalf("time filter code=%d items=%+v", code, byTime.Items)
	}
	if byEvent.Items[0].MailboxAddress != mb.Address || byEvent.Items[0].MessageID != "<audit-one@example.test>" {
		t.Fatalf("audit metadata missing: %+v", byEvent.Items[0])
	}
}

func TestSubmissionAuthRequiresMailboxPasswordAndSendPermission(t *testing.T) {
	a := newTestApp(t)
	user, mailbox, err := a.authenticateSubmission(context.Background(), "admin@lanqin.local", "ChangeMe123!")
	if err != nil {
		t.Fatalf("authenticate submission: %v", err)
	}
	if user.Email != "admin@lanqin.local" || mailbox.Address != "admin@lanqin.local" {
		t.Fatalf("unexpected auth user=%+v mailbox=%+v", user, mailbox)
	}
	if _, _, err := a.authenticateSubmission(context.Background(), "admin@lanqin.local", "wrong-password"); err == nil {
		t.Fatal("wrong password should fail")
	}

	ctx := context.Background()
	hash, err := bcrypt.GenerateFromPassword([]byte("Password123!"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	userID := newID("usr")
	domainID := mustDefaultDomainID(t, a)
	now := a.now().UTC().Format(time.RFC3339Nano)
	if _, err := a.db.ExecContext(ctx, `INSERT INTO users(id,email,display_name,role,password_hash,disabled,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, userID, "nosend@lanqin.local", "No Send", "user", string(hash), 0, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.ExecContext(ctx, `INSERT INTO mailboxes(id,user_id,domain_id,local_part,address,display_name,password_hash,quota_mb,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, newID("mb"), userID, domainID, "nosend", "nosend@lanqin.local", "No Send", string(hash), 1024, "active", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.ExecContext(ctx, `UPDATE permission_groups SET permissions_json=?, updated_at=? WHERE id=?`, encodePermissions(withoutPermissions(regularUserDefaultPermissions(), PermissionMailSend)), now, PermissionGroupRegular); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.authenticateSubmission(ctx, "nosend@lanqin.local", "Password123!"); err == nil {
		t.Fatal("missing send permission should fail")
	}
	if _, err := a.db.ExecContext(ctx, `UPDATE users SET disabled=1 WHERE id=?`, userID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.authenticateSubmission(ctx, "nosend@lanqin.local", "Password123!"); err == nil {
		t.Fatal("disabled owner should fail")
	}
}

func TestSubmissionSendsRelayAndStoresSentCopy(t *testing.T) {
	a := newTestApp(t)
	host, port, received := startCapturingSMTP(t, 2)
	a.updateConfig(func(cfg *Config) { cfg.SMTPHost = host })
	a.updateConfig(func(cfg *Config) { cfg.SMTPPort = port })
	raw := strings.Join([]string{
		"From: Admin <admin@lanqin.local>",
		"To: person@example.com",
		"Bcc: hidden@example.com",
		"Subject: Submission sent",
		"Message-ID: <submission-sent@example.test>",
		"Date: Tue, 24 Jun 2025 10:00:00 +0000",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"hello from submission",
	}, "\r\n")
	user, mb, err := a.authenticateSubmission(context.Background(), "admin@lanqin.local", "ChangeMe123!")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.submitSMTPMessage(context.Background(), user, mb, mb.Address, []string{"person@example.com", "hidden@example.com"}, strings.NewReader(raw)); err != nil {
		t.Fatalf("submit smtp message: %v", err)
	}
	if err := a.processDueSendQueue(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case body := <-received:
		if strings.Contains(strings.ToLower(body), "\r\nbcc:") || strings.Contains(body, "hidden@example.com") {
			t.Fatalf("relay body leaked bcc: %s", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("relay message not received")
	}
	sentFolderID, err := a.ensureFolder(context.Background(), mb.ID, "Sent")
	if err != nil {
		t.Fatal(err)
	}
	var subject, bccJSON string
	var read int
	if err := a.db.QueryRow(`SELECT subject,bcc_addrs,is_read FROM messages WHERE mailbox_id=? AND folder_id=? AND message_id=?`, mb.ID, sentFolderID, "<submission-sent@example.test>").Scan(&subject, &bccJSON, &read); err != nil {
		t.Fatal(err)
	}
	if subject != "Submission sent" || read != 1 {
		t.Fatalf("unexpected sent message subject=%q read=%d", subject, read)
	}
	if got := jsonDecodeSlice(bccJSON); len(got) != 1 || got[0] != "hidden@example.com" {
		t.Fatalf("bcc json=%s", bccJSON)
	}
	var deliveredAudits int
	if err := a.db.QueryRow(`SELECT COUNT(1) FROM send_audit_events WHERE event=? AND status=?`, sendAuditDelivered, sendQueueStatusDelivered).Scan(&deliveredAudits); err != nil {
		t.Fatal(err)
	}
	if deliveredAudits != 1 {
		t.Fatalf("delivered audit count=%d, want 1", deliveredAudits)
	}
}

func TestSubmissionRejectsMismatchedSender(t *testing.T) {
	a := newTestApp(t)
	user, mb, err := a.authenticateSubmission(context.Background(), "admin@lanqin.local", "ChangeMe123!")
	if err != nil {
		t.Fatal(err)
	}
	raw := "From: attacker@example.com\r\nTo: person@example.com\r\nSubject: nope\r\n\r\nbody"
	if err := a.submitSMTPMessage(context.Background(), user, mb, mb.Address, []string{"person@example.com"}, strings.NewReader(raw)); err == nil {
		t.Fatal("mismatched header From should fail")
	}
	raw = "From: admin@lanqin.local, attacker@example.com\r\nTo: person@example.com\r\nSubject: nope\r\n\r\nbody"
	if err := a.submitSMTPMessage(context.Background(), user, mb, mb.Address, []string{"person@example.com"}, strings.NewReader(raw)); err == nil {
		t.Fatal("multiple header From addresses should fail")
	}
	raw = "From: admin@lanqin.local\r\nTo: person@example.com\r\nSubject: nope\r\n\r\nbody"
	if err := a.submitSMTPMessage(context.Background(), user, mb, "attacker@example.com", []string{"person@example.com"}, strings.NewReader(raw)); err == nil {
		t.Fatal("mismatched MAIL FROM should fail")
	}
}

func TestSerializeMessageUsesStableHeaderOrder(t *testing.T) {
	header := textproto.MIMEHeader{
		"Subject":  {"stable"},
		"From":     {"admin@lanqin.local"},
		"Message":  {"custom"},
		"X-Zebra":  {"z"},
		"X-Answer": {"a"},
	}
	first := string(serializeMessage(header, []byte("body")))
	for i := 0; i < 20; i++ {
		if got := string(serializeMessage(header, []byte("body"))); got != first {
			t.Fatalf("serializeMessage is not stable:\nfirst=%q\ngot=%q", first, got)
		}
	}
	if !strings.HasPrefix(first, "From: admin@lanqin.local\r\n") {
		t.Fatalf("unexpected header order: %q", first)
	}
}

func TestSubmissionRelayFailureKeepsSentCopyAndRetries(t *testing.T) {
	a := newTestApp(t)
	a.updateConfig(func(cfg *Config) { cfg.SMTPHost = "127.0.0.1" })
	a.updateConfig(func(cfg *Config) { cfg.SMTPPort = "1" })
	user, mb, err := a.authenticateSubmission(context.Background(), "admin@lanqin.local", "ChangeMe123!")
	if err != nil {
		t.Fatal(err)
	}
	raw := "From: admin@lanqin.local\r\nTo: person@example.com\r\nSubject: relay fail\r\nMessage-ID: <relay-fail@example.test>\r\n\r\nbody"
	if err := a.submitSMTPMessage(context.Background(), user, mb, mb.Address, []string{"person@example.com"}, strings.NewReader(raw)); err != nil {
		t.Fatalf("submission should queue relay failure for retry: %v", err)
	}
	if err := a.processDueSendQueue(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := a.db.QueryRow(`SELECT COUNT(1) FROM messages WHERE mailbox_id=? AND message_id=?`, mb.ID, "<relay-fail@example.test>").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("sent copy should remain after queued relay failure, count=%d", count)
	}
	var status, lastError string
	if err := a.db.QueryRow(`SELECT status,last_error FROM send_queue WHERE mailbox_id=? AND sent_message_id <> ''`, mb.ID).Scan(&status, &lastError); err != nil {
		t.Fatal(err)
	}
	if status != sendQueueStatusFailed || lastError == "" {
		t.Fatalf("queue status=%q lastError=%q", status, lastError)
	}
}

func TestSubmissionSentCopyDedupesByMessageID(t *testing.T) {
	a := newTestApp(t)
	host, port, _ := startCapturingSMTP(t, 4)
	a.updateConfig(func(cfg *Config) { cfg.SMTPHost = host })
	a.updateConfig(func(cfg *Config) { cfg.SMTPPort = port })
	user, mb, err := a.authenticateSubmission(context.Background(), "admin@lanqin.local", "ChangeMe123!")
	if err != nil {
		t.Fatal(err)
	}
	raw := "From: admin@lanqin.local\r\nTo: person@example.com\r\nSubject: dedupe\r\nMessage-ID: <dedupe@example.test>\r\n\r\nbody"
	for i := 0; i < 2; i++ {
		if err := a.submitSMTPMessage(context.Background(), user, mb, mb.Address, []string{"person@example.com"}, strings.NewReader(raw)); err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
	}
	sentFolderID, err := a.ensureFolder(context.Background(), mb.ID, "Sent")
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := a.db.QueryRow(`SELECT COUNT(1) FROM messages WHERE mailbox_id=? AND folder_id=? AND message_id=?`, mb.ID, sentFolderID, "<dedupe@example.test>").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("sent copy count=%d, want 1", count)
	}
	var queueCount int
	if err := a.db.QueryRow(`SELECT COUNT(1) FROM send_queue WHERE mailbox_id=? AND message_id=?`, mb.ID, "<dedupe@example.test>").Scan(&queueCount); err != nil {
		t.Fatal(err)
	}
	if queueCount != 1 {
		t.Fatalf("send queue count=%d, want 1", queueCount)
	}
}

func TestInsertSentMessageOnceFailsWhenDedupeKeyHasNoMessage(t *testing.T) {
	a := newTestApp(t)
	_, mb := defaultAdminUserAndMailbox(t, a)
	ctx := context.Background()
	sentFolderID, err := a.ensureFolder(ctx, mb.ID, "Sent")
	if err != nil {
		t.Fatal(err)
	}
	messageID := "<orphan-sent-dedupe@example.test>"
	if err := a.insertSentDedupeKey(ctx, mb.ID, sentFolderID, messageID); err != nil {
		t.Fatal(err)
	}
	now := a.now().UTC()
	sentID, inserted, err := a.insertSentMessageOnce(ctx, storedMessage{
		MailboxID:  mb.ID,
		MessageUID: newID("uid"),
		MessageID:  messageID,
		Subject:    "orphan dedupe",
		From:       mb.Address,
		To:         []string{"person@example.com"},
		SentAt:     now,
		ReceivedAt: now,
		BodyText:   "body",
		IsRead:     true,
	}, nil)
	if !errors.Is(err, errSentDedupeExists) {
		t.Fatalf("insertSentMessageOnce error=%v, want errSentDedupeExists", err)
	}
	if sentID != "" || inserted {
		t.Fatalf("sentID=%q inserted=%v, want empty false", sentID, inserted)
	}
	var count int
	if err := a.db.QueryRow(`SELECT COUNT(1) FROM messages WHERE mailbox_id=? AND folder_id=? AND message_id=?`, mb.ID, sentFolderID, messageID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("orphan dedupe should not create sent message, count=%d", count)
	}
}

func TestSubmissionRequeuesTerminalFailedDuplicateMessageID(t *testing.T) {
	a := newTestApp(t)
	a.updateConfig(func(cfg *Config) { cfg.SMTPHost = "127.0.0.1" })
	a.updateConfig(func(cfg *Config) { cfg.SMTPPort = "1" })
	user, mb, err := a.authenticateSubmission(context.Background(), "admin@lanqin.local", "ChangeMe123!")
	if err != nil {
		t.Fatal(err)
	}
	raw := "From: admin@lanqin.local\r\nTo: person@example.com\r\nSubject: requeue\r\nMessage-ID: <requeue@example.test>\r\n\r\nbody"
	if err := a.submitSMTPMessage(context.Background(), user, mb, mb.Address, []string{"person@example.com"}, strings.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`UPDATE send_queue SET status=?,attempt_count=max_attempts,next_attempt_at=?,last_error='terminal' WHERE mailbox_id=? AND message_id=?`, sendQueueStatusFailed, a.now().UTC().Add(time.Hour).Format(time.RFC3339Nano), mb.ID, "<requeue@example.test>"); err != nil {
		t.Fatal(err)
	}

	host, port, received := startCapturingSMTP(t, 1)
	a.updateConfig(func(cfg *Config) { cfg.SMTPHost = host })
	a.updateConfig(func(cfg *Config) { cfg.SMTPPort = port })
	if err := a.submitSMTPMessage(context.Background(), user, mb, mb.Address, []string{"person@example.com"}, strings.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	if err := a.processDueSendQueue(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("requeued terminal failure was not relayed")
	}
	var status string
	var attemptCount int
	if err := a.db.QueryRow(`SELECT status,attempt_count FROM send_queue WHERE mailbox_id=? AND message_id=?`, mb.ID, "<requeue@example.test>").Scan(&status, &attemptCount); err != nil {
		t.Fatal(err)
	}
	if status != sendQueueStatusDelivered || attemptCount != 1 {
		t.Fatalf("queue status=%q attempts=%d, want delivered attempts=1", status, attemptCount)
	}
}

func TestSubmissionRequeuesDeliveredDuplicateMessageID(t *testing.T) {
	a := newTestApp(t)
	host, port, received := startCapturingSMTP(t, 2)
	a.updateConfig(func(cfg *Config) { cfg.SMTPHost = host })
	a.updateConfig(func(cfg *Config) { cfg.SMTPPort = port })
	user, mb, err := a.authenticateSubmission(context.Background(), "admin@lanqin.local", "ChangeMe123!")
	if err != nil {
		t.Fatal(err)
	}
	raw := "From: admin@lanqin.local\r\nTo: person@example.com\r\nSubject: resend\r\nMessage-ID: <delivered-requeue@example.test>\r\n\r\nbody"
	if err := a.submitSMTPMessage(context.Background(), user, mb, mb.Address, []string{"person@example.com"}, strings.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	if err := a.processDueSendQueue(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("first delivery not received")
	}
	if err := a.submitSMTPMessage(context.Background(), user, mb, mb.Address, []string{"person@example.com"}, strings.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	if err := a.processDueSendQueue(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("requeued delivered message was not relayed")
	}
	var status string
	var attemptCount int
	if err := a.db.QueryRow(`SELECT status,attempt_count FROM send_queue WHERE mailbox_id=? AND message_id=?`, mb.ID, "<delivered-requeue@example.test>").Scan(&status, &attemptCount); err != nil {
		t.Fatal(err)
	}
	if status != sendQueueStatusDelivered || attemptCount != 1 {
		t.Fatalf("queue status=%q attempts=%d, want delivered attempts=1", status, attemptCount)
	}
}

func TestSubmissionRequeuesCanceledDuplicateMessageID(t *testing.T) {
	a := newTestApp(t)
	host, port, received := startCapturingSMTP(t, 1)
	a.updateConfig(func(cfg *Config) { cfg.SMTPHost = host })
	a.updateConfig(func(cfg *Config) { cfg.SMTPPort = port })
	user, mb, err := a.authenticateSubmission(context.Background(), "admin@lanqin.local", "ChangeMe123!")
	if err != nil {
		t.Fatal(err)
	}
	raw := "From: admin@lanqin.local\r\nTo: person@example.com\r\nSubject: canceled resend\r\nMessage-ID: <canceled-requeue@example.test>\r\n\r\nbody"
	if err := a.submitSMTPMessage(context.Background(), user, mb, mb.Address, []string{"person@example.com"}, strings.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`UPDATE send_queue SET status=?,updated_at=? WHERE mailbox_id=? AND message_id=?`, sendQueueStatusCanceled, a.now().UTC().Format(time.RFC3339Nano), mb.ID, "<canceled-requeue@example.test>"); err != nil {
		t.Fatal(err)
	}
	if err := a.submitSMTPMessage(context.Background(), user, mb, mb.Address, []string{"person@example.com"}, strings.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	if err := a.processDueSendQueue(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("requeued canceled message was not relayed")
	}
	var status string
	var attemptCount int
	if err := a.db.QueryRow(`SELECT status,attempt_count FROM send_queue WHERE mailbox_id=? AND message_id=?`, mb.ID, "<canceled-requeue@example.test>").Scan(&status, &attemptCount); err != nil {
		t.Fatal(err)
	}
	if status != sendQueueStatusDelivered || attemptCount != 1 {
		t.Fatalf("queue status=%q attempts=%d, want delivered attempts=1", status, attemptCount)
	}
}

func TestSubmissionAllowsAuthorizedAliasSendAs(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	if _, err := a.db.ExecContext(ctx, `INSERT INTO aliases(id,domain_id,source,destination,enabled,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, newID("als"), mustDefaultDomainID(t, a), "team@lanqin.local", "admin@lanqin.local", 1, a.now().UTC().Format(time.RFC3339Nano), a.now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	user, mb, err := a.authenticateSubmission(ctx, "admin@lanqin.local", "ChangeMe123!")
	if err != nil {
		t.Fatal(err)
	}
	raw := "From: Team <team@lanqin.local>\r\nTo: person@example.com\r\nSubject: alias send-as\r\nMessage-ID: <alias-send-as@example.test>\r\n\r\nbody"
	if err := a.submitSMTPMessage(ctx, user, mb, "team@lanqin.local", []string{"person@example.com"}, strings.NewReader(raw)); err != nil {
		t.Fatalf("authorized alias send-as should submit: %v", err)
	}
	sentFolderID, err := a.ensureFolder(ctx, mb.ID, "Sent")
	if err != nil {
		t.Fatal(err)
	}
	var fromAddr string
	if err := a.db.QueryRow(`SELECT from_addr FROM messages WHERE mailbox_id=? AND folder_id=? AND message_id=?`, mb.ID, sentFolderID, "<alias-send-as@example.test>").Scan(&fromAddr); err != nil {
		t.Fatal(err)
	}
	if fromAddr != "team@lanqin.local" {
		t.Fatalf("from_addr=%q, want alias", fromAddr)
	}
}

func TestSubmissionAllowsMultiDestinationAliasSendAs(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	if _, err := a.db.ExecContext(ctx, `INSERT INTO aliases(id,domain_id,source,destination,enabled,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, newID("als"), mustDefaultDomainID(t, a), "team-many@lanqin.local", "other@lanqin.local, admin@lanqin.local", 1, a.now().UTC().Format(time.RFC3339Nano), a.now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	user, mb, err := a.authenticateSubmission(ctx, "admin@lanqin.local", "ChangeMe123!")
	if err != nil {
		t.Fatal(err)
	}
	raw := "From: Team <team-many@lanqin.local>\r\nTo: person@example.com\r\nSubject: alias send-as\r\nMessage-ID: <multi-alias-send-as@example.test>\r\n\r\nbody"
	if err := a.submitSMTPMessage(ctx, user, mb, "team-many@lanqin.local", []string{"person@example.com"}, strings.NewReader(raw)); err != nil {
		t.Fatalf("authorized multi-destination alias send-as should submit: %v", err)
	}
}

func TestSubmissionAllowsExplicitSendAsGrant(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	user, mb, err := a.authenticateSubmission(ctx, "admin@lanqin.local", "ChangeMe123!")
	if err != nil {
		t.Fatal(err)
	}
	now := a.now().UTC().Format(time.RFC3339Nano)
	if _, err := a.db.ExecContext(ctx, `INSERT INTO send_as_grants(id,mailbox_id,address,display_name,enabled,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, newID("sag"), mb.ID, "support@example.com", "Support", 1, now, now); err != nil {
		t.Fatal(err)
	}
	raw := "From: Support <support@example.com>\r\nTo: person@example.com\r\nSubject: explicit send-as\r\nMessage-ID: <explicit-send-as@example.test>\r\n\r\nbody"
	if err := a.submitSMTPMessage(ctx, user, mb, "support@example.com", []string{"person@example.com"}, strings.NewReader(raw)); err != nil {
		t.Fatalf("explicit send-as grant should submit: %v", err)
	}
	sentFolderID, err := a.ensureFolder(ctx, mb.ID, "Sent")
	if err != nil {
		t.Fatal(err)
	}
	var fromAddr, fromName string
	if err := a.db.QueryRow(`SELECT from_addr,from_name FROM messages WHERE mailbox_id=? AND folder_id=? AND message_id=?`, mb.ID, sentFolderID, "<explicit-send-as@example.test>").Scan(&fromAddr, &fromName); err != nil {
		t.Fatal(err)
	}
	if fromAddr != "support@example.com" || fromName != "Support" {
		t.Fatalf("from=%q name=%q, want explicit grant", fromAddr, fromName)
	}
}

func TestSentMessageDedupeTableExists(t *testing.T) {
	a := newTestApp(t)
	var count int
	if err := a.db.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name='sent_message_dedupe_keys'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("sent message dedupe table count=%d, want 1", count)
	}
}

func TestSendQueueMessageIDMigrationDropsDuplicatesBeforeUniqueIndex(t *testing.T) {
	a := newTestApp(t)
	user, mb := defaultAdminUserAndMailbox(t, a)
	if _, err := a.db.Exec(`DROP INDEX IF EXISTS idx_send_queue_mailbox_source_message_id`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO send_queue(id,user_id,mailbox_id,sent_message_id,message_id,source,mail_from,header_from,recipients_json,mime_base64,status,next_attempt_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"dup_old", user.ID, mb.ID, "sent1", "<dup@example.test>", sendSourceSubmission, "admin@lanqin.local", "admin@lanqin.local", "[]", "bWVzc2FnZQ==", sendQueueStatusDelivered, a.now().UTC().Format(time.RFC3339Nano), "2026-06-24T00:00:00Z", "2026-06-24T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO send_queue(id,user_id,mailbox_id,sent_message_id,message_id,source,mail_from,header_from,recipients_json,mime_base64,status,next_attempt_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"dup_keep", user.ID, mb.ID, "sent2", "<dup@example.test>", sendSourceSubmission, "admin@lanqin.local", "admin@lanqin.local", "[]", "bWVzc2FnZQ==", sendQueueStatusQueued, a.now().UTC().Format(time.RFC3339Nano), "2026-06-24T00:01:00Z", "2026-06-24T00:01:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := a.migrateSendQueueMessageID(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := a.db.QueryRow(`SELECT COUNT(1) FROM send_queue WHERE mailbox_id=? AND source=? AND message_id='<dup@example.test>'`, mb.ID, sendSourceSubmission).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("duplicate queue rows count=%d, want 1", count)
	}
	var keptID string
	if err := a.db.QueryRow(`SELECT id FROM send_queue WHERE mailbox_id=? AND source=? AND message_id='<dup@example.test>'`, mb.ID, sendSourceSubmission).Scan(&keptID); err != nil {
		t.Fatal(err)
	}
	if keptID != "dup_keep" {
		t.Fatalf("kept queue id=%q, want dup_keep", keptID)
	}
}

func TestSubmissionTLSConfigRequiresCertificateFiles(t *testing.T) {
	a := newTestApp(t)
	a.updateConfig(func(cfg *Config) { cfg.SubmissionAddr = ":587" })
	a.updateConfig(func(cfg *Config) { cfg.SubmissionTLSAddr = ":465" })
	if _, err := LoadServerTLSConfig(a.config()); err == nil {
		t.Fatal("submission TLS config should require certificate files")
	}
}

func TestSubmissionTLSConfigReloadsCertificateFiles(t *testing.T) {
	a := newTestApp(t)
	certPath, keyPath := writeTestCertificateFiles(t, "first.example.test")
	a.updateConfig(func(cfg *Config) { cfg.TLSCertFile = certPath })
	a.updateConfig(func(cfg *Config) { cfg.TLSKeyFile = keyPath })
	tlsConfig, err := LoadServerTLSConfig(a.config())
	if err != nil {
		t.Fatal(err)
	}
	first, err := tlsConfig.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatal(err)
	}
	firstLeaf, err := x509.ParseCertificate(first.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	nextCertPath, nextKeyPath := writeTestCertificateFiles(t, "second.example.test")
	nextCert, err := os.ReadFile(nextCertPath)
	if err != nil {
		t.Fatal(err)
	}
	nextKey, err := os.ReadFile(nextKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath, nextCert, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, nextKey, 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := tlsConfig.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatal(err)
	}
	secondLeaf, err := x509.ParseCertificate(second.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if firstLeaf.Subject.CommonName != "first.example.test" || secondLeaf.Subject.CommonName != "second.example.test" {
		t.Fatalf("cert reload common names first=%q second=%q", firstLeaf.Subject.CommonName, secondLeaf.Subject.CommonName)
	}
}

func TestSubmissionLoginAuthenticationWithAndWithoutInitialResponse(t *testing.T) {
	a := newTestApp(t)
	for _, withInitialResponse := range []bool{false, true} {
		t.Run(fmt.Sprintf("initial-response-%t", withInitialResponse), func(t *testing.T) {
			session := &submissionSession{app: a}
			if mechanisms := strings.Join(session.AuthMechanisms(), " "); mechanisms != "PLAIN LOGIN" {
				t.Fatalf("submission auth mechanisms=%q", mechanisms)
			}
			server, err := session.Auth(sasl.Login)
			if err != nil {
				t.Fatal(err)
			}
			var response []byte
			if withInitialResponse {
				response = []byte("admin@lanqin.local")
			}
			challenge, done, err := server.Next(response)
			if err != nil || done {
				t.Fatalf("initial LOGIN response err=%v done=%t", err, done)
			}
			if !withInitialResponse {
				if string(challenge) != "Username:" {
					t.Fatalf("username challenge=%q", challenge)
				}
				challenge, done, err = server.Next([]byte("admin@lanqin.local"))
				if err != nil || done {
					t.Fatalf("username response err=%v done=%t", err, done)
				}
			}
			if string(challenge) != "Password:" {
				t.Fatalf("password challenge=%q", challenge)
			}
			challenge, done, err = server.Next([]byte("ChangeMe123!"))
			if err != nil || !done || challenge != nil {
				t.Fatalf("password response challenge=%q err=%v done=%t", challenge, err, done)
			}
			if session.user == nil || session.mailbox == nil {
				t.Fatal("LOGIN authentication did not populate submission session")
			}
		})
	}
}

func TestSubmissionServersAcceptStartTLSAndImplicitTLS(t *testing.T) {
	a := newTestApp(t)
	host, port, received := startCapturingSMTP(t, 2)
	a.updateConfig(func(cfg *Config) { cfg.SMTPHost = host })
	a.updateConfig(func(cfg *Config) { cfg.SMTPPort = port })
	certPath, keyPath := writeTestCertificateFiles(t, "mail.example.test")
	a.updateConfig(func(cfg *Config) { cfg.TLSCertFile = certPath })
	a.updateConfig(func(cfg *Config) { cfg.TLSKeyFile = keyPath })
	tlsConfig, err := LoadServerTLSConfig(a.config())
	if err != nil {
		t.Fatal(err)
	}

	startServer := func(t *testing.T, implicit bool) string {
		t.Helper()
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		srv := a.newSubmissionServer(ln.Addr().String(), tlsConfig)
		go func() {
			if implicit {
				_ = srv.Serve(tls.NewListener(ln, tlsConfig))
			} else {
				_ = srv.Serve(ln)
			}
		}()
		t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })
		return ln.Addr().String()
	}

	raw := "From: admin@lanqin.local\r\nTo: person@example.com\r\nSubject: starttls\r\nMessage-ID: <starttls@example.test>\r\n\r\nbody"
	addr := startServer(t, false)
	client, err := smtpclient.DialStartTLS(addr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Auth(sasl.NewLoginClient("admin@lanqin.local", "ChangeMe123!")); err != nil {
		t.Fatal(err)
	}
	if err := client.SendMail("admin@lanqin.local", []string{"person@example.com"}, strings.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	if err := a.processDueSendQueue(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("starttls relay not received")
	}

	raw = "From: admin@lanqin.local\r\nTo: person@example.com\r\nSubject: smtps\r\nMessage-ID: <smtps@example.test>\r\n\r\nbody"
	addr = startServer(t, true)
	client, err = smtpclient.DialTLS(addr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Auth(sasl.NewPlainClient("", "admin@lanqin.local", "ChangeMe123!")); err != nil {
		t.Fatal(err)
	}
	if err := client.SendMail("admin@lanqin.local", []string{"person@example.com"}, strings.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	if err := a.processDueSendQueue(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("implicit tls relay not received")
	}
}

func TestAdminSMTPTestEndpoint(t *testing.T) {
	a := newTestApp(t)
	host, port, received := startFakeSMTP(t)
	a.updateConfig(func(cfg *Config) { cfg.SMTPHost = host })
	a.updateConfig(func(cfg *Config) { cfg.SMTPPort = port })
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d body=%v", code, login)
	}

	var out map[string]any
	var templates struct {
		Items []MailTemplate `json:"items"`
	}
	if code := admin.do("GET", "/api/admin/mail-templates", nil, &templates); code != http.StatusOK || len(templates.Items) == 0 {
		t.Fatalf("templates code=%d items=%d", code, len(templates.Items))
	}
	var updated MailTemplate
	if code := admin.do("POST", "/api/admin/mail-templates/smtp_test", map[string]string{
		"subject":  "自定义 SMTP 测试",
		"bodyText": "hello {{to}} from {{from}}",
		"bodyHtml": "<p>hello {{to}} from {{from}}</p>",
	}, &updated); code != http.StatusOK || updated.Subject != "自定义 SMTP 测试" {
		t.Fatalf("update template code=%d template=%+v", code, updated)
	}
	if code := admin.do("POST", "/api/admin/settings/test-smtp", map[string]string{"to": "test@example.com"}, &out); code != http.StatusOK {
		t.Fatalf("smtp test code=%d body=%v", code, out)
	}
	select {
	case body := <-received:
		if !strings.Contains(body, "From: admin@lanqin.local") || !strings.Contains(body, "To: test@example.com") || !strings.Contains(body, "=?utf-8?q?=E8=87=AA=E5=AE=9A=E4=B9=89_SMTP_=E6=B5=8B=E8=AF=95?=") {
			t.Fatalf("unexpected smtp body: %s", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("smtp test message not received")
	}
}

func TestAuthPolicyDovecotResponseFormat(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	client := &testClient{t: t, server: ts}

	var allowed map[string]any
	if code := client.do("POST", "/auth-policy?command=allow", map[string]string{"login": "admin@lanqin.local", "protocol": "smtp"}, &allowed); code != http.StatusOK {
		t.Fatalf("auth policy allow code=%d body=%v", code, allowed)
	}
	if allowed["status"] != float64(0) {
		t.Fatalf("expected numeric allow status 0, got %#v", allowed["status"])
	}

	var denied map[string]any
	if code := client.do("POST", "/auth-policy?command=allow", map[string]string{"login": "missing@lanqin.local", "protocol": "imap"}, &denied); code != http.StatusOK {
		t.Fatalf("auth policy deny code=%d body=%v", code, denied)
	}
	if denied["status"] != float64(-1) {
		t.Fatalf("expected numeric deny status -1, got %#v", denied["status"])
	}
}

func TestProfileAndPasswordUpdate(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	client := &testClient{t: t, server: ts}

	var login map[string]any
	if code := client.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d body=%v", code, login)
	}

	var profile struct {
		User User `json:"user"`
	}
	if code := client.do("POST", "/api/me/profile", map[string]string{"displayName": "蓝钦管理员"}, &profile); code != http.StatusOK || profile.User.DisplayName != "蓝钦管理员" {
		t.Fatalf("profile code=%d user=%+v", code, profile.User)
	}

	var ok map[string]any
	if code := client.do("POST", "/api/me/password", map[string]string{"currentPassword": "wrong", "newPassword": "NewPassword123!"}, &ok); code != http.StatusUnauthorized {
		t.Fatalf("wrong password change code=%d", code)
	}
	if code := client.do("POST", "/api/me/password", map[string]string{"currentPassword": "ChangeMe123!", "newPassword": "NewPassword123!"}, &ok); code != http.StatusOK {
		t.Fatalf("password change code=%d body=%v", code, ok)
	}

	fresh := &testClient{t: t, server: ts}
	if code := fresh.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, nil); code != http.StatusUnauthorized {
		t.Fatalf("old password login code=%d", code)
	}
	if code := fresh.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "NewPassword123!"}, &login); code != http.StatusOK {
		t.Fatalf("new password login code=%d", code)
	}
}

func TestUserMailSignaturesDefaultResolution(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("admin login code=%d body=%v", code, login)
	}
	domainID := mustDefaultDomainID(t, a)
	mb1 := createTestMailbox(t, admin, domainID, "signer", "Signer", "Password123!", nil)
	mb2 := createTestMailbox(t, admin, domainID, "second", "Second", "Password123!", map[string]any{"ownerEmail": mb1.Address})

	user := &testClient{t: t, server: ts}
	if code := user.do("POST", "/api/auth/login", map[string]string{"email": mb1.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("user login code=%d", code)
	}
	var global MailSignature
	if code := user.do("POST", "/api/me/signatures", map[string]any{"name": "全局签名", "content": "Global Sig", "isDefault": true}, &global); code != http.StatusCreated || !global.IsDefault || global.MailboxID != "" {
		t.Fatalf("create global signature code=%d sig=%+v", code, global)
	}
	var bound MailSignature
	if code := user.do("POST", "/api/me/signatures", map[string]any{"mailboxId": mb1.ID, "name": "邮箱签名", "content": "Mailbox Sig", "isDefault": true}, &bound); code != http.StatusCreated || !bound.IsDefault || bound.MailboxID != mb1.ID {
		t.Fatalf("create bound signature code=%d sig=%+v", code, bound)
	}
	var defaultResp struct {
		Signature *MailSignature `json:"signature"`
	}
	if code := user.do("GET", "/api/me/signatures/default?mailboxId="+mb1.ID, nil, &defaultResp); code != http.StatusOK || defaultResp.Signature == nil || defaultResp.Signature.ID != bound.ID {
		t.Fatalf("bound default code=%d resp=%+v", code, defaultResp)
	}
	if code := user.do("GET", "/api/me/signatures/default?mailboxId="+mb2.ID, nil, &defaultResp); code != http.StatusOK || defaultResp.Signature == nil || defaultResp.Signature.ID != global.ID {
		t.Fatalf("global fallback code=%d resp=%+v", code, defaultResp)
	}
	var updated MailSignature
	if code := user.do("POST", "/api/me/signatures/"+bound.ID, map[string]any{"mailboxId": mb1.ID, "name": "更新签名", "content": "Updated Sig", "isDefault": false}, &updated); code != http.StatusOK || updated.IsDefault || updated.Content != "Updated Sig" {
		t.Fatalf("update signature code=%d sig=%+v", code, updated)
	}
	if code := user.do("GET", "/api/me/signatures/default?mailboxId="+mb1.ID, nil, &defaultResp); code != http.StatusOK || defaultResp.Signature == nil || defaultResp.Signature.ID != global.ID {
		t.Fatalf("fallback after update code=%d resp=%+v", code, defaultResp)
	}
	var ok map[string]any
	if code := user.do("DELETE", "/api/me/signatures/"+global.ID, nil, &ok); code != http.StatusOK {
		t.Fatalf("delete signature code=%d body=%v", code, ok)
	}
	if code := user.do("GET", "/api/me/signatures/default?mailboxId="+mb2.ID, nil, &defaultResp); code != http.StatusOK || defaultResp.Signature != nil {
		t.Fatalf("empty default code=%d resp=%+v", code, defaultResp)
	}
}

func TestUserTwoFactorSetupAndLogin(t *testing.T) {
	a := newTestApp(t)
	a.updateConfig(func(cfg *Config) { cfg.TwoFactorEnabled = true })
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	client := &testClient{t: t, server: ts}

	var login map[string]any
	if code := client.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d body=%v", code, login)
	}

	var setup struct {
		Secret     string `json:"secret"`
		OtpauthURL string `json:"otpauthUrl"`
	}
	if code := client.do("POST", "/api/me/2fa/setup", map[string]string{}, &setup); code != http.StatusOK || setup.Secret == "" || !strings.HasPrefix(setup.OtpauthURL, "otpauth://totp/") {
		t.Fatalf("setup code=%d setup=%+v", code, setup)
	}

	var out map[string]any
	if code := client.do("POST", "/api/me/2fa/enable", map[string]string{"code": "000000"}, &out); code != http.StatusUnauthorized {
		t.Fatalf("wrong enable code=%d body=%v", code, out)
	}
	code, err := generateTOTP(setup.Secret, a.now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	var enabled struct {
		User          User     `json:"user"`
		RecoveryCodes []string `json:"recoveryCodes"`
	}
	if status := client.do("POST", "/api/me/2fa/enable", map[string]string{"code": code}, &enabled); status != http.StatusOK || !enabled.User.TwoFactorEnabled {
		t.Fatalf("enable status=%d user=%+v", status, enabled.User)
	}
	if len(enabled.RecoveryCodes) != 8 {
		t.Fatalf("recovery codes=%+v", enabled.RecoveryCodes)
	}

	fresh := &testClient{t: t, server: ts}
	var challenge struct {
		TwoFactorRequired bool   `json:"twoFactorRequired"`
		ChallengeToken    string `json:"challengeToken"`
	}
	if status := fresh.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &challenge); status != http.StatusOK || !challenge.TwoFactorRequired || challenge.ChallengeToken == "" || fresh.cookie != nil {
		t.Fatalf("challenge status=%d challenge=%+v cookie=%v", status, challenge, fresh.cookie)
	}
	if status := fresh.do("POST", "/api/auth/login", map[string]string{"challengeToken": challenge.ChallengeToken, "twoFactorCode": "000000"}, &out); status != http.StatusUnauthorized {
		t.Fatalf("wrong challenge status=%d body=%v", status, out)
	}
	if status := fresh.do("POST", "/api/auth/login", map[string]string{"challengeToken": challenge.ChallengeToken, "twoFactorCode": enabled.RecoveryCodes[0]}, &login); status != http.StatusOK || fresh.cookie == nil {
		t.Fatalf("recovery login status=%d body=%v cookie=%v", status, login, fresh.cookie)
	}
	reused := &testClient{t: t, server: ts}
	if status := reused.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &challenge); status != http.StatusOK || !challenge.TwoFactorRequired || challenge.ChallengeToken == "" {
		t.Fatalf("reused challenge status=%d challenge=%+v", status, challenge)
	}
	if status := reused.do("POST", "/api/auth/login", map[string]string{"challengeToken": challenge.ChallengeToken, "twoFactorCode": enabled.RecoveryCodes[0]}, &out); status != http.StatusUnauthorized {
		t.Fatalf("reused recovery status=%d body=%v", status, out)
	}
	totpClient := &testClient{t: t, server: ts}
	if status := totpClient.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &challenge); status != http.StatusOK || !challenge.TwoFactorRequired || challenge.ChallengeToken == "" {
		t.Fatalf("totp challenge status=%d challenge=%+v", status, challenge)
	}
	code, err = generateTOTP(setup.Secret, a.now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if status := totpClient.do("POST", "/api/auth/login", map[string]string{"challengeToken": challenge.ChallengeToken, "twoFactorCode": code}, &login); status != http.StatusOK || totpClient.cookie == nil {
		t.Fatalf("2fa login status=%d body=%v cookie=%v", status, login, totpClient.cookie)
	}
	if status := totpClient.do("POST", "/api/me/2fa/disable", map[string]string{"code": code}, &enabled); status != http.StatusOK || enabled.User.TwoFactorEnabled {
		t.Fatalf("disable status=%d user=%+v", status, enabled.User)
	}
}

func TestDNSRecords(t *testing.T) {
	a := newTestApp(t)
	var domainID string
	if err := a.db.QueryRowContext(context.Background(), `SELECT id FROM domains WHERE name=?`, "lanqin.local").Scan(&domainID); err != nil {
		t.Fatal(err)
	}
	d, err := a.domainByID(context.Background(), domainID)
	if err != nil {
		t.Fatal(err)
	}
	records := a.dnsRecordsFor(d)
	if len(records) != 4 {
		t.Fatalf("records=%d", len(records))
	}
	if records[0].Type != "MX" || !strings.Contains(records[2].Value, "v=DKIM1") {
		t.Fatalf("unexpected records: %+v", records)
	}
}

func TestDefaultMailLabelsBackfillOrderAndDeletion(t *testing.T) {
	a := newTestApp(t)
	var mailboxID string
	if err := a.db.QueryRow(`SELECT id FROM mailboxes WHERE address='admin@lanqin.local'`).Scan(&mailboxID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`DELETE FROM system_settings WHERE key='defaultMailLabelsInitialized'`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`DELETE FROM mail_labels WHERE mailbox_id=?`, mailboxID); err != nil {
		t.Fatal(err)
	}
	if err := a.migrateDefaultMailLabels(context.Background()); err != nil {
		t.Fatal(err)
	}
	labels, err := a.labelsForMailbox(context.Background(), mailboxID)
	if err != nil {
		t.Fatal(err)
	}
	defaults := defaultMailLabelDefs()
	if len(labels) != len(defaults) {
		t.Fatalf("labels=%+v", labels)
	}
	for index, expected := range defaults {
		if labels[index].Name != expected.name || labels[index].Color != expected.color {
			t.Fatalf("label %d=%+v want name=%q color=%q", index, labels[index], expected.name, expected.color)
		}
	}
	if _, err := a.db.Exec(`DELETE FROM mail_labels WHERE id=?`, labels[1].ID); err != nil {
		t.Fatal(err)
	}
	if err := a.migrateDefaultMailLabels(context.Background()); err != nil {
		t.Fatal(err)
	}
	labels, err = a.labelsForMailbox(context.Background(), mailboxID)
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) != len(defaults)-1 {
		t.Fatalf("deleted default label was restored: %+v", labels)
	}
}

func TestCheckDKIMRecordRequiresMatchingPublicKey(t *testing.T) {
	tests := []struct {
		name    string
		records []string
		key     string
		ok      bool
		message string
	}{
		{name: "matching", records: []string{"v=DKIM1; k=rsa; p=ABC123"}, key: "ABC123", ok: true, message: "DKIM 公钥匹配"},
		{name: "split whitespace", records: []string{"v=DKIM1; k=rsa; p=ABC 123\n456"}, key: "ABC123456", ok: true, message: "DKIM 公钥匹配"},
		{name: "wrong key", records: []string{"v=DKIM1; k=rsa; p=WRONG"}, key: "EXPECTED", ok: false, message: "DKIM 公钥与后台生成的记录不一致"},
		{name: "unrelated TXT", records: []string{"google-site-verification=token"}, key: "EXPECTED", ok: false, message: "未找到 DKIM 记录"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := checkDKIMRecord(tt.records, tt.key)
			if status.OK != tt.ok || status.Message != tt.message {
				t.Fatalf("status=%+v", status)
			}
		})
	}
}

func TestFixedRolesProtectAdminRoutesAndDefaultAdmin(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("admin login code=%d body=%v", code, login)
	}

	var groups struct {
		Items []PermissionGroup `json:"items"`
	}
	if code := admin.do("GET", "/api/admin/permission-groups", nil, &groups); code != http.StatusOK || len(groups.Items) != len(defaultPermissionGroups()) {
		t.Fatalf("fixed permission groups code=%d groups=%+v", code, groups.Items)
	}
	groupByID := map[string]PermissionGroup{}
	for _, group := range groups.Items {
		groupByID[group.ID] = group
	}
	for _, group := range defaultPermissionGroups() {
		if _, ok := groupByID[group.ID]; !ok {
			t.Fatalf("missing fixed permission group %s in %+v", group.ID, groups.Items)
		}
	}
	if groupByID[PermissionGroupRegular].Limits != defaultPermissionLimits() {
		t.Fatalf("regular group limits=%+v want %+v", groupByID[PermissionGroupRegular].Limits, defaultPermissionLimits())
	}
	if groups.Items[0].ID != PermissionGroupSuperAdmin || groups.Items[1].ID != PermissionGroupRegular {
		t.Fatalf("unexpected fixed permission groups: %+v", groups.Items)
	}

	var errBody map[string]any
	var users struct {
		Items []AdminUser `json:"items"`
	}
	var customGroup PermissionGroup
	if code := admin.do("POST", "/api/admin/permission-groups", map[string]any{
		"name":        "Mailbox Viewers",
		"description": "Can view mailboxes only",
		"permissions": []string{PermissionAdminOverview, PermissionMailboxesView},
		"limits":      PermissionLimits{MaxAttachmentMB: 5, MaxMailboxCount: 4, SMTPDailyLimit: 8, SMTPMinuteLimit: 2, IMAPMinuteLimit: 5, POP3MinuteLimit: 3},
	}, &customGroup); code != http.StatusCreated {
		t.Fatalf("custom permission group creation code=%d group=%+v", code, customGroup)
	}
	if customGroup.Limits.MaxAttachmentMB != 5 || customGroup.Limits.MaxMailboxCount != 4 || customGroup.Limits.SMTPDailyLimit != 8 || customGroup.Limits.SMTPMinuteLimit != 2 || customGroup.Limits.IMAPMinuteLimit != 5 || customGroup.Limits.POP3MinuteLimit != 3 {
		t.Fatalf("custom permission group limits=%+v", customGroup.Limits)
	}
	if customGroup.System || customGroup.ID == "" || !userHasPermission(&User{Role: "user", Permissions: customGroup.Permissions}, PermissionMailboxesView) || userHasPermission(&User{Role: "user", Permissions: customGroup.Permissions}, PermissionMailboxesCreate) {
		t.Fatalf("custom permission group permissions=%+v", customGroup)
	}
	if code := admin.do("POST", "/api/admin/permission-groups/"+PermissionGroupSuperAdmin, map[string]any{
		"name":        "Changed",
		"description": "Should not change",
		"permissions": []string{PermissionMailboxesView},
	}, &errBody); code != http.StatusForbidden {
		t.Fatalf("system permission group update should be forbidden code=%d body=%v", code, errBody)
	}
	var updatedRegular PermissionGroup
	if code := admin.do("POST", "/api/admin/permission-groups/"+PermissionGroupRegular, map[string]any{
		"name":        "Changed Regular",
		"description": "Should not change",
		"permissions": regularUserDefaultPermissions(),
		"limits":      defaultPermissionLimits(),
	}, &updatedRegular); code != http.StatusOK {
		t.Fatalf("regular system permission group update code=%d group=%+v", code, updatedRegular)
	}
	if updatedRegular.Name != "普通用户" || updatedRegular.Description != "仅可使用自己的邮箱功能，不包含后台权限。" {
		t.Fatalf("regular system permission group identity changed: %+v", updatedRegular)
	}
	regularGroup, err := a.permissionGroupByID(context.Background(), PermissionGroupRegular)
	if err != nil {
		t.Fatal(err)
	}
	if !regularGroup.System || !userHasPermission(&User{Role: "user", Permissions: regularGroup.Permissions}, PermissionMailAccess) || userHasPermission(&User{Role: "user", Permissions: regularGroup.Permissions}, PermissionAdminOverview) {
		t.Fatalf("regular group should retain the saved default permissions=%+v", regularGroup)
	}
	if code := admin.do("DELETE", "/api/admin/permission-groups/"+PermissionGroupSuperAdmin, nil, &errBody); code != http.StatusForbidden {
		t.Fatalf("system permission group delete should be forbidden code=%d body=%v", code, errBody)
	}
	if code := admin.do("DELETE", "/api/admin/permission-groups/"+PermissionGroupRegular, nil, &errBody); code != http.StatusForbidden {
		t.Fatalf("regular user group delete should be forbidden code=%d body=%v", code, errBody)
	}
	if code := admin.do("POST", "/api/admin/users", map[string]any{
		"email":              "invalid-group@lanqin.local",
		"displayName":        "Invalid Group",
		"role":               "user",
		"password":           "Password123!",
		"disabled":           false,
		"permissionGroupIds": []string{PermissionGroupSuperAdmin},
	}, &errBody); code != http.StatusBadRequest {
		t.Fatalf("assigning super admin group should be rejected code=%d body=%v", code, errBody)
	}

	var mailboxAdminGroup PermissionGroup
	if code := admin.do("POST", "/api/admin/permission-groups", map[string]any{
		"name":        "Mailbox Admins",
		"description": "Can manage mailboxes",
		"permissions": []string{
			PermissionAdminOverview,
			PermissionUsersView,
			PermissionDomainsView,
			PermissionMailboxesView,
			PermissionMailboxesCreate,
			PermissionMailboxesUpdate,
			PermissionMailboxesDelete,
		},
	}, &mailboxAdminGroup); code != http.StatusCreated {
		t.Fatalf("create mailbox admin group code=%d group=%+v", code, mailboxAdminGroup)
	}

	var userAdminGroup PermissionGroup
	if code := admin.do("POST", "/api/admin/permission-groups", map[string]any{
		"name":        "User Admins",
		"description": "Can manage users",
		"permissions": []string{
			PermissionAdminOverview,
			PermissionUsersView,
			PermissionUsersCreate,
			PermissionUsersUpdate,
			PermissionUsersDelete,
			PermissionUsersResetPassword,
			PermissionGroupsView,
		},
	}, &userAdminGroup); code != http.StatusCreated {
		t.Fatalf("create user admin group code=%d group=%+v", code, userAdminGroup)
	}

	var mailboxUser AdminUser
	if code := admin.do("POST", "/api/admin/users", map[string]any{
		"email":              "mailbox-admin@lanqin.local",
		"displayName":        "Mailbox Admin",
		"role":               "user",
		"password":           "Password123!",
		"disabled":           false,
		"permissionGroupIds": []string{mailboxAdminGroup.ID},
	}, &mailboxUser); code != http.StatusCreated {
		t.Fatalf("create mailbox admin user code=%d user=%+v", code, mailboxUser)
	}
	if mailboxUser.Role != "user" || !containsString(mailboxUser.PermissionGroupIDs, PermissionGroupRegular) || !containsString(mailboxUser.PermissionGroupIDs, mailboxAdminGroup.ID) || !userHasPermission(&mailboxUser.User, PermissionMailboxesManage) || userHasPermission(&mailboxUser.User, PermissionSystemSettings) {
		t.Fatalf("mailbox admin authorization=%+v", mailboxUser.User)
	}

	var plainUser AdminUser
	if code := admin.do("POST", "/api/admin/users", map[string]any{
		"email":              "plain-user@lanqin.local",
		"displayName":        "Plain User",
		"role":               "user",
		"password":           "Password123!",
		"disabled":           false,
		"permissionGroupIds": []string{},
	}, &plainUser); code != http.StatusCreated {
		t.Fatalf("create plain user code=%d user=%+v", code, plainUser)
	}
	if len(plainUser.PermissionGroupIDs) != 1 || plainUser.PermissionGroupIDs[0] != PermissionGroupRegular || !userHasPermission(&plainUser.User, PermissionMailAccess) || userHasPermission(&plainUser.User, PermissionAdminOverview) {
		t.Fatalf("plain user should inherit regular permissions: %+v", plainUser.User)
	}

	var customUser AdminUser
	if code := admin.do("POST", "/api/admin/users", map[string]any{
		"email":              "mailbox-viewer@lanqin.local",
		"displayName":        "Mailbox Viewer",
		"role":               "user",
		"password":           "Password123!",
		"disabled":           false,
		"permissionGroupIds": []string{customGroup.ID},
	}, &customUser); code != http.StatusCreated {
		t.Fatalf("create custom group user code=%d user=%+v", code, customUser)
	}
	if !userHasPermission(&customUser.User, PermissionMailboxesView) || userHasPermission(&customUser.User, PermissionMailboxesCreate) {
		t.Fatalf("custom group user authorization=%+v", customUser.User)
	}
	if code := admin.do("DELETE", "/api/admin/permission-groups/"+customGroup.ID, nil, &errBody); code != http.StatusBadRequest {
		t.Fatalf("assigned custom permission group delete should be rejected code=%d body=%v", code, errBody)
	}

	mailboxAdmin := &testClient{t: t, server: ts}
	if code := mailboxAdmin.do("POST", "/api/auth/login", map[string]string{"email": "mailbox-admin@lanqin.local", "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("mailbox admin login code=%d", code)
	}
	var mailboxList struct {
		Items []Mailbox `json:"items"`
	}
	if code := mailboxAdmin.do("GET", "/api/admin/mailboxes", nil, &mailboxList); code != http.StatusOK {
		t.Fatalf("mailbox admin should access mailboxes code=%d", code)
	}
	if code := mailboxAdmin.do("GET", "/api/admin/settings", nil, &errBody); code != http.StatusForbidden {
		t.Fatalf("mailbox admin settings should be forbidden code=%d body=%v", code, errBody)
	}
	if code := mailboxAdmin.do("GET", "/api/admin/users", nil, &errBody); code != http.StatusOK {
		t.Fatalf("mailbox admin should read users for mailbox ownership code=%d body=%v", code, errBody)
	}
	viewer := &testClient{t: t, server: ts}
	if code := viewer.do("POST", "/api/auth/login", map[string]string{"email": "mailbox-viewer@lanqin.local", "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("mailbox viewer login code=%d", code)
	}
	if code := viewer.do("GET", "/api/admin/mailboxes", nil, &mailboxList); code != http.StatusOK {
		t.Fatalf("mailbox viewer should read mailboxes code=%d", code)
	}
	if code := viewer.do("POST", "/api/admin/mailboxes", map[string]any{
		"domainId":    mustDefaultDomainID(t, a),
		"localPart":   "blocked-create",
		"displayName": "Blocked Create",
		"password":    "Password123!",
		"quotaMb":     1024,
		"role":        "user",
	}, &errBody); code != http.StatusForbidden {
		t.Fatalf("mailbox viewer should not create mailboxes code=%d body=%v", code, errBody)
	}
	if code := mailboxAdmin.do("POST", "/api/admin/users", map[string]any{
		"email":              "blocked-by-mailbox-admin@lanqin.local",
		"displayName":        "Blocked",
		"role":               "user",
		"password":           "Password123!",
		"disabled":           false,
		"permissionGroupIds": []string{mailboxAdminGroup.ID},
	}, &errBody); code != http.StatusForbidden {
		t.Fatalf("mailbox admin should not create users code=%d body=%v", code, errBody)
	}

	var userManager AdminUser
	if code := admin.do("POST", "/api/admin/users", map[string]any{
		"email":              "user-admin@lanqin.local",
		"displayName":        "User Admin",
		"role":               "user",
		"password":           "Password123!",
		"disabled":           false,
		"permissionGroupIds": []string{userAdminGroup.ID},
	}, &userManager); code != http.StatusCreated {
		t.Fatalf("create user admin code=%d user=%+v", code, userManager)
	}
	userAdmin := &testClient{t: t, server: ts}
	if code := userAdmin.do("POST", "/api/auth/login", map[string]string{"email": "user-admin@lanqin.local", "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("user admin login code=%d", code)
	}
	if code := userAdmin.do("GET", "/api/admin/users", nil, &users); code != http.StatusOK {
		t.Fatalf("user admin users code=%d body=%v", code, users)
	}
	if code := userAdmin.do("POST", "/api/admin/users", map[string]any{
		"email":              "delegated-mailbox@lanqin.local",
		"displayName":        "Delegated Mailbox",
		"role":               "user",
		"password":           "Password123!",
		"disabled":           false,
		"permissionGroupIds": []string{mailboxAdminGroup.ID},
	}, &errBody); code != http.StatusBadRequest {
		t.Fatalf("user admin should not assign mailbox admin group code=%d body=%v", code, errBody)
	}
	var regularUser AdminUser
	if code := userAdmin.do("POST", "/api/admin/users", map[string]any{
		"email":              "delegated-user@lanqin.local",
		"displayName":        "Delegated User",
		"role":               "user",
		"password":           "Password123!",
		"disabled":           false,
		"permissionGroupIds": []string{userAdminGroup.ID},
	}, &regularUser); code != http.StatusCreated {
		t.Fatalf("user admin should assign own group code=%d user=%+v", code, regularUser)
	}
	if code := userAdmin.do("POST", "/api/admin/users", map[string]any{
		"email":              "delegated-super@lanqin.local",
		"displayName":        "Delegated Super",
		"role":               "admin",
		"password":           "Password123!",
		"disabled":           false,
		"permissionGroupIds": []string{},
	}, &errBody); code != http.StatusForbidden {
		t.Fatalf("user admin should not create super admin code=%d body=%v", code, errBody)
	}

	if code := admin.do("GET", "/api/admin/users", nil, &users); code != http.StatusOK || len(users.Items) == 0 {
		t.Fatalf("admin users code=%d items=%d", code, len(users.Items))
	}
	var defaultAdmin AdminUser
	for _, user := range users.Items {
		if user.Email == "admin@lanqin.local" {
			defaultAdmin = user
			break
		}
	}
	if defaultAdmin.ID == "" || !defaultAdmin.Protected || defaultAdmin.Role != "admin" {
		t.Fatalf("default admin should be protected super admin: %+v", defaultAdmin.User)
	}
	if code := admin.do("POST", "/api/admin/users/"+defaultAdmin.ID, map[string]any{
		"displayName": "LanQin Admin",
		"role":        "user",
		"disabled":    false,
	}, &errBody); code != http.StatusBadRequest {
		t.Fatalf("default admin downgrade should be rejected code=%d body=%v", code, errBody)
	}
	if code := admin.do("POST", "/api/admin/users/"+defaultAdmin.ID, map[string]any{
		"displayName": "LanQin Admin",
		"role":        "admin",
		"disabled":    true,
	}, &errBody); code != http.StatusBadRequest {
		t.Fatalf("default admin disable should be rejected code=%d body=%v", code, errBody)
	}
	if code := admin.do("DELETE", "/api/admin/users/"+defaultAdmin.ID, nil, &errBody); code != http.StatusBadRequest {
		t.Fatalf("default admin delete should be rejected code=%d body=%v", code, errBody)
	}
}

func TestLegacySystemPermissionGroupsAreCleanedUp(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	legacyIDs := []string{
		"pg_permission_manager",
		"pg_user_manager",
		"pg_system_operator",
		"pg_mail_operator",
	}

	for _, groupID := range legacyIDs {
		if _, err := a.db.ExecContext(ctx, `INSERT INTO permission_groups(id,name,description,permissions_json,system,created_at,updated_at)
			VALUES(?,?,?,?,1,?,?)`, groupID, "Legacy "+groupID, "", "[]", now, now); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.ensureDefaultPermissionGroups(ctx); err != nil {
		t.Fatal(err)
	}
	for _, groupID := range legacyIDs {
		var count int
		if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM permission_groups WHERE id=?`, groupID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("legacy permission group %s was not removed", groupID)
		}
	}
}

func TestRegularUserMailPermissionsAreEnforced(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}

	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("admin login code=%d body=%v", code, login)
	}
	mb := createTestMailbox(t, admin, mustDefaultDomainID(t, a), "front-perm", "Front Permissions", "Password123!", nil)

	user := &testClient{t: t, server: ts}
	if code := user.do("POST", "/api/auth/login", map[string]string{"email": mb.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("user login code=%d", code)
	}
	var mine struct {
		Items []Mailbox `json:"items"`
	}
	if code := user.do("GET", "/api/mail/mailboxes", nil, &mine); code != http.StatusOK || len(mine.Items) != 1 || mine.Items[0].ID != mb.ID {
		t.Fatalf("regular user should access mail front code=%d items=%+v", code, mine.Items)
	}
	var errBody map[string]any
	if code := user.do("GET", "/api/admin/overview", nil, &errBody); code != http.StatusForbidden {
		t.Fatalf("regular mail permissions should not grant admin access code=%d body=%v", code, errBody)
	}

	setRegularPermissionGroupForTest(t, a, withoutPermissions(regularUserDefaultPermissions(), PermissionMailAccess), defaultPermissionLimits())
	noAccess := &testClient{t: t, server: ts}
	if code := noAccess.do("POST", "/api/auth/login", map[string]string{"email": mb.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("no access login code=%d", code)
	}
	if code := noAccess.do("GET", "/api/mail/mailboxes", nil, &errBody); code != http.StatusForbidden {
		t.Fatalf("missing mail access should block mailbox list code=%d body=%v", code, errBody)
	}

	setRegularPermissionGroupForTest(t, a, withoutPermissions(regularUserDefaultPermissions(), PermissionMailSend), defaultPermissionLimits())
	noSend := &testClient{t: t, server: ts}
	if code := noSend.do("POST", "/api/auth/login", map[string]string{"email": mb.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("no send login code=%d", code)
	}
	sendPayload := map[string]any{
		"mailboxId": mb.ID,
		"to":        []string{"someone@example.test"},
		"subject":   "blocked send",
		"text":      "body",
		"html":      "<p>body</p>",
	}
	if code := noSend.do("POST", "/api/mail/send", sendPayload, &errBody); code != http.StatusForbidden {
		t.Fatalf("missing send permission should block send code=%d body=%v", code, errBody)
	}
	schedulePayload := map[string]any{
		"mailboxId": mb.ID,
		"to":        []string{"someone@example.test"},
		"subject":   "blocked schedule",
		"text":      "body",
		"html":      "<p>body</p>",
		"sendAt":    time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339Nano),
	}
	if code := noSend.do("POST", "/api/mail/schedule-send", schedulePayload, &errBody); code != http.StatusForbidden {
		t.Fatalf("missing send permission should block scheduled send creation code=%d body=%v", code, errBody)
	}
	if code := noSend.do("GET", "/api/mail/scheduled-sends?mailboxId="+mb.ID, nil, &struct {
		Items []ScheduledSend `json:"items"`
	}{}); code != http.StatusOK {
		t.Fatalf("schedule management permission should remain usable code=%d", code)
	}
}

func TestMaildirSyncImportsRFC822(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	root := t.TempDir()
	a.updateConfig(func(cfg *Config) { cfg.MaildirRoot = root })
	var domainID string
	if err := a.db.QueryRowContext(ctx, `SELECT id FROM domains WHERE name=?`, "lanqin.local").Scan(&domainID); err != nil {
		t.Fatal(err)
	}
	adminUser, _, err := a.userByEmail(ctx, "admin@lanqin.local")
	if err != nil {
		t.Fatal(err)
	}
	// seed() already created mailbox admin@lanqin.local
	var mailboxID string
	if err := a.db.QueryRowContext(ctx, `SELECT id FROM mailboxes WHERE user_id=? AND address=?`, adminUser.ID, "admin@lanqin.local").Scan(&mailboxID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.ExecContext(ctx, `DELETE FROM messages WHERE mailbox_id=?`, mailboxID); err != nil {
		t.Fatal(err)
	}

	mailboxes, err := a.maildirMailboxes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var admin maildirMailbox
	for _, mb := range mailboxes {
		if mb.Address == "admin@lanqin.local" {
			admin = mb
			break
		}
	}
	if admin.ID == "" {
		t.Fatal("admin mailbox not found")
	}

	dir := filepath.Join(root, admin.Domain, admin.LocalPart, "Maildir", "new")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := strings.Join([]string{
		"From: sender@example.test",
		"To: admin@lanqin.local",
		"Subject: Maildir import test",
		"Message-Id: <maildir-import@example.test>",
		"Date: Sat, 13 Jun 2026 13:00:00 +0000",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"hello from maildir",
	}, "\r\n")
	if err := os.WriteFile(filepath.Join(dir, "1749819600.M1P1.test"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	count, err := a.syncMaildirOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("imported=%d, want 1", count)
	}
	count, err = a.syncMaildirOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("second import=%d, want duplicate skip", count)
	}

	var subject, body string
	err = a.db.QueryRow(`SELECT subject, body_text FROM messages WHERE mailbox_id=? AND message_id='<maildir-import@example.test>'`, admin.ID).Scan(&subject, &body)
	if err != nil {
		t.Fatal(err)
	}
	if subject != "Maildir import test" || !strings.Contains(body, "hello from maildir") {
		t.Fatalf("unexpected imported message subject=%q body=%q", subject, body)
	}
}

func TestMaildirImportStoresAuthenticationResults(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	root := t.TempDir()
	a.updateConfig(func(cfg *Config) { cfg.MaildirRoot = root })
	ts := httptest.NewServer(a.Router())
	defer ts.Close()

	adminUser, _, err := a.userByEmail(ctx, "admin@lanqin.local")
	if err != nil {
		t.Fatal(err)
	}
	var mailboxID string
	if err := a.db.QueryRowContext(ctx, `SELECT id FROM mailboxes WHERE user_id=? AND address=?`, adminUser.ID, "admin@lanqin.local").Scan(&mailboxID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.ExecContext(ctx, `DELETE FROM messages WHERE mailbox_id=?`, mailboxID); err != nil {
		t.Fatal(err)
	}

	mailboxes, err := a.maildirMailboxes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var admin maildirMailbox
	for _, mb := range mailboxes {
		if mb.Address == "admin@lanqin.local" {
			admin = mb
			break
		}
	}
	if admin.ID == "" {
		t.Fatal("admin mailbox not found")
	}
	dir := filepath.Join(root, admin.Domain, admin.LocalPart, "Maildir", "new")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := strings.Join([]string{
		"From: sender@example.test",
		"To: admin@lanqin.local",
		"Subject: auth import test",
		"Message-Id: <auth-import@example.test>",
		"Date: Sat, 13 Jun 2026 13:00:00 +0000",
		"Authentication-Results: mx.lanqin.local; spf=pass smtp.mailfrom=example.test; dkim=fail header.d=example.test; dmarc=temperror",
		"Received-SPF: pass (mx.lanqin.local: domain of sender@example.test designates 192.0.2.1 as permitted sender)",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"auth body",
	}, "\r\n")
	if err := os.WriteFile(filepath.Join(dir, "1749819601.M1P1.auth"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if count, err := a.syncMaildirOnce(ctx); err != nil || count != 1 {
		t.Fatalf("sync count=%d err=%v", count, err)
	}

	client := &testClient{t: t, server: ts}
	var login map[string]any
	if code := client.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d body=%v", code, login)
	}
	var list struct {
		Items []MailMessage `json:"items"`
	}
	if code := client.do("GET", "/api/mail/messages?folder=Inbox&q=auth%20import", nil, &list); code != http.StatusOK || len(list.Items) != 1 {
		t.Fatalf("list code=%d items=%+v", code, list.Items)
	}
	var detail MailMessage
	if code := client.do("GET", "/api/mail/messages/"+list.Items[0].ID+"?markRead=0", nil, &detail); code != http.StatusOK {
		t.Fatalf("detail code=%d detail=%+v", code, detail)
	}
	if detail.Authentication.SPF != "pass" || detail.Authentication.DKIM != "fail" || detail.Authentication.DMARC != "temperror" {
		t.Fatalf("unexpected auth detail: %+v", detail.Authentication)
	}
	if !strings.Contains(detail.Authentication.AuthenticationResults, "spf=pass") || !strings.Contains(detail.Authentication.ReceivedSPF, "sender@example.test") {
		t.Fatalf("raw auth headers missing: %+v", detail.Authentication)
	}
}

func TestMaildirSyncHealthDisabled(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}
	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d body=%v", code, login)
	}

	var health maildirSyncHealthResponse
	if code := admin.do("GET", "/api/admin/maildir-sync/health", nil, &health); code != http.StatusOK {
		t.Fatalf("health code=%d body=%+v", code, health)
	}
	if health.Configured || health.Enabled || health.WorkerStarted || health.Running {
		t.Fatalf("unexpected disabled health: %+v", health)
	}
	if health.ScanSeconds != 30 {
		t.Fatalf("scan seconds=%d, want default 30", health.ScanSeconds)
	}
}

func TestMaildirSyncHealthAfterTrackedSync(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	root := t.TempDir()
	a.updateConfig(func(cfg *Config) { cfg.MaildirRoot = root })
	a.updateConfig(func(cfg *Config) { cfg.MaildirScanSeconds = 45 })
	adminUser, _, err := a.userByEmail(ctx, "admin@lanqin.local")
	if err != nil {
		t.Fatal(err)
	}
	var mailboxID string
	if err := a.db.QueryRowContext(ctx, `SELECT id FROM mailboxes WHERE user_id=? AND address=?`, adminUser.ID, "admin@lanqin.local").Scan(&mailboxID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.ExecContext(ctx, `DELETE FROM messages WHERE mailbox_id=?`, mailboxID); err != nil {
		t.Fatal(err)
	}
	mailboxes, err := a.maildirMailboxes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var admin maildirMailbox
	for _, mb := range mailboxes {
		if mb.Address == "admin@lanqin.local" {
			admin = mb
			break
		}
	}
	if admin.ID == "" {
		t.Fatal("admin mailbox not found")
	}
	dir := filepath.Join(root, admin.Domain, admin.LocalPart, "Maildir", "new")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := strings.Join([]string{
		"From: sender@example.test",
		"To: admin@lanqin.local",
		"Subject: Maildir health import",
		"Message-Id: <maildir-health@example.test>",
		"Date: Sat, 13 Jun 2026 15:00:00 +0000",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"hello from health test",
	}, "\r\n")
	if err := os.WriteFile(filepath.Join(dir, "1749826800.M1P1.health"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	counts, err := a.syncMaildirOnceTracked(ctx, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Imported != 1 || counts.FilesScanned != 1 {
		t.Fatalf("counts=%+v, want imported=1 filesScanned=1", counts)
	}
	health := a.maildirHealth.snapshot(a.config())
	if !health.Configured || !health.Enabled {
		t.Fatalf("configured health=%+v, want enabled", health)
	}
	if health.Running {
		t.Fatalf("health still running: %+v", health)
	}
	if health.LastRun == nil || health.LastRun.Status != "success" {
		t.Fatalf("last run=%+v, want success", health.LastRun)
	}
	if health.LastRun.Counts.Imported != 1 || health.Summary.Imported != 1 {
		t.Fatalf("health counts last=%+v summary=%+v", health.LastRun.Counts, health.Summary)
	}
	if health.NextRunAt == nil {
		t.Fatalf("next run is nil")
	}
}

func TestMaildirSyncImportsSentFolder(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	root := t.TempDir()
	a.updateConfig(func(cfg *Config) { cfg.MaildirRoot = root })
	adminUser, _, err := a.userByEmail(ctx, "admin@lanqin.local")
	if err != nil {
		t.Fatal(err)
	}
	var mailboxID string
	if err := a.db.QueryRowContext(ctx, `SELECT id FROM mailboxes WHERE user_id=? AND address=?`, adminUser.ID, "admin@lanqin.local").Scan(&mailboxID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.ExecContext(ctx, `DELETE FROM messages WHERE mailbox_id=?`, mailboxID); err != nil {
		t.Fatal(err)
	}
	sentFolderID, err := a.ensureFolder(ctx, mailboxID, "Sent")
	if err != nil {
		t.Fatal(err)
	}

	mailboxes, err := a.maildirMailboxes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var admin maildirMailbox
	for _, mb := range mailboxes {
		if mb.Address == "admin@lanqin.local" {
			admin = mb
			break
		}
	}
	if admin.ID == "" {
		t.Fatal("admin mailbox not found")
	}

	dir := filepath.Join(root, admin.Domain, admin.LocalPart, "Maildir", ".Sent", "new")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := strings.Join([]string{
		"From: admin@lanqin.local",
		"To: recipient@example.test",
		"Subject: SMTP sent archive",
		"Message-Id: <smtp-sent-archive@example.test>",
		"Date: Sat, 13 Jun 2026 14:00:00 +0000",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"archived from smtp client",
	}, "\r\n")
	if err := os.WriteFile(filepath.Join(dir, "1749823200.M1P1.sent"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	count, err := a.syncMaildirOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("imported=%d, want 1", count)
	}

	var folderID, subject string
	var read int
	err = a.db.QueryRow(`SELECT folder_id, subject, is_read FROM messages WHERE mailbox_id=? AND message_id='<smtp-sent-archive@example.test>'`, admin.ID).Scan(&folderID, &subject, &read)
	if err != nil {
		t.Fatal(err)
	}
	if folderID != sentFolderID || subject != "SMTP sent archive" || read != 1 {
		t.Fatalf("unexpected sent import folder=%q want=%q subject=%q read=%d", folderID, sentFolderID, subject, read)
	}
}

func TestWebmailSentWritesMaildirSent(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	a.updateConfig(func(cfg *Config) { cfg.MaildirRoot = t.TempDir() })
	user, mb := defaultAdminUserAndMailbox(t, a)
	clearMailboxMessagesForTest(t, a, mb.ID)

	msg, err := a.sendMailNow(ctx, user, mb, mailComposeInput{
		MailboxID: mb.ID,
		To:        []string{"recipient@example.test"},
		Subject:   "maildir sent copy",
		Text:      "sent body",
		HTML:      "<p>sent body</p>",
	})
	if err != nil {
		t.Fatal(err)
	}
	rawPath := maildirRawPathForTest(t, a, msg.ID)
	if !strings.Contains(filepath.ToSlash(rawPath), "/.Sent/cur/") {
		t.Fatalf("raw_path=%q, want .Sent/cur", rawPath)
	}
	if !strings.Contains(filepath.Base(rawPath), maildirFlagSeparator()+"S") {
		t.Fatalf("sent raw_path missing seen flag: %s", rawPath)
	}
	raw, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "Subject: maildir sent copy") {
		t.Fatalf("sent maildir raw missing subject:\n%s", string(raw))
	}
	count, err := a.syncMaildirOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("sync imported own sent copy=%d, want 0", count)
	}
}

func TestMaildirSyncBackfillsSQLiteOnlySent(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	a.updateConfig(func(cfg *Config) { cfg.MaildirRoot = t.TempDir() })
	user, mb := defaultAdminUserAndMailbox(t, a)
	clearMailboxMessagesForTest(t, a, mb.ID)

	msg, err := a.sendMailNow(ctx, user, mb, mailComposeInput{
		MailboxID: mb.ID,
		To:        []string{"recipient@example.test"},
		Subject:   "legacy sent copy",
		Text:      "legacy body",
		HTML:      "<p>legacy body</p>",
	})
	if err != nil {
		t.Fatal(err)
	}
	oldPath := maildirRawPathForTest(t, a, msg.ID)
	if err := os.Remove(oldPath); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.ExecContext(ctx, `UPDATE messages SET raw_path='' WHERE id=?`, msg.ID); err != nil {
		t.Fatal(err)
	}
	count, err := a.syncMaildirOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("backfilled=%d, want 1", count)
	}
	newPath := maildirRawPathForTest(t, a, msg.ID)
	if !strings.Contains(filepath.ToSlash(newPath), "/.Sent/cur/") {
		t.Fatalf("raw_path=%q, want .Sent/cur", newPath)
	}
	var messages int
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE mailbox_id=? AND message_id=?`, mb.ID, msg.MessageID).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if messages != 1 {
		t.Fatalf("messages with same Message-ID=%d, want 1", messages)
	}
}

func TestDraftWritesAndUpdatesMaildirDrafts(t *testing.T) {
	a := newTestApp(t)
	a.updateConfig(func(cfg *Config) { cfg.MaildirRoot = t.TempDir() })
	srv := httptest.NewServer(a.Router())
	defer srv.Close()
	client := &testClient{t: t, server: srv}
	var login map[string]any
	if code := client.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d body=%v", code, login)
	}
	_, mb := defaultAdminUserAndMailbox(t, a)
	clearMailboxMessagesForTest(t, a, mb.ID)

	var draft MailMessage
	payload := map[string]any{
		"mailboxId": mb.ID,
		"to":        []string{"recipient@example.test"},
		"subject":   "draft one",
		"text":      "draft body one",
		"html":      "<p>draft body one</p>",
	}
	if code := client.do("POST", "/api/mail/drafts", payload, &draft); code != http.StatusCreated {
		t.Fatalf("save draft code=%d draft=%+v", code, draft)
	}
	rawPath := maildirRawPathForTest(t, a, draft.ID)
	if !strings.Contains(filepath.ToSlash(rawPath), "/.Drafts/cur/") {
		t.Fatalf("raw_path=%q, want .Drafts/cur", rawPath)
	}
	if !strings.Contains(filepath.Base(rawPath), maildirFlagSeparator()+"S") {
		t.Fatalf("draft raw_path missing seen flag: %s", rawPath)
	}
	oldRawPath := rawPath

	payload["subject"] = "draft two"
	payload["text"] = "draft body two"
	payload["html"] = "<p>draft body two</p>"
	if code := client.do("POST", "/api/mail/drafts/"+draft.ID, payload, &draft); code != http.StatusOK {
		t.Fatalf("update draft code=%d draft=%+v", code, draft)
	}
	rawPath = maildirRawPathForTest(t, a, draft.ID)
	if _, err := os.Stat(oldRawPath); err == nil && oldRawPath != rawPath {
		t.Fatalf("old draft maildir file still exists: %s", oldRawPath)
	}
	raw, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "Subject: draft two") {
		t.Fatalf("updated draft raw missing new subject:\n%s", string(raw))
	}
}

func TestMoveAndDeleteMessageUpdateMaildir(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	a.updateConfig(func(cfg *Config) { cfg.MaildirRoot = t.TempDir() })
	srv := httptest.NewServer(a.Router())
	defer srv.Close()
	client := &testClient{t: t, server: srv}
	var login map[string]any
	if code := client.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d body=%v", code, login)
	}
	user, mb := defaultAdminUserAndMailbox(t, a)
	clearMailboxMessagesForTest(t, a, mb.ID)

	msg, err := a.sendMailNow(ctx, user, mb, mailComposeInput{
		MailboxID: mb.ID,
		To:        []string{"recipient@example.test"},
		Subject:   "move me",
		Text:      "move body",
		HTML:      "<p>move body</p>",
	})
	if err != nil {
		t.Fatal(err)
	}
	sentPath := maildirRawPathForTest(t, a, msg.ID)
	if code := client.do("POST", "/api/mail/messages/"+msg.ID+"/move", map[string]string{"folder": "Archive"}, nil); code != http.StatusOK {
		t.Fatalf("move code=%d", code)
	}
	archivePath := maildirRawPathForTest(t, a, msg.ID)
	if !strings.Contains(filepath.ToSlash(archivePath), "/.Archive/cur/") {
		t.Fatalf("raw_path=%q, want .Archive/cur", archivePath)
	}
	if _, err := os.Stat(sentPath); err == nil && sentPath != archivePath {
		t.Fatalf("old sent maildir file still exists: %s", sentPath)
	}
	if code := client.do("DELETE", "/api/mail/messages/"+msg.ID, nil, nil); code != http.StatusOK {
		t.Fatalf("trash code=%d", code)
	}
	trashPath := maildirRawPathForTest(t, a, msg.ID)
	if !strings.Contains(filepath.ToSlash(trashPath), "/.Trash/cur/") {
		t.Fatalf("raw_path=%q, want .Trash/cur", trashPath)
	}
	if _, err := os.Stat(archivePath); err == nil && archivePath != trashPath {
		t.Fatalf("old archive maildir file still exists: %s", archivePath)
	}
	if code := client.do("DELETE", "/api/mail/messages/"+msg.ID, nil, nil); code != http.StatusOK {
		t.Fatalf("permanent delete code=%d", code)
	}
	if _, err := os.Stat(trashPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("trash maildir file exists after permanent delete err=%v", err)
	}
}

func TestMoveUnreadMessageToCustomFolderPreservesUnread(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	a.updateConfig(func(cfg *Config) { cfg.MaildirRoot = t.TempDir() })
	srv := httptest.NewServer(a.Router())
	defer srv.Close()
	client := &testClient{t: t, server: srv}
	var login map[string]any
	if code := client.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d body=%v", code, login)
	}
	user, mb := defaultAdminUserAndMailbox(t, a)
	clearMailboxMessagesForTest(t, a, mb.ID)
	msg, err := a.sendMailNow(ctx, user, mb, mailComposeInput{
		MailboxID: mb.ID,
		To:        []string{"recipient@example.test"},
		Subject:   "custom unread",
		Text:      "custom unread body",
	})
	if err != nil {
		t.Fatal(err)
	}
	if code := client.do("POST", "/api/mail/messages/"+msg.ID+"/mark-read", map[string]bool{"read": false}, nil); code != http.StatusOK {
		t.Fatalf("mark unread code=%d", code)
	}
	if code := client.do("POST", "/api/mail/messages/"+msg.ID+"/move", map[string]string{"folder": "iQIYI"}, nil); code != http.StatusOK {
		t.Fatalf("move code=%d", code)
	}
	rawPath := maildirRawPathForTest(t, a, msg.ID)
	if !strings.Contains(filepath.ToSlash(rawPath), "/.iQIYI/new/") {
		t.Fatalf("raw_path=%q, want unread custom folder path", rawPath)
	}
	if _, err := a.syncMaildirOnce(ctx); err != nil {
		t.Fatal(err)
	}
	var read int
	if err := a.db.QueryRowContext(ctx, `SELECT is_read FROM messages WHERE id=?`, msg.ID).Scan(&read); err != nil {
		t.Fatal(err)
	}
	if read != 0 {
		t.Fatalf("custom folder message read=%d, want unread", read)
	}
	var folders struct {
		Items []MailFolder `json:"items"`
	}
	if code := client.do("GET", "/api/mail/folders", nil, &folders); code != http.StatusOK {
		t.Fatalf("folders code=%d", code)
	}
	for _, folder := range folders.Items {
		if folder.Name == "iQIYI" && folder.UnreadCount == 1 {
			return
		}
	}
	t.Fatalf("custom folder unread count missing: %+v", folders.Items)
}

func TestMessageFlagsUpdateMaildir(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	a.updateConfig(func(cfg *Config) { cfg.MaildirRoot = t.TempDir() })
	srv := httptest.NewServer(a.Router())
	defer srv.Close()
	client := &testClient{t: t, server: srv}
	var login map[string]any
	if code := client.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d body=%v", code, login)
	}
	user, mb := defaultAdminUserAndMailbox(t, a)
	clearMailboxMessagesForTest(t, a, mb.ID)

	msg, err := a.sendMailNow(ctx, user, mb, mailComposeInput{
		MailboxID: mb.ID,
		To:        []string{"admin@lanqin.local"},
		Subject:   "flag me",
		Text:      "flag body",
		HTML:      "<p>flag body</p>",
	})
	if err != nil {
		t.Fatal(err)
	}
	if code := client.do("POST", "/api/mail/messages/"+msg.ID+"/mark-read", map[string]bool{"read": false}, nil); code != http.StatusOK {
		t.Fatalf("mark unread code=%d", code)
	}
	unreadPath := maildirRawPathForTest(t, a, msg.ID)
	if strings.Contains(filepath.Base(unreadPath), maildirFlagSeparator()) {
		t.Fatalf("unread path should not have seen flag: %s", unreadPath)
	}
	if !strings.EqualFold(filepath.Base(filepath.Dir(unreadPath)), "new") {
		t.Fatalf("unread path dir=%s, want new", filepath.Dir(unreadPath))
	}
	if code := client.do("POST", "/api/mail/messages/"+msg.ID+"/star", map[string]bool{"starred": true}, nil); code != http.StatusOK {
		t.Fatalf("star code=%d", code)
	}
	starredPath := maildirRawPathForTest(t, a, msg.ID)
	if !strings.Contains(filepath.Base(starredPath), maildirFlagSeparator()+"F") {
		t.Fatalf("starred path missing F flag: %s", starredPath)
	}
	if !strings.EqualFold(filepath.Base(filepath.Dir(starredPath)), "cur") {
		t.Fatalf("starred path dir=%s, want cur", filepath.Dir(starredPath))
	}
}

func TestIMAPUIDAndModSeqProgression(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	a.updateConfig(func(cfg *Config) { cfg.MaildirRoot = t.TempDir() })
	srv := httptest.NewServer(a.Router())
	defer srv.Close()
	client := &testClient{t: t, server: srv}
	var login map[string]any
	if code := client.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d body=%v", code, login)
	}
	user, mb := defaultAdminUserAndMailbox(t, a)
	clearMailboxMessagesForTest(t, a, mb.ID)
	sentID, err := a.ensureFolder(ctx, mb.ID, "Sent")
	if err != nil {
		t.Fatal(err)
	}
	archiveID, err := a.ensureFolder(ctx, mb.ID, "Archive")
	if err != nil {
		t.Fatal(err)
	}

	first, err := a.sendMailNow(ctx, user, mb, mailComposeInput{MailboxID: mb.ID, To: []string{"one@example.test"}, Subject: "uid one", Text: "one", HTML: "<p>one</p>"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.sendMailNow(ctx, user, mb, mailComposeInput{MailboxID: mb.ID, To: []string{"two@example.test"}, Subject: "uid two", Text: "two", HTML: "<p>two</p>"})
	if err != nil {
		t.Fatal(err)
	}
	var firstUID, firstModSeq, secondUID int64
	if err := a.db.QueryRowContext(ctx, `SELECT imap_uid,imap_modseq FROM messages WHERE id=?`, first.ID).Scan(&firstUID, &firstModSeq); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRowContext(ctx, `SELECT imap_uid FROM messages WHERE id=?`, second.ID).Scan(&secondUID); err != nil {
		t.Fatal(err)
	}
	if firstUID <= 0 || secondUID != firstUID+1 {
		t.Fatalf("sent UIDs first=%d second=%d, want consecutive positive", firstUID, secondUID)
	}
	var sentUIDNext int64
	if err := a.db.QueryRowContext(ctx, `SELECT uid_next FROM folders WHERE id=?`, sentID).Scan(&sentUIDNext); err != nil {
		t.Fatal(err)
	}
	if sentUIDNext <= secondUID {
		t.Fatalf("sent uid_next=%d, second uid=%d", sentUIDNext, secondUID)
	}

	starred := true
	if err := a.updateMessageMaildirFlags(ctx, first.ID, nil, &starred); err != nil {
		t.Fatal(err)
	}
	modSeq, err := a.updateMessageModSeq(ctx, first.ID, sentID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.ExecContext(ctx, `UPDATE messages SET is_starred=1,imap_modseq=? WHERE id=?`, modSeq, first.ID); err != nil {
		t.Fatal(err)
	}
	var afterFlagUID, afterFlagModSeq int64
	if err := a.db.QueryRowContext(ctx, `SELECT imap_uid,imap_modseq FROM messages WHERE id=?`, first.ID).Scan(&afterFlagUID, &afterFlagModSeq); err != nil {
		t.Fatal(err)
	}
	if afterFlagUID != firstUID || afterFlagModSeq <= firstModSeq {
		t.Fatalf("after flag uid/modseq=%d/%d, want uid %d and modseq > %d", afterFlagUID, afterFlagModSeq, firstUID, firstModSeq)
	}

	if err := a.moveMessageMaildir(ctx, first.ID, archiveID); err != nil {
		t.Fatal(err)
	}
	var archiveUID, archiveModSeq int64
	var folderID string
	if err := a.db.QueryRowContext(ctx, `SELECT folder_id,imap_uid,imap_modseq FROM messages WHERE id=?`, first.ID).Scan(&folderID, &archiveUID, &archiveModSeq); err != nil {
		t.Fatal(err)
	}
	if folderID != archiveID {
		t.Fatalf("folder after move=%s, want archive %s", folderID, archiveID)
	}
	if archiveUID <= 0 {
		t.Fatalf("archive uid=%d, want positive uid", archiveUID)
	}
	var archiveUIDNext, archiveHighestModSeq int64
	if err := a.db.QueryRowContext(ctx, `SELECT uid_next,highest_modseq FROM folders WHERE id=?`, archiveID).Scan(&archiveUIDNext, &archiveHighestModSeq); err != nil {
		t.Fatal(err)
	}
	if archiveUIDNext <= archiveUID {
		t.Fatalf("archive uid_next=%d, uid=%d", archiveUIDNext, archiveUID)
	}
	if archiveModSeq != archiveHighestModSeq {
		t.Fatalf("archive modseq=%d highest=%d, want equal", archiveModSeq, archiveHighestModSeq)
	}
}

func TestMaildirSyncUpdatesMovedMessageState(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	a.updateConfig(func(cfg *Config) { cfg.MaildirRoot = t.TempDir() })
	user, mb := defaultAdminUserAndMailbox(t, a)
	clearMailboxMessagesForTest(t, a, mb.ID)

	msg, err := a.sendMailNow(ctx, user, mb, mailComposeInput{
		MailboxID: mb.ID,
		To:        []string{"recipient@example.test"},
		Subject:   "imap moved",
		Text:      "move body",
		HTML:      "<p>move body</p>",
	})
	if err != nil {
		t.Fatal(err)
	}
	sentPath := maildirRawPathForTest(t, a, msg.ID)
	archiveID, err := a.ensureFolder(ctx, mb.ID, "Archive")
	if err != nil {
		t.Fatal(err)
	}
	archiveDir := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(sentPath))), ".Archive", "cur")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(archiveDir, filepath.Base(sentPath))
	if err := os.Rename(sentPath, archivePath); err != nil {
		t.Fatal(err)
	}
	count, err := a.syncMaildirOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("sync count=%d, want no import/backfill", count)
	}
	var folderID, rawPath string
	if err := a.db.QueryRowContext(ctx, `SELECT folder_id,raw_path FROM messages WHERE id=?`, msg.ID).Scan(&folderID, &rawPath); err != nil {
		t.Fatal(err)
	}
	if folderID != archiveID || rawPath != archivePath {
		t.Fatalf("folder/raw after move=%q %q, want %q %q", folderID, rawPath, archiveID, archivePath)
	}
}

func TestMaildirSyncKeepsDistinctCopiesWithSameMessageID(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	a.updateConfig(func(cfg *Config) { cfg.MaildirRoot = t.TempDir() })
	user, mb := defaultAdminUserAndMailbox(t, a)
	clearMailboxMessagesForTest(t, a, mb.ID)

	msg, err := a.sendMailNow(ctx, user, mb, mailComposeInput{
		MailboxID: mb.ID,
		To:        []string{"admin@lanqin.local"},
		Subject:   "self copy",
		Text:      "self body",
		HTML:      "<p>self body</p>",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.syncMaildirOnce(ctx); err != nil {
		t.Fatal(err)
	}
	var copies int
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE mailbox_id=? AND message_id=?`, mb.ID, msg.MessageID).Scan(&copies); err != nil {
		t.Fatal(err)
	}
	if copies != 2 {
		t.Fatalf("copies with same Message-ID=%d, want Sent and Inbox copies", copies)
	}
}

func TestMaildirSyncUpdatesFlagsFromIMAP(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	a.updateConfig(func(cfg *Config) { cfg.MaildirRoot = t.TempDir() })
	user, mb := defaultAdminUserAndMailbox(t, a)
	clearMailboxMessagesForTest(t, a, mb.ID)

	msg, err := a.sendMailNow(ctx, user, mb, mailComposeInput{
		MailboxID: mb.ID,
		To:        []string{"recipient@example.test"},
		Subject:   "imap flags",
		Text:      "flag body",
		HTML:      "<p>flag body</p>",
	})
	if err != nil {
		t.Fatal(err)
	}
	rawPath := maildirRawPathForTest(t, a, msg.ID)
	flaggedPath := maildirPathWithFlags(rawPath, false, true)
	if err := os.MkdirAll(filepath.Dir(flaggedPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(rawPath, flaggedPath); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.ExecContext(ctx, `UPDATE messages SET updated_at=? WHERE id=?`, a.now().UTC().Add(-10*time.Minute).Format(time.RFC3339Nano), msg.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.syncMaildirOnce(ctx); err != nil {
		t.Fatal(err)
	}
	var read, starred int
	var dbPath string
	if err := a.db.QueryRowContext(ctx, `SELECT is_read,is_starred,raw_path FROM messages WHERE id=?`, msg.ID).Scan(&read, &starred, &dbPath); err != nil {
		t.Fatal(err)
	}
	if read != 0 || starred != 1 || dbPath != flaggedPath {
		t.Fatalf("flags/path after sync read=%d starred=%d path=%q want 0 1 %q", read, starred, dbPath, flaggedPath)
	}
}

func TestMaildirSyncDeletesMissingMessage(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	a.updateConfig(func(cfg *Config) { cfg.MaildirRoot = t.TempDir() })
	user, mb := defaultAdminUserAndMailbox(t, a)
	clearMailboxMessagesForTest(t, a, mb.ID)

	msg, err := a.sendMailNow(ctx, user, mb, mailComposeInput{
		MailboxID: mb.ID,
		To:        []string{"recipient@example.test"},
		Subject:   "imap delete",
		Text:      "delete body",
		HTML:      "<p>delete body</p>",
	})
	if err != nil {
		t.Fatal(err)
	}
	rawPath := maildirRawPathForTest(t, a, msg.ID)
	if err := os.Remove(rawPath); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.ExecContext(ctx, `UPDATE messages SET updated_at=? WHERE id=?`, a.now().UTC().Add(-10*time.Minute).Format(time.RFC3339Nano), msg.ID); err != nil {
		t.Fatal(err)
	}
	count, err := a.syncMaildirOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("cleanup count=%d, want 1", count)
	}
	var remaining int
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE id=?`, msg.ID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("message remaining=%d, want deleted", remaining)
	}
}

func TestMailboxQuotaRejectsNewMessage(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	user, mb := defaultAdminUserAndMailbox(t, a)
	clearMailboxMessagesForTest(t, a, mb.ID)
	if _, err := a.db.ExecContext(ctx, `UPDATE users SET storage_quota_mb=1 WHERE id=?`, user.ID); err != nil {
		t.Fatal(err)
	}
	_, err := a.sendMailNow(ctx, user, mb, mailComposeInput{
		MailboxID: mb.ID,
		To:        []string{"person@example.test"},
		Subject:   "quota overflow",
		Text:      strings.Repeat("x", 1024*1024+1),
	})
	if !errors.Is(err, errMailboxQuotaExceeded) {
		t.Fatalf("sendMailNow error=%v, want quota exceeded", err)
	}

	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	client := &testClient{t: t, server: ts}
	var login map[string]any
	if code := client.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d", code)
	}
	var errBody map[string]any
	if code := client.do("POST", "/api/mail/send", map[string]any{
		"mailboxId": mb.ID,
		"to":        []string{"person@example.test"},
		"subject":   "quota overflow api",
		"text":      strings.Repeat("y", 1024*1024+1),
	}, &errBody); code != http.StatusInsufficientStorage {
		t.Fatalf("quota api code=%d body=%v", code, errBody)
	}
}

func TestMailStatsQuotaAndCleanupIsolation(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}
	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d", code)
	}
	domainID := mustDefaultDomainID(t, a)
	aliceMB := createTestMailbox(t, admin, domainID, "quota-alice", "Quota Alice", "Password123!", map[string]any{"quotaMb": 2})
	bobMB := createTestMailbox(t, admin, domainID, "quota-bob", "Quota Bob", "Password123!", map[string]any{"quotaMb": 2})
	aliceUser, _, err := a.userByEmail(ctx, aliceMB.Address)
	if err != nil {
		t.Fatal(err)
	}
	bobUser, _, err := a.userByEmail(ctx, bobMB.Address)
	if err != nil {
		t.Fatal(err)
	}
	aliceTrash, err := a.ensureFolder(ctx, aliceMB.ID, "Trash")
	if err != nil {
		t.Fatal(err)
	}
	bobTrash, err := a.ensureFolder(ctx, bobMB.ID, "Trash")
	if err != nil {
		t.Fatal(err)
	}
	attachment := AttachmentInput{Filename: "note.txt", ContentType: "text/plain", ContentBase64: base64.StdEncoding.EncodeToString([]byte("hello attachment"))}
	if _, err := a.insertMessage(ctx, storedMessage{MailboxID: aliceMB.ID, FolderID: aliceTrash, MessageUID: newID("uid"), MessageID: "<alice-trash@example.test>", Subject: "alice trash", From: "sender@example.test", To: []string{aliceMB.Address}, SentAt: a.now().UTC(), ReceivedAt: a.now().UTC(), Snippet: "body", BodyText: "body"}, []AttachmentInput{attachment}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.insertMessage(ctx, storedMessage{MailboxID: bobMB.ID, FolderID: bobTrash, MessageUID: newID("uid"), MessageID: "<bob-trash@example.test>", Subject: "bob trash", From: "sender@example.test", To: []string{bobMB.Address}, SentAt: a.now().UTC(), ReceivedAt: a.now().UTC(), Snippet: "body", BodyText: "body"}, nil); err != nil {
		t.Fatal(err)
	}

	alice := &testClient{t: t, server: ts}
	if code := alice.do("POST", "/api/auth/login", map[string]string{"email": aliceMB.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("alice login code=%d", code)
	}
	var stats MailStats
	if code := alice.do("GET", "/api/me/stats?mailboxId="+aliceMB.ID+"&days=7", nil, &stats); code != http.StatusOK {
		t.Fatalf("stats code=%d stats=%+v", code, stats)
	}
	var aliceStorageQuotaMB int64
	if err := a.db.QueryRowContext(ctx, `SELECT storage_quota_mb FROM users WHERE id=?`, aliceUser.ID).Scan(&aliceStorageQuotaMB); err != nil {
		t.Fatal(err)
	}
	if stats.QuotaBytes != aliceStorageQuotaMB*1024*1024 || stats.AttachmentBytes == 0 || stats.QuotaUsedPct <= 0 {
		t.Fatalf("stats quota/attachment not populated: %+v", stats)
	}
	if stats.TotalIncoming != 1 || stats.TotalOutgoing != 0 || stats.AverageMessageBytes <= 0 {
		t.Fatalf("stats message totals not populated: %+v", stats)
	}
	if len(stats.Trend) != 7 || stats.Trend[len(stats.Trend)-1].Incoming != 1 {
		t.Fatalf("stats trend not populated: %+v", stats.Trend)
	}
	if !mailStatsDistributionHas(stats.Distribution, "trash", 1) || !mailStatsDistributionHas(stats.Distribution, "attachments", 1) {
		t.Fatalf("stats distribution not populated: %+v", stats.Distribution)
	}
	if len(stats.TopContacts) == 0 || stats.TopContacts[0].Email != "sender@example.test" || stats.TopContacts[0].Count != 1 {
		t.Fatalf("stats top contacts not populated: %+v", stats.TopContacts)
	}
	var cleanup struct {
		OK       bool  `json:"ok"`
		Affected int64 `json:"affected"`
	}
	if code := alice.do("POST", "/api/me/cleanup", map[string]any{"mailboxId": aliceMB.ID, "target": "empty-trash"}, &cleanup); code != http.StatusOK || cleanup.Affected != 1 {
		t.Fatalf("cleanup code=%d body=%+v", code, cleanup)
	}
	var aliceRemaining, bobRemaining int
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE mailbox_id=?`, aliceMB.ID).Scan(&aliceRemaining); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE mailbox_id=?`, bobMB.ID).Scan(&bobRemaining); err != nil {
		t.Fatal(err)
	}
	if aliceRemaining != 0 || bobRemaining != 1 {
		t.Fatalf("cleanup isolation alice=%d bob=%d, want 0/1", aliceRemaining, bobRemaining)
	}
	if aliceUser.ID == "" || bobUser.ID == "" {
		t.Fatal("test users were not created")
	}
}

func mailStatsDistributionHas(items []MailStatsDistributionItem, key string, count int64) bool {
	for _, item := range items {
		if item.Key == key && item.Count == count {
			return true
		}
	}
	return false
}

func mustDefaultDomainID(t *testing.T, a *App) string {
	t.Helper()
	var id string
	if err := a.db.QueryRow(`SELECT id FROM domains LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func containsString(items []string, needle string) bool {
	for _, item := range items {
		if item == needle {
			return true
		}
	}
	return false
}

func folderListContains(items []MailFolder, name string) bool {
	for _, item := range items {
		if item.Name == name {
			return true
		}
	}
	return false
}

func customFolderNames(items []MailFolder) []string {
	names := []string{}
	for _, item := range items {
		if !isSystemFolderName(item.Name) {
			names = append(names, item.Name)
		}
	}
	return names
}

func withoutPermissions(items []string, removed ...string) []string {
	removedSet := map[string]bool{}
	for _, item := range removed {
		removedSet[item] = true
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if !removedSet[item] {
			out = append(out, item)
		}
	}
	return out
}

func maildirRawPathForTest(t *testing.T, a *App, messageID string) string {
	t.Helper()
	var rawPath string
	if err := a.db.QueryRow(`SELECT raw_path FROM messages WHERE id=?`, messageID).Scan(&rawPath); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(rawPath) == "" {
		t.Fatalf("message %s raw_path is empty", messageID)
	}
	if _, err := os.Stat(rawPath); err != nil {
		t.Fatalf("raw_path %s stat error: %v", rawPath, err)
	}
	return rawPath
}

func clearMailboxMessagesForTest(t *testing.T, a *App, mailboxID string) {
	t.Helper()
	if _, err := a.db.Exec(`DELETE FROM messages WHERE mailbox_id=?`, mailboxID); err != nil {
		t.Fatal(err)
	}
}
