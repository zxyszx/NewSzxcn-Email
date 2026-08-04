package app

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	stdmail "net/mail"
	"net/url"
	"strings"
	"testing"
	"time"
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

	eml := []byte("From: sender@example.com\r\nTo: " + ownerMailbox.Address + "\r\nSubject: 中文标题\r\nDate: Tue, 2 Jan 2024 12:00:00 +0000\r\nMessage-ID: <imported@example.com>\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nhello import")
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
	if code := owner.do("GET", "/api/mail/messages?folder=Inbox&mailboxId="+ownerMailbox.ID, nil, &list); code != http.StatusOK || len(list.Items) != 2 || list.Items[0].Subject != "中文标题" || list.Items[1].Subject != "older imported message" {
		t.Fatalf("list code=%d items=%+v", code, list.Items)
	}
	receivedAt := time.Date(2024, time.January, 3, 8, 30, 0, 0, time.UTC)
	if _, err := a.db.Exec(`UPDATE messages SET received_at=? WHERE id=?`, receivedAt.Format(time.RFC3339Nano), list.Items[0].ID); err != nil {
		t.Fatal(err)
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
	if zr.File[0].Name != "中文标题 (20240103).eml" {
		t.Fatalf("first filename=%q", zr.File[0].Name)
	}
	wantModified := receivedAt
	if !zr.File[0].Modified.Equal(wantModified) {
		t.Fatalf("first modified=%s want=%s", zr.File[0].Modified, wantModified)
	}
	entry, err := zr.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	exported, err := io.ReadAll(entry)
	entry.Close()
	if err != nil {
		t.Fatalf("exported message err=%v raw=%q", err, exported)
	}
	parsed, err := stdmail.ReadMessage(bytes.NewReader(exported))
	if err != nil {
		t.Fatal(err)
	}
	decodedSubject, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
	if err != nil || decodedSubject != "中文标题" {
		t.Fatalf("decoded subject=%q err=%v", decodedSubject, err)
	}
	messageDate, err := parsed.Header.Date()
	if err != nil || !messageDate.Equal(time.Date(2024, time.January, 2, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("message date=%s err=%v", messageDate, err)
	}

	selectedPath := "/api/mail/export?view=folder&folder=Inbox&mailboxId=" + ownerMailbox.ID + "&messageId=" + url.QueryEscape(list.Items[1].ID)
	status, selectedArchive := getMailExport(t, owner, selectedPath)
	if status != http.StatusOK {
		t.Fatalf("selected export status=%d body=%q", status, selectedArchive)
	}
	selectedZip, err := zip.NewReader(bytes.NewReader(selectedArchive), int64(len(selectedArchive)))
	if err != nil {
		t.Fatal(err)
	}
	if len(selectedZip.File) != 1 || selectedZip.File[0].Name != "older imported message (20240101).eml" {
		t.Fatalf("selected entries=%v", exportEntryNames(selectedZip.File))
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

func TestSelectedMailExportStillEnforcesOwnership(t *testing.T) {
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
	ownerMailbox := createTestMailbox(t, admin, domains.Items[0].ID, "export-owner", "Export Owner", "Password123!", nil)
	otherMailbox := createTestMailbox(t, admin, domains.Items[0].ID, "export-other", "Export Other", "Password123!", nil)
	owner := &testClient{t: t, server: ts}
	other := &testClient{t: t, server: ts}
	if code := owner.do("POST", "/api/auth/login", map[string]string{"email": ownerMailbox.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("owner login=%d", code)
	}
	if code := other.do("POST", "/api/auth/login", map[string]string{"email": otherMailbox.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("other login=%d", code)
	}
	otherEML := []byte("From: sender@example.com\r\nTo: " + otherMailbox.Address + "\r\nSubject: private message\r\nDate: Tue, 2 Jan 2024 12:00:00 +0000\r\nMessage-ID: <private@example.com>\r\n\r\nprivate")
	var imported map[string]any
	if code := doMailImport(t, other, otherMailbox.ID, "Inbox", map[string][]byte{"private.eml": otherEML}, &imported); code != http.StatusOK {
		t.Fatalf("other import=%d response=%v", code, imported)
	}
	var otherList struct {
		Items []MailMessage `json:"items"`
	}
	if code := other.do("GET", "/api/mail/messages?folder=Inbox&mailboxId="+otherMailbox.ID, nil, &otherList); code != http.StatusOK || len(otherList.Items) != 1 {
		t.Fatalf("other list code=%d items=%d", code, len(otherList.Items))
	}
	path := "/api/mail/export?view=folder&folder=Inbox&mailboxId=" + ownerMailbox.ID + "&messageId=" + url.QueryEscape(otherList.Items[0].ID)
	status, archive := getMailExport(t, owner, path)
	if status != http.StatusOK {
		t.Fatalf("cross-owner export status=%d body=%q", status, archive)
	}
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != 0 {
		t.Fatalf("cross-owner export leaked entries=%v", exportEntryNames(zr.File))
	}
}

func exportEntryNames(files []*zip.File) []string {
	names := make([]string, 0, len(files))
	for _, file := range files {
		names = append(names, file.Name)
	}
	return names
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
