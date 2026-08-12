package app

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupEndpointsRejectMismatchedConfirmation(t *testing.T) {
	a := newTestApp(t)
	stopTestWorkers(a)
	server := httptest.NewServer(a.Router())
	defer server.Close()
	admin := &testClient{t: t, server: server}
	var response map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &response); code != http.StatusOK {
		t.Fatalf("login code=%d body=%v", code, response)
	}
	response = nil
	if code := admin.do("POST", "/api/admin/backups", map[string]any{"password": "BackupPassword123!", "confirmPassword": "DifferentPassword123!"}, &response); code != http.StatusBadRequest {
		t.Fatalf("manual backup mismatch code=%d body=%v", code, response)
	}
	response = nil
	if code := admin.do("POST", "/api/admin/backups/settings", map[string]any{"enabled": false, "days": 7, "password": "BackupPassword123!", "confirmPassword": "DifferentPassword123!"}, &response); code != http.StatusBadRequest {
		t.Fatalf("scheduled backup mismatch code=%d body=%v", code, response)
	}
}

func TestDiscoverTelegramGroupsReturnsUniqueCandidates(t *testing.T) {
	telegramServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"result":[`+
			`{"update_id":1,"message":{"text":"/newszxcn ABC123","chat":{"id":-1001,"type":"supergroup","title":"主备份"}}},`+
			`{"update_id":2,"message":{"text":"/newszxcn ABC123","chat":{"id":-1002,"type":"group","title":"异地备份"}}},`+
			`{"update_id":3,"message":{"text":"/newszxcn ABC123","chat":{"id":-1001,"type":"supergroup","title":"主备份"}}},`+
			`{"update_id":4,"message":{"text":"/newszxcn WRONG","chat":{"id":-1003,"type":"group","title":"无关群组"}}}]}`)
	}))
	defer telegramServer.Close()
	a := newTestApp(t)
	stopTestWorkers(a)
	a.telegramURL = telegramServer.URL
	groups, err := a.discoverTelegramGroups(context.Background(), "test-token", "ABC123")
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 || groups[0].ChatID != "-1001" || groups[1].ChatID != "-1002" {
		t.Fatalf("unexpected groups: %+v", groups)
	}
}

func TestGoogleDriveUploadRequestUsesMultipartRelated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "newszxcn-backup-test.tar.zst.enc")
	if err := os.WriteFile(path, []byte("encrypted backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	req, err := newGoogleDriveUploadRequest(context.Background(), path, "folder-123")
	if err != nil {
		t.Fatal(err)
	}
	mediaType, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/related" || params["boundary"] == "" {
		t.Fatalf("content type = %q, %v", req.Header.Get("Content-Type"), err)
	}
	reader := multipart.NewReader(req.Body, params["boundary"])
	metadataPart, err := reader.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		Name    string   `json:"name"`
		Parents []string `json:"parents"`
	}
	if err := json.NewDecoder(metadataPart).Decode(&metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Name != filepath.Base(path) || len(metadata.Parents) != 1 || metadata.Parents[0] != "folder-123" {
		t.Fatalf("metadata = %+v", metadata)
	}
	filePart, err := reader.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(filePart)
	if err != nil || string(raw) != "encrypted backup" {
		t.Fatalf("uploaded bytes = %q, %v", raw, err)
	}
}

func TestBackupEncryptionRequiresDeploymentSecret(t *testing.T) {
	dir := t.TempDir()
	a := newTestAppWithConfig(t, Config{
		Addr: ":0", DBPath: filepath.Join(dir, "data", "lanqin.db"), DataDir: filepath.Join(dir, "data"),
		CookieName: "lanqin_test", SessionTTLHours: 24, AdminEmail: "admin@example.com", AdminPassword: "ChangeMe123!", AllowInsecureHTTP: true,
	})
	if _, err := a.encryptBackupPassword("BackupPassword123!"); err == nil {
		t.Fatal("backup password encryption succeeded without a deployment secret")
	}
}

func TestBackupPasswordValidation(t *testing.T) {
	for _, valid := range []string{"12345678", "Restore Password 123!"} {
		if !validBackupPassword(valid) {
			t.Errorf("valid password rejected: %q", valid)
		}
	}
	for _, invalid := range []string{"1234567", "password\nvalue", "password\x00value", strings.Repeat("x", 1025)} {
		if validBackupPassword(invalid) {
			t.Errorf("invalid password accepted: %q", invalid)
		}
	}
}

func TestPublicServerIPValidation(t *testing.T) {
	for _, value := range []string{"203.0.113.10", "2001:4860:4860::8888"} {
		if !isPublicIP(net.ParseIP(value)) {
			t.Errorf("public IP rejected: %s", value)
		}
	}
	for _, value := range []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "169.254.1.1", "::1", "fc00::1"} {
		if isPublicIP(net.ParseIP(value)) {
			t.Errorf("non-public IP accepted: %s", value)
		}
	}
	if got := detectPublicServerIP(context.Background(), "203.0.113.10"); got != "203.0.113.10" {
		t.Fatalf("literal public IP = %q", got)
	}
	if got := detectPublicServerIP(context.Background(), "127.0.0.1"); got != "" {
		t.Fatalf("literal private IP = %q", got)
	}
}

func TestWriteRuntimeBackupEnv(t *testing.T) {
	t.Setenv("LANQIN_PUBLIC_HOSTNAME", "mail.example.com")
	t.Setenv("LANQIN_TEST_QUOTED", "value'with\\slashes\nand-newline")
	t.Setenv("LANQIN_BACKUP_DIR", "/backups")
	t.Setenv("LANQIN_UPDATE_SERVICE_URL", "http://updater:8080/v1/update")
	t.Setenv("UNRELATED_SECRET", "must-not-be-backed-up")
	path := filepath.Join(t.TempDir(), ".env")
	if err := writeRuntimeBackupEnv(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	contents := string(raw)
	for _, expected := range []string{"LANQIN_PUBLIC_HOSTNAME='mail.example.com'", `LANQIN_TEST_QUOTED='value\'with\\slashes\nand-newline'`} {
		if !strings.Contains(contents, expected) {
			t.Errorf("backup environment missing %q: %s", expected, contents)
		}
	}
	for _, excluded := range []string{"UNRELATED_SECRET", "must-not-be-backed-up", "LANQIN_BACKUP_DIR", "LANQIN_UPDATE_SERVICE_URL", "http://updater:8080"} {
		if strings.Contains(contents, excluded) {
			t.Fatalf("backup environment included excluded value %q", excluded)
		}
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("backup environment permissions = %v, %v", info.Mode().Perm(), err)
	}
}

func TestBackupAssetsAvailableWithBundledCompose(t *testing.T) {
	dir := t.TempDir()
	compose := filepath.Join(dir, "deploy", "docker-compose.yml")
	if err := os.MkdirAll(filepath.Dir(compose), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(compose, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := newTestAppWithConfig(t, Config{
		Addr: ":0", DBPath: filepath.Join(dir, "data", "lanqin.db"), DataDir: filepath.Join(dir, "data"),
		CookieName: "lanqin_test", SessionTTLHours: 24, AdminEmail: "admin@example.com", AdminPassword: "ChangeMe123!",
		AllowInsecureHTTP: true, BackupSourceDir: filepath.Dir(compose), BackupDir: filepath.Join(dir, "data", "disaster-backups"),
	})
	stopTestWorkers(a)
	if !a.backupAssetsAvailable() {
		t.Fatal("bundled compose did not enable complete backups")
	}
	if err := os.Remove(compose); err != nil {
		t.Fatal(err)
	}
	if a.backupAssetsAvailable() {
		t.Fatal("missing bundled compose incorrectly enabled complete backups")
	}
}

func TestBackupPasswordEncryptionAndTelegramReport(t *testing.T) {
	dir := t.TempDir()
	a := newTestAppWithConfig(t, Config{
		Addr: ":0", AppVersion: "v1.2.31", DBPath: filepath.Join(dir, "data", "lanqin.db"), DataDir: filepath.Join(dir, "data"),
		CookieName: "lanqin_test", SessionTTLHours: 24, AdminEmail: "admin@newszxcn.com", AdminPassword: "ChangeMe123!",
		PublicHostname: "mail.newszxcn.com", PublicBaseURL: "https://mail.newszxcn.com", AllowInsecureHTTP: true, UpdateServiceToken: "test-update-secret",
	})

	ciphertext, err := a.encryptBackupPassword("BackupPassword123!")
	if err != nil || ciphertext == "BackupPassword123!" {
		t.Fatalf("password encryption failed: %q %v", ciphertext, err)
	}
	plain, err := a.decryptBackupPassword(ciphertext)
	if err != nil || plain != "BackupPassword123!" {
		t.Fatalf("password decryption = %q, %v", plain, err)
	}
	if !validTelegramPrivateChatID("-1001234567890") {
		t.Fatal("private Telegram group chat ID was rejected")
	}

	now := a.now().UTC().Format("2006-01-02T15:04:05Z")
	if _, err := a.db.Exec(`INSERT INTO domains(id,name,status,dkim_selector,dkim_public_key,dkim_private_key,dns_status,created_at,updated_at) VALUES('domain_xyes','xyes.me','active','mail','','','unchecked',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO users(id,login_name,email,display_name,role,password_hash,created_at,updated_at) VALUES('user_xyes','user@xyes.me','user@xyes.me','User','user','hash',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "newszxcn-backup-20260811-120000-1.2.31.tar.zst.enc")
	if err := os.WriteFile(path, []byte("encrypted backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	report, err := a.backupTelegramReport(context.Background(), path, info)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"备份成功", "mail.newszxcn.com", "已有域名", "newszxcn.com", "xyes.me", "管理员账号", "admin@newszxcn.com", "普通用户账号", "user@xyes.me", "请不要解压", "本地上传", "1Password"} {
		if !strings.Contains(report, expected) {
			t.Errorf("report missing %q: %s", expected, report)
		}
	}
	if strings.Contains(report, "newszxcn.com（管理员）") {
		t.Fatal("domain list incorrectly contains account role")
	}
	if strings.Contains(report, "BackupPassword123!") || strings.Contains(report, "ChangeMe123!") {
		t.Fatal("report leaked a password")
	}
}
