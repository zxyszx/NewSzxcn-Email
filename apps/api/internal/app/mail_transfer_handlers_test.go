package app

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseMBOXMultipleMessages(t *testing.T) {
	raw := strings.Join([]string{
		"From sender@example.com Mon Jan  1 00:00:00 2024",
		"From: sender@example.com",
		"To: first@example.com",
		"Subject: first",
		"",
		"first body",
		">From escaped body line",
		"From sender@example.com Tue Jan  2 00:00:00 2024",
		"From: sender@example.com",
		"To: second@example.com",
		"Subject: second",
		"",
		"second body",
	}, "\n")
	messages, err := parseMBOX(strings.NewReader(raw), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 {
		t.Fatalf("messages=%d", len(messages))
	}
	if !bytes.Contains(messages[0], []byte("Subject: first")) || !bytes.Contains(messages[0], []byte("From escaped body line")) {
		t.Fatalf("first message=%q", messages[0])
	}
	if !bytes.Contains(messages[1], []byte("Subject: second")) {
		t.Fatalf("second message=%q", messages[1])
	}
}

func TestMailImportExportAndOwnership(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}
	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("admin login=%d", code)
	}
	var domains struct {
		Items []Domain `json:"items"`
	}
	if code := admin.do("GET", "/api/admin/domains", nil, &domains); code != http.StatusOK || len(domains.Items) == 0 {
		t.Fatalf("domains code=%d items=%d", code, len(domains.Items))
	}
	ownerMailbox := createTestMailbox(t, admin, domains.Items[0].ID, "transfer-owner", "Transfer Owner", "Password123!", nil)
	otherMailbox := createTestMailbox(t, admin, domains.Items[0].ID, "transfer-other", "Transfer Other", "Password123!", nil)
	owner := &testClient{t: t, server: ts}
	if code := owner.do("POST", "/api/auth/login", map[string]string{"email": ownerMailbox.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("owner login=%d", code)
	}

	eml := []byte("From: sender@example.com\r\nTo: " + ownerMailbox.Address + "\r\nSubject: imported message\r\nDate: Tue, 2 Jan 2024 12:00:00 +0000\r\nMessage-ID: <imported@example.com>\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nhello import")
	olderEML := []byte("From: sender@example.com\r\nTo: " + ownerMailbox.Address + "\r\nSubject: older imported message\r\nDate: Mon, 1 Jan 2024 12:00:00 +0000\r\nMessage-ID: <older-imported@example.com>\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nolder import")
	var imported struct {
		Imported int      `json:"imported"`
		Skipped  int      `json:"skipped"`
		Errors   []string `json:"errors"`
	}
	if code := doMailImport(t, owner, ownerMailbox.ID, "Inbox", map[string][]byte{"message.eml": eml, "older.eml": olderEML}, &imported); code != http.StatusOK || imported.Imported != 2 || imported.Skipped != 0 {
		t.Fatalf("import code=%d response=%+v", code, imported)
	}

	var list struct {
		Items []MailMessage `json:"items"`
	}
	if code := owner.do("GET", "/api/mail/messages?folder=Inbox&mailboxId="+ownerMailbox.ID, nil, &list); code != http.StatusOK || len(list.Items) != 2 || list.Items[0].Subject != "imported message" || list.Items[1].Subject != "older imported message" {
		t.Fatalf("list code=%d items=%+v", code, list.Items)
	}

	status, archive := getMailExport(t, owner, "/api/mail/export?view=folder&folder=Inbox&mailboxId="+ownerMailbox.ID)
	if status != http.StatusOK {
		t.Fatalf("export status=%d body=%q", status, archive)
	}
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != 2 {
		t.Fatalf("zip entries=%d", len(zr.File))
	}
	entry, err := zr.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	exported, err := io.ReadAll(entry)
	entry.Close()
	if err != nil || !bytes.Contains(exported, []byte("Subject: imported message")) {
		t.Fatalf("exported message err=%v raw=%q", err, exported)
	}

	var denied map[string]any
	if code := doMailImport(t, owner, otherMailbox.ID, "Inbox", map[string][]byte{"message.eml": eml}, &denied); code != http.StatusNotFound {
		t.Fatalf("cross-mailbox import code=%d", code)
	}
	status, _ = getMailExport(t, owner, "/api/mail/export?view=unknown")
	if status != http.StatusForbidden {
		t.Fatalf("unknown export status=%d", status)
	}
}

func doMailImport(t *testing.T, client *testClient, mailboxID, folder string, files map[string][]byte, out any) int {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("mailboxId", mailboxID)
	_ = writer.WriteField("folder", folder)
	for name, content := range files {
		part, err := writer.CreateFormFile("files", name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, client.server.URL+"/api/mail/import", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if client.cookie != nil {
		req.AddCookie(client.cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode import response: %v", err)
		}
	}
	return resp.StatusCode
}

func getMailExport(t *testing.T, client *testClient, path string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, client.server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if client.cookie != nil {
		req.AddCookie(client.cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, body
}
