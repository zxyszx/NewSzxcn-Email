package app

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const maxMailEventStreamsPerUser = 8

// Signals only invalidate a user's cached mail. No email metadata enters the stream.
type mailEventHub struct {
	mu      sync.Mutex
	streams map[string]map[chan struct{}]struct{}
}

func (h *mailEventHub) subscribe(userID string) (<-chan struct{}, func(), bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if userID == "" || len(h.streams[userID]) >= maxMailEventStreamsPerUser {
		return nil, nil, false
	}
	if h.streams == nil {
		h.streams = make(map[string]map[chan struct{}]struct{})
	}
	if h.streams[userID] == nil {
		h.streams[userID] = make(map[chan struct{}]struct{})
	}
	ch := make(chan struct{}, 1)
	h.streams[userID][ch] = struct{}{}
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.streams[userID], ch)
		if len(h.streams[userID]) == 0 {
			delete(h.streams, userID)
		}
	}, true
}

func (h *mailEventHub) publish(userID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.streams[userID] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (a *App) notifyMailboxChanged(ctx context.Context, mailboxID string) {
	var userID string
	if err := a.db.QueryRowContext(ctx, `SELECT user_id FROM mailboxes WHERE id=? AND status='active'`, mailboxID).Scan(&userID); err == nil {
		a.mailEvents.publish(userID)
	}
}

func (a *App) handleEvents(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if !userHasPermission(user, PermissionMailRead) {
		respondError(w, http.StatusForbidden, "permission required")
		return
	}
	if _, ok := w.(http.Flusher); !ok {
		respondError(w, http.StatusInternalServerError, "streaming unavailable")
		return
	}
	changes, unsubscribe, ok := a.mailEvents.subscribe(user.ID)
	if !ok {
		w.Header().Set("Retry-After", "30")
		respondError(w, http.StatusTooManyRequests, "too many event streams")
		return
	}
	defer unsubscribe()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	write := func(event, data string) bool {
		_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
			return false
		}
		return controller.Flush() == nil
	}
	if !write("sync", `{"status":"connected"}`) {
		return
	}
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		var event, data string
		select {
		case <-r.Context().Done():
			return
		case <-changes:
			event, data = "sync", `{"status":"changed"}`
		case <-ticker.C:
			event, data = "heartbeat", `{"status":"alive"}`
		}
		// A long-lived connection must not outlive logout, expiry or permission revocation.
		current, err := a.authenticateRequest(r)
		if err != nil || current == nil || current.ID != user.ID || !userHasPermission(current, PermissionMailRead) {
			return
		}
		if !write(event, data) {
			return
		}
	}
}
