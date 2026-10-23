package app

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	authRateWindow = 15 * time.Minute
	authBlockTime  = 15 * time.Minute
)

type authRateTarget struct {
	key   string
	limit int
}

func loginRateTargets(login, remoteAddr string) []authRateTarget {
	targets := []authRateTarget{{key: "ip:" + requestSourceIP(remoteAddr), limit: 30}}
	if login = strings.ToLower(strings.TrimSpace(login)); login != "" {
		targets = append(targets, authRateTarget{key: "account:" + login, limit: 5})
	}
	return targets
}

func challengeRateTargets(token, remoteAddr string) []authRateTarget {
	return []authRateTarget{
		{key: "ip:" + requestSourceIP(remoteAddr), limit: 30},
		{key: "challenge:" + hashToken(strings.TrimSpace(token)), limit: 5},
	}
}

func requestSourceIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(remoteAddr)
}

func (a *App) checkAuthRateLimit(ctx context.Context, targets []authRateTarget) (time.Duration, error) {
	now := a.now().UTC()
	var retryAfter time.Duration
	for _, target := range targets {
		var blockedUntilText string
		err := a.db.QueryRowContext(ctx, `SELECT blocked_until FROM auth_rate_limits WHERE key_hash=?`, hashToken(target.key)).Scan(&blockedUntilText)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return 0, err
		}
		blockedUntil := parseTime(blockedUntilText)
		if remaining := blockedUntil.Sub(now); remaining > retryAfter {
			retryAfter = remaining
		}
	}
	return retryAfter, nil
}

func (a *App) recordAuthFailure(ctx context.Context, targets []authRateTarget) (time.Duration, error) {
	now := a.now().UTC()
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var retryAfter time.Duration
	for _, target := range targets {
		keyHash := hashToken(target.key)
		var failures int
		var windowText, blockedText string
		err := tx.QueryRowContext(ctx, `SELECT failures,window_started_at,blocked_until FROM auth_rate_limits WHERE key_hash=?`, keyHash).Scan(&failures, &windowText, &blockedText)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
		windowStart := parseTime(windowText)
		blockedUntil := parseTime(blockedText)
		if errors.Is(err, sql.ErrNoRows) || windowStart.IsZero() || now.Sub(windowStart) >= authRateWindow {
			failures = 0
			windowStart = now
			blockedUntil = time.Time{}
		}
		failures++
		if failures >= target.limit && !blockedUntil.After(now) {
			blockedUntil = now.Add(authBlockTime)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO auth_rate_limits(key_hash,failures,window_started_at,blocked_until,updated_at)
			VALUES(?,?,?,?,?) ON CONFLICT(key_hash) DO UPDATE SET failures=excluded.failures,window_started_at=excluded.window_started_at,blocked_until=excluded.blocked_until,updated_at=excluded.updated_at`,
			keyHash, failures, windowStart.Format(time.RFC3339Nano), formatOptionalTime(blockedUntil), now.Format(time.RFC3339Nano)); err != nil {
			return 0, err
		}
		if remaining := blockedUntil.Sub(now); remaining > retryAfter {
			retryAfter = remaining
		}
	}
	_, _ = tx.ExecContext(ctx, `DELETE FROM auth_rate_limits WHERE updated_at<?`, now.Add(-24*time.Hour).Format(time.RFC3339Nano))
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return retryAfter, nil
}

func (a *App) clearAuthRateLimits(ctx context.Context, targets ...authRateTarget) {
	for _, target := range targets {
		_, _ = a.db.ExecContext(ctx, `DELETE FROM auth_rate_limits WHERE key_hash=?`, hashToken(target.key))
	}
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339Nano)
}

func respondAuthRateLimited(w http.ResponseWriter, retryAfter time.Duration) {
	seconds := int(math.Ceil(retryAfter.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	respondError(w, http.StatusTooManyRequests, "尝试次数过多，请稍后再试")
}
