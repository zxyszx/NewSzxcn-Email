package app

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSubNestReadOnlyIntegrationGrantLifecycle(t *testing.T) {
	a := newTestApp(t)
	server := httptest.NewServer(a.Router())
	defer server.Close()
	admin := &testClient{t: t, server: server}
	if code := admin.do(http.MethodPost, "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, nil); code != http.StatusOK {
		t.Fatalf("admin login code=%d", code)
	}

	var owner AdminUser
	if code := admin.do(http.MethodPost, "/api/admin/users", map[string]any{
		"email": "subnest-owner@lanqin.local", "displayName": "SubNest Owner", "password": "SubNestPassword123!", "role": "user",
	}, &owner); code != http.StatusCreated {
		t.Fatalf("create owner code=%d owner=%+v", code, owner)
	}
	mailbox, err := a.mailboxByAddress(t.Context(), owner.Email)
	if err != nil {
		t.Fatal(err)
	}
	var inboxID, archiveID string
	if err := a.db.QueryRow(`SELECT id FROM folders WHERE mailbox_id=? AND lower(name)='inbox'`, mailbox.ID).Scan(&inboxID); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow(`SELECT id FROM folders WHERE mailbox_id=? AND lower(name)='archive'`, mailbox.ID).Scan(&archiveID); err != nil {
		t.Fatal(err)
	}
	now := a.now().UTC()
	recentInbox := insertSubNestTestMessage(t, a, mailbox, inboxID, "recent inbox", now.Add(-5*time.Minute), true)
	oldInbox := insertSubNestTestMessage(t, a, mailbox, inboxID, "old inbox", now.Add(-2*time.Hour), false)
	recentArchive := insertSubNestTestMessage(t, a, mailbox, archiveID, "recent archive", now.Add(-4*time.Minute), false)

	regular := &testClient{t: t, server: server}
	if code := regular.do(http.MethodPost, "/api/auth/login", map[string]string{"email": owner.Email, "password": "SubNestPassword123!"}, nil); code != http.StatusOK {
		t.Fatalf("owner login code=%d", code)
	}
	if code := regular.do(http.MethodPost, "/api/me/api-tokens", map[string]any{"name": "forbidden", "scopes": []string{"subnest:read"}}, &map[string]any{}); code != http.StatusForbidden {
		t.Fatalf("regular user subnest token code=%d", code)
	}

	token := createTestAPITokenWithScopes(t, admin, "subnest-read-only", []string{"subnest:read"})
	client := &testClient{t: t, server: server, bearer: token}
	var mailboxes struct {
		Items []subNestMailbox `json:"items"`
	}
	if code := client.do(http.MethodGet, "/api/open/v1/subnest/mailboxes", nil, &mailboxes); code != http.StatusOK {
		t.Fatalf("list mailboxes code=%d", code)
	}
	if !subNestMailboxesContain(mailboxes.Items, mailbox.ID) {
		t.Fatalf("cross-account mailbox missing: %+v", mailboxes.Items)
	}
	var folders struct {
		Items []subNestFolder `json:"items"`
	}
	if code := client.do(http.MethodGet, "/api/open/v1/subnest/mailboxes/"+mailbox.ID+"/folders", nil, &folders); code != http.StatusOK || len(folders.Items) == 0 {
		t.Fatalf("list folders code=%d folders=%+v", code, folders.Items)
	}

	var grant subNestGrant
	if code := client.do(http.MethodPost, "/api/open/v1/subnest/grants", map[string]any{
		"externalGrantId": "subnest-link-001", "mailboxId": mailbox.ID, "folderIds": []string{inboxID}, "windowMinutes": 30,
	}, &grant); code != http.StatusCreated {
		t.Fatalf("create grant code=%d grant=%+v", code, grant)
	}
	if grant.ID == "" || grant.ExternalGrantID != "subnest-link-001" || grant.MailboxID != mailbox.ID || grant.Status != "active" {
		t.Fatalf("unexpected grant=%+v", grant)
	}
	var replayed subNestGrant
	if code := client.do(http.MethodPost, "/api/open/v1/subnest/grants", map[string]any{
		"externalGrantId": "subnest-link-001", "mailboxId": mailbox.ID, "folderIds": []string{inboxID}, "windowMinutes": 30,
	}, &replayed); code != http.StatusOK || replayed.ID != grant.ID {
		t.Fatalf("idempotent grant replay code=%d grant=%+v", code, replayed)
	}
	if code := client.do(http.MethodPost, "/api/open/v1/subnest/grants", map[string]any{
		"externalGrantId": "subnest-link-001", "mailboxId": mailbox.ID, "folderIds": []string{inboxID}, "windowMinutes": 1440,
	}, &map[string]any{}); code != http.StatusConflict {
		t.Fatalf("changed duplicate external grant code=%d", code)
	}

	var page struct {
		Items []sharedInboxMessage `json:"items"`
	}
	base := "/api/open/v1/subnest/grants/" + grant.ID
	if code := client.do(http.MethodGet, base+"/messages", nil, &page); code != http.StatusOK {
		t.Fatalf("list grant messages code=%d", code)
	}
	if !sharedInboxItemsContain(page.Items, recentInbox) || sharedInboxItemsContain(page.Items, oldInbox) || sharedInboxItemsContain(page.Items, recentArchive) {
		t.Fatalf("grant bypassed folder or time range: %+v", page.Items)
	}
	var detail sharedInboxMessageDetail
	if code := client.do(http.MethodGet, base+"/messages/"+recentInbox, nil, &detail); code != http.StatusOK || detail.ID != recentInbox || len(detail.Attachments) != 1 {
		t.Fatalf("message detail code=%d detail=%+v", code, detail)
	}
	attachmentID := detail.Attachments[0].ID
	req, err := http.NewRequest(http.MethodGet, server.URL+base+"/attachments/"+attachmentID, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	attachmentBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(attachmentBody) != "subnest attachment" {
		t.Fatalf("attachment code=%d body=%q", resp.StatusCode, attachmentBody)
	}

	if code := client.do(http.MethodPut, base, map[string]any{
		"mailboxId": mailbox.ID, "folderIds": []string{inboxID, archiveID}, "windowMinutes": 1440,
	}, &grant); code != http.StatusOK || grant.ID == "" {
		t.Fatalf("update grant code=%d grant=%+v", code, grant)
	}
	if code := client.do(http.MethodGet, base+"/messages", nil, &page); code != http.StatusOK || !sharedInboxItemsContain(page.Items, oldInbox) || !sharedInboxItemsContain(page.Items, recentArchive) {
		t.Fatalf("updated grant messages code=%d items=%+v", code, page.Items)
	}

	if code := client.do(http.MethodDelete, base, nil, &map[string]any{}); code != http.StatusOK {
		t.Fatalf("revoke grant code=%d", code)
	}
	if code := client.do(http.MethodGet, base+"/messages", nil, &map[string]any{}); code != http.StatusForbidden {
		t.Fatalf("revoked grant list code=%d", code)
	}
	if code := client.do(http.MethodGet, base+"/messages/"+recentInbox, nil, &map[string]any{}); code != http.StatusForbidden {
		t.Fatalf("revoked grant detail code=%d", code)
	}
	if code := client.do(http.MethodGet, base+"/attachments/"+attachmentID, nil, &map[string]any{}); code != http.StatusForbidden {
		t.Fatalf("revoked grant attachment code=%d", code)
	}
	var auditCount int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM inbox_integration_audit WHERE grant_id=?`, grant.ID).Scan(&auditCount); err != nil || auditCount < 5 {
		t.Fatalf("audit count=%d err=%v", auditCount, err)
	}

	wrongScope := createTestAPITokenWithScopes(t, admin, "messages-only", []string{"messages:read"})
	wrongClient := &testClient{t: t, server: server, bearer: wrongScope}
	if code := wrongClient.do(http.MethodGet, "/api/open/v1/subnest/mailboxes", nil, &map[string]any{}); code != http.StatusForbidden {
		t.Fatalf("wrong scope code=%d", code)
	}
	legacyWildcard := createTestAPIToken(t, admin, "legacy-wildcard")
	legacyClient := &testClient{t: t, server: server, bearer: legacyWildcard}
	if code := legacyClient.do(http.MethodGet, "/api/open/v1/subnest/mailboxes", nil, &map[string]any{}); code != http.StatusForbidden {
		t.Fatalf("legacy wildcard gained subnest scope code=%d", code)
	}
	if _, err := a.db.Exec(`UPDATE users SET role='user' WHERE email='admin@lanqin.local'`); err != nil {
		t.Fatal(err)
	}
	if code := client.do(http.MethodGet, "/api/open/v1/subnest/mailboxes", nil, &map[string]any{}); code != http.StatusForbidden {
		t.Fatalf("demoted administrator retained subnest access code=%d", code)
	}
}

func insertSubNestTestMessage(t *testing.T, a *App, mailbox *Mailbox, folderID, subject string, receivedAt time.Time, withAttachment bool) string {
	t.Helper()
	attachments := []AttachmentInput(nil)
	if withAttachment {
		attachments = []AttachmentInput{{Filename: "subnest.txt", ContentType: "text/plain", ContentBase64: base64.StdEncoding.EncodeToString([]byte("subnest attachment"))}}
	}
	id, err := a.insertMessage(t.Context(), storedMessage{
		MailboxID: mailbox.ID, FolderID: folderID, RecipientAddr: mailbox.Address,
		MessageUID: newID("uid"), MessageID: "<" + newID("subnest") + "@example.test>", Subject: subject,
		From: "sender@example.test", FromName: "Sender", To: []string{mailbox.Address}, SentAt: receivedAt, ReceivedAt: receivedAt,
		Snippet: "subnest integration test", BodyText: "test body", BodyHTML: "<p>test body</p>",
	}, attachments)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func subNestMailboxesContain(items []subNestMailbox, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}
