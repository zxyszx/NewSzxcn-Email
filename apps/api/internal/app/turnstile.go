package app

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type turnstileVerifyResponse struct {
	Success    bool     `json:"success"`
	ErrorCodes []string `json:"error-codes"`
}

func (a *App) verifyTurnstile(ctx context.Context, token, remoteIP string) error {
	if !a.config().TurnstileEnabled {
		return nil
	}
	token = strings.TrimSpace(token)
	secret := strings.TrimSpace(a.config().TurnstileSecretKey)
	if secret == "" || token == "" {
		return errors.New("turnstile verification required")
	}
	form := url.Values{}
	form.Set("secret", secret)
	form.Set("response", token)
	if ip := normalizeRemoteIP(remoteIP); ip != "" {
		form.Set("remoteip", ip)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://challenges.cloudflare.com/turnstile/v0/siteverify", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 8 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	var out turnstileVerifyResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return err
	}
	if !out.Success {
		return errors.New("turnstile verification failed")
	}
	return nil
}

func normalizeRemoteIP(value string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(value))
	if err == nil {
		return host
	}
	return strings.TrimSpace(value)
}
