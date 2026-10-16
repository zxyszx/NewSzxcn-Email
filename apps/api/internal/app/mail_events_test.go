package app

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMailEventHubIsolationAndCoalescing(t *testing.T) {
	var hub mailEventHub
	first, cancelFirst, ok := hub.subscribe("first")
	if !ok {
		t.Fatal("subscribe failed")
	}
	defer cancelFirst()
	sibling, cancelSibling, _ := hub.subscribe("first")
	defer cancelSibling()
	second, cancelSecond, _ := hub.subscribe("second")
	defer cancelSecond()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); hub.publish("first") }()
	}
	wg.Wait()
	select {
	case <-first:
	default:
		t.Fatal("owner did not receive a signal")
	}
	select {
	case <-sibling:
	default:
		t.Fatal("owner's other stream did not receive a signal")
	}
	select {
	case <-first:
		t.Fatal("burst was not coalesced")
	case <-second:
		t.Fatal("signal leaked to another user")
	default:
	}
	cancelFirst()
	hub.publish("first")
	select {
	case <-first:
		t.Fatal("unsubscribed stream received a signal")
	default:
	}
}

func TestMailEventHubStreamLimitAndCleanup(t *testing.T) {
	var hub mailEventHub
	if _, _, ok := hub.subscribe(""); ok {
		t.Fatal("empty identity was accepted")
	}
	var cancels []func()
	for i := 0; i < maxMailEventStreamsPerUser; i++ {
		_, cancel, ok := hub.subscribe("user")
		if !ok {
			t.Fatal("limit reached too early")
		}
		cancels = append(cancels, cancel)
	}
	if _, _, ok := hub.subscribe("user"); ok {
		t.Fatal("stream limit not enforced")
	}
	for _, cancel := range cancels {
		cancel()
	}
	if len(hub.streams) != 0 {
		t.Fatal("disconnected streams retained")
	}
	_, cancel, ok := hub.subscribe("user")
	if !ok {
		t.Fatal("stream slot not released")
	}
	cancel()
}

func TestMailEventsDeliverLocalMailAndCloseAfterLogout(t *testing.T) {
	a := newTestApp(t)
	stopTestWorkers(a)
	ts := httptest.NewServer(a.Router())
	defer ts.Close()
	admin := &testClient{t: t, server: ts}
	var login map[string]any
	if code := admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@lanqin.local", "password": "ChangeMe123!"}, &login); code != http.StatusOK {
		t.Fatalf("login status=%d", code)
	}
	recipient := createTestMailbox(t, admin, mustDefaultDomainID(t, a), "events-recipient", "Event Recipient", "Password123!", nil)
	client := &testClient{t: t, server: ts}
	if code := client.do("POST", "/api/auth/login", map[string]string{"email": recipient.Address, "password": "Password123!"}, &login); code != http.StatusOK {
		t.Fatalf("recipient login status=%d", code)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/events", nil)
	req.AddCookie(client.cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || res.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("stream status=%d headers=%v", res.StatusCode, res.Header)
	}
	reader := bufio.NewReader(res.Body)
	readEvent := func() string {
		t.Helper()
		var event strings.Builder
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			event.WriteString(line)
			if line == "\n" {
				return event.String()
			}
		}
	}
	if got := readEvent(); got != "event: sync\ndata: {\"status\":\"connected\"}\n\n" {
		t.Fatalf("unexpected initial event %q", got)
	}
	other, unsubscribe, _ := a.mailEvents.subscribe("unrelated-user")
	defer unsubscribe()
	var sent MailMessage
	if code := admin.do("POST", "/api/mail/send", map[string]any{"to": []string{recipient.Address}, "subject": "private subject", "text": "private content"}, &sent); code != http.StatusCreated {
		t.Fatalf("send status=%d", code)
	}
	if got := readEvent(); got != "event: sync\ndata: {\"status\":\"changed\"}\n\n" {
		t.Fatalf("unexpected change event %q", got)
	}
	select {
	case <-other:
		t.Fatal("delivery notified an unrelated user")
	default:
	}
	var messages struct {
		Items []MailMessage `json:"items"`
	}
	if code := client.do("GET", "/api/mail/messages?folder=Inbox", nil, &messages); code != http.StatusOK || len(messages.Items) != 1 || messages.Items[0].Subject != "private subject" {
		t.Fatalf("mail was not available when signaled: status=%d count=%d", code, len(messages.Items))
	}
	if code := client.do("POST", "/api/auth/logout", nil, nil); code != http.StatusOK {
		t.Fatalf("logout status=%d", code)
	}
	a.notifyMailboxChanged(context.Background(), recipient.ID)
	if line, err := reader.ReadString('\n'); err == nil {
		t.Fatalf("logged-out stream remained readable: %q", line)
	}
	if ctx.Err() != nil {
		t.Fatal("stream only closed because test deadline expired")
	}
}

func TestMailEventsRequireAuthenticationAndReadPermission(t *testing.T) {
	a := newTestApp(t)
	unauthenticated := httptest.NewRecorder()
	a.Router().ServeHTTP(unauthenticated, httptest.NewRequest("GET", "/api/events", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d", unauthenticated.Code)
	}
	req := httptest.NewRequest("GET", "/api/events", nil)
	req = req.WithContext(context.WithValue(req.Context(), userContextKey, &User{ID: "no-mail-access", Role: "user"}))
	denied := httptest.NewRecorder()
	a.handleEvents(denied, req)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("missing permission status=%d", denied.Code)
	}
	admin, _ := defaultAdminUserAndMailbox(t, a)
	var cancels []func()
	for i := 0; i < maxMailEventStreamsPerUser; i++ {
		_, unsubscribe, _ := a.mailEvents.subscribe(admin.ID)
		cancels = append(cancels, unsubscribe)
	}
	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
	}()
	limited := httptest.NewRecorder()
	req = req.WithContext(context.WithValue(req.Context(), userContextKey, admin))
	a.handleEvents(limited, req)
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("stream limit status=%d", limited.Code)
	}
}
