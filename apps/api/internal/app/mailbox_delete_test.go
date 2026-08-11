package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminCannotDeleteOwnPrimaryMailbox(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}
	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("admin login=%d", code)
	}
	var mailboxes struct {
		Items []Mailbox `json:"items"`
	}
	if code := admin.do("GET", "/api/mail/mailboxes", nil, &mailboxes); code != http.StatusOK || len(mailboxes.Items) != 1 {
		t.Fatalf("mailboxes code=%d items=%d", code, len(mailboxes.Items))
	}
	if !mailboxes.Items[0].Primary {
		t.Fatal("administrator mailbox should be marked as primary")
	}
	if code := admin.do("DELETE", "/api/admin/mailboxes/"+mailboxes.Items[0].ID, nil, &map[string]any{}); code != http.StatusBadRequest {
		t.Fatalf("delete primary mailbox code=%d", code)
	}
	if code := admin.do("GET", "/api/mail/mailboxes", nil, &mailboxes); code != http.StatusOK || len(mailboxes.Items) != 1 {
		t.Fatalf("mailboxes after delete code=%d items=%d", code, len(mailboxes.Items))
	}
	var me map[string]any
	if code := admin.do("GET", "/api/me", nil, &me); code != http.StatusOK {
		t.Fatalf("account was not preserved code=%d", code)
	}
}
