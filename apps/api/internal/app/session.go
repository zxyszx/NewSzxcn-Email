package app

import (
	"net/http"
	"time"
)

func (a *App) issueSession(w http.ResponseWriter, r *http.Request, userID string) error {
	return a.issueSessionForHours(w, r, userID, a.config().SessionTTLHours)
}

// Remembering a device changes expiry only after the normal password/MFA checks.
// It uses the same server-revocable, HttpOnly session, never a stored password.
func (a *App) rememberDeviceHours() int {
	hours := a.config().RememberDeviceTTLHours
	if hours > 24*90 {
		hours = 24 * 90
	}
	if hours <= a.config().SessionTTLHours {
		return 0
	}
	return hours
}

func (a *App) issueLoginSession(w http.ResponseWriter, r *http.Request, userID string, remember bool) error {
	hours := a.config().SessionTTLHours
	if rememberedHours := a.rememberDeviceHours(); remember && rememberedHours > hours {
		hours = rememberedHours
	}
	return a.issueSessionForHours(w, r, userID, hours)
}

func (a *App) issueSessionForHours(w http.ResponseWriter, r *http.Request, userID string, hours int) error {
	token := randomToken()
	sessionID := newID("ses")
	lifetime := time.Duration(hours) * time.Hour
	expires := a.now().UTC().Add(lifetime)
	if _, err := a.db.ExecContext(r.Context(), `INSERT INTO sessions(id,user_id,token_hash,expires_at,created_at) VALUES(?,?,?,?,?)`,
		sessionID, userID, hashToken(token), expires.Format(time.RFC3339Nano), a.now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     a.config().CookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(lifetime.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   !a.config().AllowInsecureHTTP,
	})
	return nil
}
