package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInboxShareEnforcesFoldersTimeAndRevocation(t *testing.T) {
	a := newTestApp(t)
	a.updateConfig(func(cfg *Config) { cfg.UpdateServiceToken = "inbox-share-test-secret" })
	server := httptest.NewServer(a.Router())
	defer server.Close()
	client := &testClient{t: t, server: server}
	var login map[string]any
	if code := client.do(http.MethodPost, "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login code=%d body=%v", code, login)
	}
	_, mailbox := defaultAdminUserAndMailbox(t, a)
	var inboxID, archiveID string
	if err := a.db.QueryRow(`SELECT id FROM folders WHERE mailbox_id=? AND lower(name)='inbox'`, mailbox.ID).Scan(&inboxID); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow(`SELECT id FROM folders WHERE mailbox_id=? AND lower(name)='archive'`, mailbox.ID).Scan(&archiveID); err != nil {
		t.Fatal(err)
	}
	now := a.now().UTC()
	recentInbox := insertSharedInboxTestMessage(t, a, mailbox, inboxID, "recent inbox", now.Add(-5*time.Minute))
	oldInbox := insertSharedInboxTestMessage(t, a, mailbox, inboxID, "old inbox", now.Add(-2*time.Hour))
	recentArchive := insertSharedInboxTestMessage(t, a, mailbox, archiveID, "recent archive", now.Add(-4*time.Minute))

	var settings inboxShareSettingsResponse
	if code := client.do(http.MethodGet, "/api/me/mailboxes/"+mailbox.ID+"/inbox-share", nil, &settings); code != http.StatusOK {
		t.Fatalf("settings code=%d", code)
	}
	if settings.Enabled || len(settings.FolderIDs) != 1 || settings.FolderIDs[0] != inboxID {
		t.Fatalf("unexpected defaults: %+v", settings)
	}
	if code := client.do(http.MethodPost, "/api/me/mailboxes/"+mailbox.ID+"/inbox-share", map[string]any{"windowMinutes": 30, "folderIds": []string{inboxID}}, &settings); code != http.StatusCreated {
		t.Fatalf("create share code=%d settings=%+v", code, settings)
	}
	token := tokenFromShareURL(t, settings.ShareURL)
	var storedHash, storedCipher string
	if err := a.db.QueryRow(`SELECT token_hash,token_cipher FROM inbox_shares WHERE mailbox_id=?`, mailbox.ID).Scan(&storedHash, &storedCipher); err != nil {
		t.Fatal(err)
	}
	if storedHash == token || storedHash != hashToken(token) || storedCipher == "" || storedCipher == token {
		t.Fatalf("share token storage is not protected")
	}
	var reopened inboxShareSettingsResponse
	if code := client.do(http.MethodGet, "/api/me/mailboxes/"+mailbox.ID+"/inbox-share", nil, &reopened); code != http.StatusOK || reopened.ShareURL != settings.ShareURL {
		t.Fatalf("share link was not available after reopening: code=%d got=%q want=%q", code, reopened.ShareURL, settings.ShareURL)
	}

	public := &testClient{t: t, server: server, bearer: token}
	var page struct {
		Items []sharedInboxMessage `json:"items"`
	}
	if code := public.do(http.MethodGet, "/api/shared-inbox", nil, &page); code != http.StatusOK {
		t.Fatalf("public list code=%d", code)
	}
	if !sharedInboxItemsContain(page.Items, recentInbox) || sharedInboxItemsContain(page.Items, oldInbox) || sharedInboxItemsContain(page.Items, recentArchive) {
		t.Fatalf("public list bypassed time or folder scope: %+v", page.Items)
	}
	for _, id := range []string{oldInbox, recentArchive} {
		var body map[string]any
		if code := public.do(http.MethodGet, "/api/shared-inbox/messages/"+id, nil, &body); code != http.StatusNotFound {
			t.Fatalf("out-of-scope message %s code=%d", id, code)
		}
	}

	if code := client.do(http.MethodPut, "/api/me/mailboxes/"+mailbox.ID+"/inbox-share", map[string]any{"windowMinutes": 1440, "folderIds": []string{inboxID, archiveID}}, &settings); code != http.StatusOK {
		t.Fatalf("update share code=%d", code)
	}
	if code := public.do(http.MethodGet, "/api/shared-inbox", nil, &page); code != http.StatusOK || !sharedInboxItemsContain(page.Items, oldInbox) || !sharedInboxItemsContain(page.Items, recentArchive) {
		t.Fatalf("updated share list code=%d items=%+v", code, page.Items)
	}

	oldToken := token
	if code := client.do(http.MethodPost, "/api/me/mailboxes/"+mailbox.ID+"/inbox-share", map[string]any{"windowMinutes": 1440, "folderIds": []string{inboxID, archiveID}}, &settings); code != http.StatusCreated {
		t.Fatalf("reset share code=%d", code)
	}
	public.bearer = oldToken
	var errorBody map[string]any
	if code := public.do(http.MethodGet, "/api/shared-inbox", nil, &errorBody); code != http.StatusNotFound {
		t.Fatalf("old token remained valid after reset: %d", code)
	}
	public.bearer = tokenFromShareURL(t, settings.ShareURL)
	if code := public.do(http.MethodGet, "/api/shared-inbox", nil, &page); code != http.StatusOK {
		t.Fatalf("new token code=%d", code)
	}
	if code := client.do(http.MethodDelete, "/api/me/mailboxes/"+mailbox.ID+"/inbox-share", nil, &errorBody); code != http.StatusOK {
		t.Fatalf("delete share code=%d", code)
	}
	if code := public.do(http.MethodGet, "/api/shared-inbox", nil, &errorBody); code != http.StatusNotFound {
		t.Fatalf("token remained valid after disable: %d", code)
	}
}

func TestInboxShareRequiresPermissionAndRevocationInvalidatesLink(t *testing.T) {
	a := newTestApp(t)
	a.updateConfig(func(cfg *Config) { cfg.UpdateServiceToken = "inbox-share-test-secret" })
	server := httptest.NewServer(a.Router())
	defer server.Close()
	admin := &testClient{t: t, server: server}
	var login map[string]any
	if code := admin.do(http.MethodPost, "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("admin login code=%d", code)
	}
	var group PermissionGroup
	if code := admin.do(http.MethodPost, "/api/admin/permission-groups", map[string]any{
		"name": "Inbox sharing", "description": "test", "permissions": []string{PermissionInboxShare}, "limits": defaultPermissionLimits(),
	}, &group); code != http.StatusCreated {
		t.Fatalf("create permission group code=%d group=%+v", code, group)
	}
	var account AdminUser
	if code := admin.do(http.MethodPost, "/api/admin/users", map[string]any{
		"email": "share-user@lanqin.local", "displayName": "Share User", "password": "SharePassword123!", "role": "user", "permissionGroupIds": []string{group.ID},
	}, &account); code != http.StatusCreated {
		t.Fatalf("create user code=%d user=%+v", code, account)
	}
	userClient := &testClient{t: t, server: server}
	if code := userClient.do(http.MethodPost, "/api/auth/login", map[string]string{"email": account.Email, "password": "SharePassword123!"}, &login); code != http.StatusOK {
		t.Fatalf("user login code=%d", code)
	}
	mailbox, err := a.mailboxByAddress(context.Background(), account.Email)
	if err != nil {
		t.Fatal(err)
	}
	var inboxID string
	if err := a.db.QueryRow(`SELECT id FROM folders WHERE mailbox_id=? AND lower(name)='inbox'`, mailbox.ID).Scan(&inboxID); err != nil {
		t.Fatal(err)
	}
	var settings inboxShareSettingsResponse
	if code := userClient.do(http.MethodPost, "/api/me/mailboxes/"+mailbox.ID+"/inbox-share", map[string]any{"windowMinutes": 30, "folderIds": []string{inboxID}}, &settings); code != http.StatusCreated {
		t.Fatalf("authorized user create code=%d", code)
	}
	public := &testClient{t: t, server: server, bearer: tokenFromShareURL(t, settings.ShareURL)}
	var response map[string]any
	if code := public.do(http.MethodGet, "/api/shared-inbox", nil, &response); code != http.StatusOK {
		t.Fatalf("shared link before revocation code=%d", code)
	}
	if _, err := a.db.Exec(`DELETE FROM user_permission_groups WHERE user_id=? AND group_id=?`, account.ID, group.ID); err != nil {
		t.Fatal(err)
	}
	if code := userClient.do(http.MethodGet, "/api/me/mailboxes/"+mailbox.ID+"/inbox-share", nil, &response); code != http.StatusForbidden {
		t.Fatalf("management API after revocation code=%d", code)
	}
	if code := public.do(http.MethodGet, "/api/shared-inbox", nil, &response); code != http.StatusNotFound {
		t.Fatalf("shared link after permission revocation code=%d", code)
	}
}

func sharedInboxItemsContain(items []sharedInboxMessage, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func insertSharedInboxTestMessage(t *testing.T, a *App, mailbox *Mailbox, folderID, subject string, receivedAt time.Time) string {
	t.Helper()
	id, err := a.insertMessage(context.Background(), storedMessage{
		MailboxID: mailbox.ID, FolderID: folderID, RecipientAddr: mailbox.Address,
		MessageUID: newID("uid"), MessageID: "<" + newID("shared") + "@example.test>", Subject: subject,
		From: "sender@example.test", FromName: "Sender", To: []string{mailbox.Address}, SentAt: receivedAt, ReceivedAt: receivedAt,
		Snippet: "shared inbox test", BodyText: "test body", BodyHTML: "<p>test body</p>",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func tokenFromShareURL(t *testing.T, value string) string {
	t.Helper()
	parts := strings.SplitN(value, "#", 2)
	if len(parts) != 2 || !strings.HasPrefix(parts[1], "nis_") {
		t.Fatalf("invalid share URL: %q", value)
	}
	return parts[1]
}
