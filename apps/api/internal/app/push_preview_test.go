package app

import (
	"net/http"
	"strings"
	"testing"
)

func TestPushPreviewSessionOwnershipAndPrivacy(t *testing.T) {
	a, client, _, mailbox, device := pushTestSetup(t)
	if got := client.do("POST", "/api/me/push-devices", device, nil); got != 200 {
		t.Fatal(got)
	}
	messageID := pushInsertTestMessage(t, a, mailbox, true)
	if _, err := a.db.Exec(`UPDATE messages SET subject=? WHERE id=?`, "Test\r\n\u202e"+strings.Repeat("题", 200), messageID); err != nil {
		t.Fatal(err)
	}
	var eventID string
	if err := a.db.QueryRow(`SELECT id FROM push_outbox WHERE message_id=?`, messageID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	path := "/api/me/push-events/" + eventID
	anonymous := &testClient{t: t, server: client.server}
	if got := anonymous.do("GET", path, nil, nil); got != 401 {
		t.Fatalf("anonymous=%d", got)
	}
	var preview map[string]string
	if got := client.do("GET", path, nil, &preview); got != 200 {
		t.Fatalf("owner=%d", got)
	}
	if len(preview) != 2 || preview["mailboxAddress"] != mailbox.Address || len([]rune(preview["subject"])) != 160 || strings.ContainsAny(preview["subject"], "\r\n\u202e") {
		t.Fatal("unsafe or excess preview fields")
	}
	var read int
	if err := a.db.QueryRow(`SELECT is_read FROM messages WHERE id=?`, messageID).Scan(&read); err != nil || read != 0 {
		t.Fatal("preview changed unread state")
	}
	secondSession := &testClient{t: t, server: client.server}
	if got := secondSession.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, nil); got != 200 {
		t.Fatal(got)
	}
	if got := secondSession.do("GET", path, nil, nil); got != 404 {
		t.Fatalf("different session=%d", got)
	}
	if got := client.do("GET", "/api/me/push-events/invalid", nil, nil); got != 404 {
		t.Fatal(got)
	}
	req, _ := http.NewRequest("GET", client.server.URL+path, nil)
	req.AddCookie(client.cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("preview may be cached")
	}
	if got := client.do("POST", "/api/auth/logout", nil, nil); got != 200 {
		t.Fatal(got)
	}
	if got := client.do("GET", path, nil, nil); got != 401 {
		t.Fatalf("logout=%d", got)
	}
}

func TestPushPreviewRejectsExpiredEventsAndInactiveMailboxes(t *testing.T) {
	a, client, _, mailbox, device := pushTestSetup(t)
	if got := client.do("POST", "/api/me/push-devices", device, nil); got != http.StatusOK {
		t.Fatal(got)
	}
	messageID := pushInsertTestMessage(t, a, mailbox, true)
	var eventID string
	if err := a.db.QueryRow(`SELECT id FROM push_outbox WHERE message_id=?`, messageID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	path := "/api/me/push-events/" + eventID
	if got := client.do("GET", path, nil, nil); got != http.StatusOK {
		t.Fatalf("active event=%d", got)
	}
	if _, err := a.db.Exec(`UPDATE push_outbox SET created_at=? WHERE id=?`, a.now().Unix()-3601, eventID); err != nil {
		t.Fatal(err)
	}
	if got := client.do("GET", path, nil, nil); got != http.StatusNotFound {
		t.Fatalf("expired event=%d", got)
	}
	if _, err := a.db.Exec(`UPDATE push_outbox SET created_at=? WHERE id=?`, a.now().Unix(), eventID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`UPDATE mailboxes SET status='disabled' WHERE id=?`, mailbox.ID); err != nil {
		t.Fatal(err)
	}
	if got := client.do("GET", path, nil, nil); got != http.StatusNotFound {
		t.Fatalf("inactive mailbox=%d", got)
	}
}
