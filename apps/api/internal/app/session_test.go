package app

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRememberDeviceExpiryAndRevocation(t *testing.T) {
	a := newTestApp(t)
	stopTestWorkers(a)
	a.updateConfig(func(cfg *Config) { cfg.RememberDeviceTTLHours = 24 * 90 })
	base := time.Now().UTC()
	var elapsed atomic.Int64
	a.now = func() time.Time { return base.Add(time.Duration(elapsed.Load())) }
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	login := func(remember bool) *testClient {
		c := &testClient{t: t, server: ts}
		if code := c.do("POST", "/api/auth/login", map[string]any{"email": "admin@lanqin.local", "password": "ChangeMe123!", "rememberDevice": remember}, nil); code != http.StatusOK {
			t.Fatalf("login status=%d", code)
		}
		return c
	}
	regular, remembered := login(false), login(true)
	if regular.cookie.MaxAge != 24*3600 || remembered.cookie.MaxAge != 90*24*3600 {
		t.Fatalf("wrong cookie lifetimes: regular=%d remembered=%d", regular.cookie.MaxAge, remembered.cookie.MaxAge)
	}
	if !remembered.cookie.HttpOnly || remembered.cookie.SameSite != http.SameSiteLaxMode {
		t.Fatal("persistent session lost cookie protections")
	}
	elapsed.Store(int64(48 * time.Hour))
	if status := regular.do("GET", "/api/me", nil, nil); status != http.StatusUnauthorized {
		t.Fatalf("normal session did not expire: %d", status)
	}
	if status := remembered.do("GET", "/api/me", nil, nil); status != http.StatusOK {
		t.Fatalf("remembered device lost login after ordinary expiry: %d", status)
	}
	// A new client/process can reuse the saved cookie without storing a password.
	reopened := &testClient{t: t, server: ts, cookie: remembered.cookie}
	if status := reopened.do("GET", "/api/me", nil, nil); status != http.StatusOK {
		t.Fatalf("restored device status=%d", status)
	}
	if status := remembered.do("POST", "/api/auth/logout", nil, nil); status != http.StatusOK {
		t.Fatalf("logout status=%d", status)
	}
	if status := reopened.do("GET", "/api/me", nil, nil); status != http.StatusUnauthorized {
		t.Fatalf("logged-out cookie can be replayed: %d", status)
	}
	elapsed.Store(0)
	remembered = login(true)
	elapsed.Store(int64(91 * 24 * time.Hour))
	if status := remembered.do("GET", "/api/me", nil, nil); status != http.StatusUnauthorized {
		t.Fatalf("remembered device did not expire: %d", status)
	}
}

func TestRememberDevicePolicyAndPasswordRevocation(t *testing.T) {
	a := newTestApp(t)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	for _, tc := range []struct{ configured, want int }{{0, 0}, {-1, 0}, {12, 0}, {720, 720}, {99999, 2160}} {
		a.updateConfig(func(cfg *Config) { cfg.RememberDeviceTTLHours = tc.configured })
		var settings PublicSettings
		c := &testClient{t: t, server: ts}
		if status := c.do("GET", "/api/public/settings", nil, &settings); status != http.StatusOK || settings.RememberDeviceHours != tc.want {
			t.Fatalf("policy=%d status=%d hours=%d", tc.configured, status, settings.RememberDeviceHours)
		}
	}
	login := map[string]any{"email": "admin@lanqin.local", "password": "ChangeMe123!", "rememberDevice": true}
	device := &testClient{t: t, server: ts}
	if status := device.do("POST", "/api/auth/login", login, nil); status != http.StatusOK {
		t.Fatalf("login status=%d", status)
	}
	stolen := &testClient{t: t, server: ts, cookie: device.cookie}
	if status := device.do("POST", "/api/me/password", map[string]string{"currentPassword": "ChangeMe123!", "newPassword": "NewPassword123!"}, nil); status != http.StatusOK {
		t.Fatalf("password change status=%d", status)
	}
	if status := stolen.do("GET", "/api/me", nil, nil); status != http.StatusUnauthorized {
		t.Fatalf("password change did not revoke remembered cookie: %d", status)
	}
}
