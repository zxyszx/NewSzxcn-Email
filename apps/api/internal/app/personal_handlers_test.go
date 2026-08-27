package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestContactsLifecycleAndOwnership(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()

	admin := &testClient{t: t, server: ts}
	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("admin login code=%d body=%v", code, login)
	}

	var createdUser AdminUser
	if code := admin.do("POST", "/api/admin/users", map[string]any{
		"email":       "contact-owner@lanqin.local",
		"displayName": "Contact Owner",
		"role":        "user",
		"password":    "Password123!",
		"disabled":    false,
	}, &createdUser); code != http.StatusCreated {
		t.Fatalf("create user code=%d user=%+v", code, createdUser)
	}

	owner := &testClient{t: t, server: ts}
	if code := owner.do("POST", "/api/auth/login", map[string]string{"email": createdUser.Email, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("owner login code=%d body=%v", code, login)
	}

	var contact Contact
	if code := owner.do("POST", "/api/me/contacts", map[string]string{
		"name": "  Driver Friend  ", "email": "  Friend@Example.COM  ", "note": "  weekend group  ",
	}, &contact); code != http.StatusCreated {
		t.Fatalf("create contact code=%d contact=%+v", code, contact)
	}
	if contact.UserID != createdUser.ID || contact.Name != "Driver Friend" || contact.Email != "friend@example.com" || contact.Note != "weekend group" {
		t.Fatalf("normalized contact=%+v", contact)
	}

	var updated Contact
	if code := owner.do("POST", "/api/me/contacts", map[string]string{
		"name": "Updated Friend", "email": "friend@example.com", "note": "updated",
	}, &updated); code != http.StatusCreated {
		t.Fatalf("upsert contact code=%d contact=%+v", code, updated)
	}
	if updated.ID != contact.ID || updated.Name != "Updated Friend" || updated.Note != "updated" {
		t.Fatalf("updated contact=%+v original=%+v", updated, contact)
	}

	var list struct {
		Items []Contact `json:"items"`
	}
	if code := owner.do("GET", "/api/me/contacts", nil, &list); code != http.StatusOK || len(list.Items) != 1 || list.Items[0].ID != contact.ID {
		t.Fatalf("owner list code=%d items=%+v", code, list.Items)
	}

	if code := admin.do("GET", "/api/me/contacts", nil, &list); code != http.StatusOK || len(list.Items) != 0 {
		t.Fatalf("admin should not see owner contacts code=%d items=%+v", code, list.Items)
	}
	var errBody map[string]any
	if code := admin.do("DELETE", "/api/me/contacts/"+contact.ID, nil, &errBody); code != http.StatusNotFound {
		t.Fatalf("cross-user delete code=%d body=%v", code, errBody)
	}
	for _, invalid := range []string{"invalid", "missing@", "@example.com", "Friend <friend@example.com>"} {
		if code := owner.do("POST", "/api/me/contacts", map[string]string{"email": invalid}, &errBody); code != http.StatusBadRequest {
			t.Fatalf("invalid contact email %q code=%d body=%v", invalid, code, errBody)
		}
	}
	if code := owner.do("DELETE", "/api/me/contacts/"+contact.ID, nil, &errBody); code != http.StatusOK {
		t.Fatalf("delete contact code=%d body=%v", code, errBody)
	}
	if code := owner.do("GET", "/api/me/contacts", nil, &list); code != http.StatusOK || len(list.Items) != 0 {
		t.Fatalf("list after delete code=%d items=%+v", code, list.Items)
	}

	unauthenticated := &testClient{t: t, server: ts}
	if code := unauthenticated.do("GET", "/api/me/contacts", nil, &errBody); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated contacts code=%d body=%v", code, errBody)
	}
}
