package app

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type SystemSettings struct {
	PublicHostname                     string   `json:"publicHostname"`
	PublicBaseURL                      string   `json:"publicBaseUrl"`
	SMTPHost                           string   `json:"smtpHost"`
	SMTPPort                           string   `json:"smtpPort"`
	SMTPUsername                       string   `json:"smtpUsername"`
	SMTPPasswordSet                    bool     `json:"smtpPasswordSet"`
	SMTPRequireTLS                     bool     `json:"smtpRequireTls"`
	MaildirRoot                        string   `json:"maildirRoot"`
	MaildirScanSeconds                 int      `json:"maildirScanSeconds"`
	SessionTTLHours                    int      `json:"sessionTtlHours"`
	AllowInsecureHTTP                  bool     `json:"allowInsecureHttp"`
	OpenRegistration                   bool     `json:"openRegistration"`
	TwoFactorEnabled                   bool     `json:"twoFactorEnabled"`
	TurnstileEnabled                   bool     `json:"turnstileEnabled"`
	TurnstileSiteKey                   string   `json:"turnstileSiteKey"`
	TurnstileSecretSet                 bool     `json:"turnstileSecretSet"`
	CatchAllEnabled                    bool     `json:"catchAllEnabled"`
	MailAutoRefresh                    bool     `json:"mailAutoRefresh"`
	MailRefreshSeconds                 int      `json:"mailRefreshSeconds"`
	UserMailboxApplyEnabled            bool     `json:"userMailboxApplyEnabled"`
	UserMailboxDomainIDs               []string `json:"userMailboxDomainIds"`
	ReservedMailboxPrefixes            string   `json:"reservedMailboxPrefixes"`
	ExternalIMAPEnabled                bool     `json:"externalImapEnabled"`
	ExternalIMAPSecretSet              bool     `json:"externalImapSecretSet"`
	ExternalIMAPSyncSeconds            int      `json:"externalImapSyncSeconds"`
	ExternalIMAPAllowPrivateHosts      bool     `json:"externalImapAllowPrivateHosts"`
	ExternalIMAPGmailClientID          string   `json:"externalImapGmailClientId"`
	ExternalIMAPGmailClientSecretSet   bool     `json:"externalImapGmailClientSecretSet"`
	ExternalIMAPOutlookClientID        string   `json:"externalImapOutlookClientId"`
	ExternalIMAPOutlookClientSecretSet bool     `json:"externalImapOutlookClientSecretSet"`
	TelegramMailEnabled                bool     `json:"telegramMailEnabled"`
	TelegramBotTokenSet                bool     `json:"telegramBotTokenSet"`
	TelegramPrivateChatID              string   `json:"telegramPrivateChatId"`
	TelegramBodyMode                   string   `json:"telegramBodyMode"`
	TelegramMailboxIDs                 []string `json:"telegramMailboxIds"`
	TelegramIncludeUnregistered        bool     `json:"telegramIncludeUnregistered"`
}

type systemSettingsUpdate struct {
	PublicHostname                  string   `json:"publicHostname"`
	PublicBaseURL                   string   `json:"publicBaseUrl"`
	SMTPHost                        string   `json:"smtpHost"`
	SMTPPort                        string   `json:"smtpPort"`
	SMTPUsername                    string   `json:"smtpUsername"`
	SMTPPassword                    string   `json:"smtpPassword"`
	SMTPRequireTLS                  bool     `json:"smtpRequireTls"`
	MaildirRoot                     string   `json:"maildirRoot"`
	MaildirScanSeconds              int      `json:"maildirScanSeconds"`
	SessionTTLHours                 int      `json:"sessionTtlHours"`
	AllowInsecureHTTP               bool     `json:"allowInsecureHttp"`
	OpenRegistration                bool     `json:"openRegistration"`
	TwoFactorEnabled                bool     `json:"twoFactorEnabled"`
	TurnstileEnabled                bool     `json:"turnstileEnabled"`
	TurnstileSiteKey                string   `json:"turnstileSiteKey"`
	TurnstileSecretKey              string   `json:"turnstileSecretKey"`
	CatchAllEnabled                 bool     `json:"catchAllEnabled"`
	MailAutoRefresh                 bool     `json:"mailAutoRefresh"`
	MailRefreshSeconds              int      `json:"mailRefreshSeconds"`
	UserMailboxApplyEnabled         bool     `json:"userMailboxApplyEnabled"`
	UserMailboxDomainIDs            []string `json:"userMailboxDomainIds"`
	ReservedMailboxPrefixes         string   `json:"reservedMailboxPrefixes"`
	ExternalIMAPEnabled             bool     `json:"externalImapEnabled"`
	ExternalIMAPSecretKey           string   `json:"externalImapSecretKey"`
	ExternalIMAPSyncSeconds         int      `json:"externalImapSyncSeconds"`
	ExternalIMAPAllowPrivateHosts   bool     `json:"externalImapAllowPrivateHosts"`
	ExternalIMAPGmailClientID       string   `json:"externalImapGmailClientId"`
	ExternalIMAPGmailClientSecret   string   `json:"externalImapGmailClientSecret"`
	ExternalIMAPOutlookClientID     string   `json:"externalImapOutlookClientId"`
	ExternalIMAPOutlookClientSecret string   `json:"externalImapOutlookClientSecret"`
	TelegramMailEnabled             bool     `json:"telegramMailEnabled"`
	TelegramBotToken                string   `json:"telegramBotToken"`
	TelegramPrivateChatID           string   `json:"telegramPrivateChatId"`
	TelegramBodyMode                string   `json:"telegramBodyMode"`
	TelegramMailboxIDs              []string `json:"telegramMailboxIds"`
	TelegramIncludeUnregistered     bool     `json:"telegramIncludeUnregistered"`
}

type PublicSettings struct {
	OpenRegistration    bool           `json:"openRegistration"`
	TurnstileEnabled    bool           `json:"turnstileEnabled"`
	TurnstileSiteKey    string         `json:"turnstileSiteKey"`
	PublicHostname      string         `json:"publicHostname"`
	MailAutoRefresh     bool           `json:"mailAutoRefresh"`
	MailRefreshMs       int            `json:"mailRefreshMs"`
	ExternalIMAPEnabled bool           `json:"externalImapEnabled"`
	MailboxDomains      []PublicDomain `json:"mailboxDomains,omitempty"`
}

type PublicDomain struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type smtpTestRequest struct {
	To string `json:"to"`
}

func (a *App) handleGetSystemSettings(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, a.systemSettingsSnapshot())
}

func (a *App) handlePublicSettings(w http.ResponseWriter, r *http.Request) {
	cfg := a.config()
	enabled := cfg.TurnstileEnabled && strings.TrimSpace(cfg.TurnstileSiteKey) != "" && strings.TrimSpace(cfg.TurnstileSecretKey) != ""
	refreshSeconds := cfg.MailRefreshSeconds
	if refreshSeconds <= 0 {
		refreshSeconds = 30
	}
	settings := PublicSettings{OpenRegistration: cfg.OpenRegistration, TurnstileEnabled: enabled, TurnstileSiteKey: cfg.TurnstileSiteKey, PublicHostname: cfg.PublicHostname, MailAutoRefresh: cfg.MailAutoRefresh, MailRefreshMs: refreshSeconds * 1000, ExternalIMAPEnabled: cfg.ExternalIMAPEnabled}

	// Include available domains for mailbox creation during registration
	if cfg.OpenRegistration {
		rows, err := a.db.QueryContext(r.Context(), `SELECT id, name FROM domains WHERE status='active' ORDER BY name`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var d PublicDomain
				if err := rows.Scan(&d.ID, &d.Name); err != nil {
					continue
				}
				settings.MailboxDomains = append(settings.MailboxDomains, d)
			}
		}
	}

	respondJSON(w, http.StatusOK, settings)
}

func (a *App) handleUpdateSystemSettings(w http.ResponseWriter, r *http.Request) {
	a.telegramDeliveryMu.Lock()
	defer a.telegramDeliveryMu.Unlock()
	var req systemSettingsUpdate
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	next := a.config()
	next.PublicHostname = normalizeHostname(req.PublicHostname)
	if next.PublicHostname == "" {
		badRequest(w, errors.New("publicHostname is required"))
		return
	}
	next.PublicBaseURL = strings.TrimSpace(req.PublicBaseURL)
	next.SMTPHost = strings.TrimSpace(req.SMTPHost)
	next.SMTPPort = strings.TrimSpace(req.SMTPPort)
	if next.SMTPPort == "" {
		next.SMTPPort = "25"
	}
	if _, err := strconv.Atoi(next.SMTPPort); err != nil {
		badRequest(w, errors.New("smtpPort must be a number"))
		return
	}
	next.SMTPUsername = strings.TrimSpace(req.SMTPUsername)
	if strings.TrimSpace(req.SMTPPassword) != "" {
		next.SMTPPassword = req.SMTPPassword
	}
	next.SMTPRequireTLS = req.SMTPRequireTLS
	next.MaildirRoot = strings.TrimSpace(req.MaildirRoot)
	if req.MaildirScanSeconds <= 0 {
		req.MaildirScanSeconds = 30
	}
	next.MaildirScanSeconds = req.MaildirScanSeconds
	if req.SessionTTLHours <= 0 {
		req.SessionTTLHours = 24 * 7
	}
	next.SessionTTLHours = req.SessionTTLHours
	next.AllowInsecureHTTP = req.AllowInsecureHTTP
	next.OpenRegistration = req.OpenRegistration
	next.TwoFactorEnabled = req.TwoFactorEnabled
	next.TurnstileEnabled = req.TurnstileEnabled
	next.TurnstileSiteKey = strings.TrimSpace(req.TurnstileSiteKey)
	if strings.TrimSpace(req.TurnstileSecretKey) != "" {
		next.TurnstileSecretKey = strings.TrimSpace(req.TurnstileSecretKey)
	}
	if next.TurnstileEnabled && (next.TurnstileSiteKey == "" || next.TurnstileSecretKey == "") {
		badRequest(w, errors.New("turnstile keys are required when enabled"))
		return
	}
	next.CatchAllEnabled = req.CatchAllEnabled
	next.MailAutoRefresh = req.MailAutoRefresh
	if req.MailRefreshSeconds <= 0 {
		req.MailRefreshSeconds = 30
	}
	next.MailRefreshSeconds = req.MailRefreshSeconds
	next.UserMailboxApplyEnabled = req.UserMailboxApplyEnabled
	next.UserMailboxDomainIDs = strings.Join(cleanIDList(req.UserMailboxDomainIDs), ",")
	next.ReservedMailboxPrefixes = strings.Join(parseReservedPrefixes(req.ReservedMailboxPrefixes), ",")
	next.ExternalIMAPEnabled = req.ExternalIMAPEnabled
	if strings.TrimSpace(req.ExternalIMAPSecretKey) != "" {
		next.ExternalIMAPSecretKey = strings.TrimSpace(req.ExternalIMAPSecretKey)
	}
	if req.ExternalIMAPSyncSeconds <= 0 {
		req.ExternalIMAPSyncSeconds = 300
	}
	next.ExternalIMAPSyncSeconds = req.ExternalIMAPSyncSeconds
	next.ExternalIMAPAllowPrivateHosts = req.ExternalIMAPAllowPrivateHosts
	next.ExternalIMAPGmailClientID = strings.TrimSpace(req.ExternalIMAPGmailClientID)
	if strings.TrimSpace(req.ExternalIMAPGmailClientSecret) != "" {
		next.ExternalIMAPGmailClientSecret = strings.TrimSpace(req.ExternalIMAPGmailClientSecret)
	}
	next.ExternalIMAPOutlookClientID = strings.TrimSpace(req.ExternalIMAPOutlookClientID)
	if strings.TrimSpace(req.ExternalIMAPOutlookClientSecret) != "" {
		next.ExternalIMAPOutlookClientSecret = strings.TrimSpace(req.ExternalIMAPOutlookClientSecret)
	}
	if next.ExternalIMAPEnabled && strings.TrimSpace(next.ExternalIMAPSecretKey) == "" {
		badRequest(w, errors.New("外部 IMAP 加密密钥未设置"))
		return
	}
	next.TelegramMailEnabled = req.TelegramMailEnabled
	if strings.TrimSpace(req.TelegramBotToken) != "" {
		next.TelegramBotToken = strings.TrimSpace(req.TelegramBotToken)
	}
	next.TelegramPrivateChatID = strings.TrimSpace(req.TelegramPrivateChatID)
	next.TelegramBodyMode = normalizeTelegramBodyMode(req.TelegramBodyMode)
	next.TelegramMailboxIDs = strings.Join(a.activeTelegramMailboxIDs(r.Context(), req.TelegramMailboxIDs), ",")
	next.TelegramIncludeUnregistered = req.TelegramIncludeUnregistered
	if next.TelegramMailEnabled {
		if next.TelegramBotToken == "" {
			badRequest(w, errors.New("Telegram Bot Token 未设置"))
			return
		}
		if !validTelegramPrivateChatID(next.TelegramPrivateChatID) {
			badRequest(w, errors.New("Telegram 私聊 Chat ID 无效"))
			return
		}
		if next.TelegramMailboxIDs == "" && !next.TelegramIncludeUnregistered {
			badRequest(w, errors.New("请至少选择一个 Telegram 通知邮箱或开启未知收件通知"))
			return
		}
	}

	if err := a.saveSystemSettings(r.Context(), next, false); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save settings")
		return
	}
	a.setConfig(next)
	respondJSON(w, http.StatusOK, a.systemSettingsSnapshot())
}

func (a *App) handleTestSMTP(w http.ResponseWriter, r *http.Request) {
	var req smtpTestRequest
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	cfg := a.config()
	if strings.TrimSpace(cfg.SMTPHost) == "" {
		badRequest(w, errors.New("SMTP 主机未设置"))
		return
	}
	if strings.TrimSpace(cfg.SMTPPort) == "" {
		cfg.SMTPPort = "25"
	}
	if _, err := strconv.Atoi(cfg.SMTPPort); err != nil {
		badRequest(w, errors.New("SMTP 端口无效"))
		return
	}
	to := normalizeEmail(req.To)
	if to == "" || !strings.Contains(to, "@") {
		badRequest(w, errors.New("收件邮箱无效"))
		return
	}
	from := cfg.AdminEmail
	if user := currentUser(r); user != nil && strings.Contains(user.Email, "@") {
		from = user.Email
	}
	if strings.TrimSpace(from) == "" || !strings.Contains(from, "@") {
		badRequest(w, errors.New("发件邮箱无效"))
		return
	}
	domain := cfg.PublicHostname
	if parts := strings.SplitN(from, "@", 2); len(parts) == 2 && parts[1] != "" {
		domain = parts[1]
	}
	if domain == "" {
		domain = "lanqin.local"
	}
	now := a.now().UTC()
	subject := "NewSzxcn 邮箱 SMTP 测试"
	bodyText := "这是一封 SMTP 测试邮件。"
	bodyHTML := "<p>这是一封 SMTP 测试邮件。</p>"
	if tpl, err := a.mailTemplate(r.Context(), smtpTestTemplateKey); err == nil {
		rendered := renderMailTemplate(tpl, templateRenderData{
			To:             to,
			From:           from,
			PublicHostname: cfg.PublicHostname,
			PublicBaseURL:  cfg.PublicBaseURL,
			Time:           now,
		})
		subject, bodyText, bodyHTML = rendered.Subject, rendered.Text, rendered.HTML
	}
	mimeBytes, err := BuildMIME(MIMEMessage{
		From:      from,
		To:        []string{to},
		Subject:   subject,
		Text:      bodyText,
		HTML:      bodyHTML,
		MessageID: "<" + newID("msg") + "@" + domain + ">",
		Date:      now,
	})
	if err != nil {
		badRequest(w, err)
		return
	}
	if err := sendSMTPWithConfig(cfg, from, []string{to}, mimeBytes); err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) systemSettingsSnapshot() SystemSettings {
	cfg := a.config()
	return SystemSettings{
		PublicHostname:                     cfg.PublicHostname,
		PublicBaseURL:                      cfg.PublicBaseURL,
		SMTPHost:                           cfg.SMTPHost,
		SMTPPort:                           cfg.SMTPPort,
		SMTPUsername:                       cfg.SMTPUsername,
		SMTPPasswordSet:                    strings.TrimSpace(cfg.SMTPPassword) != "",
		SMTPRequireTLS:                     cfg.SMTPRequireTLS,
		MaildirRoot:                        cfg.MaildirRoot,
		MaildirScanSeconds:                 cfg.MaildirScanSeconds,
		SessionTTLHours:                    cfg.SessionTTLHours,
		AllowInsecureHTTP:                  cfg.AllowInsecureHTTP,
		OpenRegistration:                   cfg.OpenRegistration,
		TwoFactorEnabled:                   cfg.TwoFactorEnabled,
		TurnstileEnabled:                   cfg.TurnstileEnabled,
		TurnstileSiteKey:                   cfg.TurnstileSiteKey,
		TurnstileSecretSet:                 strings.TrimSpace(cfg.TurnstileSecretKey) != "",
		CatchAllEnabled:                    cfg.CatchAllEnabled,
		MailAutoRefresh:                    cfg.MailAutoRefresh,
		MailRefreshSeconds:                 cfg.MailRefreshSeconds,
		UserMailboxApplyEnabled:            cfg.UserMailboxApplyEnabled,
		UserMailboxDomainIDs:               cleanIDList(strings.Split(cfg.UserMailboxDomainIDs, ",")),
		ReservedMailboxPrefixes:            strings.Join(parseReservedPrefixes(cfg.ReservedMailboxPrefixes), "\n"),
		ExternalIMAPEnabled:                cfg.ExternalIMAPEnabled,
		ExternalIMAPSecretSet:              strings.TrimSpace(cfg.ExternalIMAPSecretKey) != "",
		ExternalIMAPSyncSeconds:            cfg.ExternalIMAPSyncSeconds,
		ExternalIMAPAllowPrivateHosts:      cfg.ExternalIMAPAllowPrivateHosts,
		ExternalIMAPGmailClientID:          cfg.ExternalIMAPGmailClientID,
		ExternalIMAPGmailClientSecretSet:   strings.TrimSpace(cfg.ExternalIMAPGmailClientSecret) != "",
		ExternalIMAPOutlookClientID:        cfg.ExternalIMAPOutlookClientID,
		ExternalIMAPOutlookClientSecretSet: strings.TrimSpace(cfg.ExternalIMAPOutlookClientSecret) != "",
		TelegramMailEnabled:                cfg.TelegramMailEnabled,
		TelegramBotTokenSet:                strings.TrimSpace(cfg.TelegramBotToken) != "",
		TelegramPrivateChatID:              cfg.TelegramPrivateChatID,
		TelegramBodyMode:                   normalizeTelegramBodyMode(cfg.TelegramBodyMode),
		TelegramMailboxIDs:                 cleanIDList(strings.Split(cfg.TelegramMailboxIDs, ",")),
		TelegramIncludeUnregistered:        cfg.TelegramIncludeUnregistered,
	}
}

func (a *App) loadPersistedSystemSettings(ctx context.Context) error {
	cfg := a.config()
	rows, err := a.db.QueryContext(ctx, `SELECT key,value FROM system_settings`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return err
		}
		switch key {
		case "publicHostname":
			cfg.PublicHostname = value
		case "publicBaseUrl":
			cfg.PublicBaseURL = value
		case "smtpHost":
			cfg.SMTPHost = value
		case "smtpPort":
			cfg.SMTPPort = value
		case "smtpUsername":
			cfg.SMTPUsername = value
		case "smtpPassword":
			cfg.SMTPPassword = value
		case "smtpRequireTls":
			cfg.SMTPRequireTLS = value == "true"
		case "maildirRoot":
			cfg.MaildirRoot = value
		case "maildirScanSeconds":
			if n, err := strconv.Atoi(value); err == nil && n > 0 {
				cfg.MaildirScanSeconds = n
			}
		case "sessionTtlHours":
			if n, err := strconv.Atoi(value); err == nil && n > 0 {
				cfg.SessionTTLHours = n
			}
		case "allowInsecureHttp":
			cfg.AllowInsecureHTTP = value == "true"
		case "openRegistration":
			cfg.OpenRegistration = value == "true"
		case "twoFactorEnabled":
			cfg.TwoFactorEnabled = value == "true"
		case "turnstileEnabled":
			cfg.TurnstileEnabled = value == "true"
		case "turnstileSiteKey":
			cfg.TurnstileSiteKey = value
		case "turnstileSecretKey":
			cfg.TurnstileSecretKey = value
		case "catchAllEnabled":
			cfg.CatchAllEnabled = value == "true"
		case "mailAutoRefresh":
			cfg.MailAutoRefresh = value == "true"
		case "mailRefreshSeconds":
			if n, err := strconv.Atoi(value); err == nil && n > 0 {
				cfg.MailRefreshSeconds = n
			}
		case "userMailboxApplyEnabled":
			cfg.UserMailboxApplyEnabled = value == "true"
		case "userMailboxDomainIds":
			cfg.UserMailboxDomainIDs = value
		case "reservedMailboxPrefixes":
			cfg.ReservedMailboxPrefixes = value
		case "externalImapEnabled":
			cfg.ExternalIMAPEnabled = value == "true"
		case "externalImapSecretKey":
			cfg.ExternalIMAPSecretKey = value
		case "externalImapSyncSeconds":
			if n, err := strconv.Atoi(value); err == nil && n > 0 {
				cfg.ExternalIMAPSyncSeconds = n
			}
		case "externalImapAllowPrivateHosts":
			cfg.ExternalIMAPAllowPrivateHosts = value == "true"
		case "externalImapGmailClientId":
			cfg.ExternalIMAPGmailClientID = value
		case "externalImapGmailClientSecret":
			cfg.ExternalIMAPGmailClientSecret = value
		case "externalImapOutlookClientId":
			cfg.ExternalIMAPOutlookClientID = value
		case "externalImapOutlookClientSecret":
			cfg.ExternalIMAPOutlookClientSecret = value
		case "telegramMailEnabled":
			cfg.TelegramMailEnabled = value == "true"
		case "telegramBotToken":
			cfg.TelegramBotToken = value
		case "telegramPrivateChatId":
			cfg.TelegramPrivateChatID = value
		case "telegramBodyMode":
			cfg.TelegramBodyMode = normalizeTelegramBodyMode(value)
		case "telegramMailboxIds":
			cfg.TelegramMailboxIDs = strings.Join(cleanIDList(strings.Split(value, ",")), ",")
		case "telegramIncludeUnregistered":
			cfg.TelegramIncludeUnregistered = value == "true"
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	a.setConfig(cfg)
	return nil
}

func (a *App) saveSystemSettings(ctx context.Context, cfg Config, clearPendingTelegram bool) error {
	values := map[string]string{
		"publicHostname":                  cfg.PublicHostname,
		"publicBaseUrl":                   cfg.PublicBaseURL,
		"smtpHost":                        cfg.SMTPHost,
		"smtpPort":                        cfg.SMTPPort,
		"smtpUsername":                    cfg.SMTPUsername,
		"smtpPassword":                    cfg.SMTPPassword,
		"smtpRequireTls":                  strconv.FormatBool(cfg.SMTPRequireTLS),
		"maildirRoot":                     cfg.MaildirRoot,
		"maildirScanSeconds":              strconv.Itoa(cfg.MaildirScanSeconds),
		"sessionTtlHours":                 strconv.Itoa(cfg.SessionTTLHours),
		"allowInsecureHttp":               strconv.FormatBool(cfg.AllowInsecureHTTP),
		"openRegistration":                strconv.FormatBool(cfg.OpenRegistration),
		"twoFactorEnabled":                strconv.FormatBool(cfg.TwoFactorEnabled),
		"turnstileEnabled":                strconv.FormatBool(cfg.TurnstileEnabled),
		"turnstileSiteKey":                cfg.TurnstileSiteKey,
		"turnstileSecretKey":              cfg.TurnstileSecretKey,
		"catchAllEnabled":                 strconv.FormatBool(cfg.CatchAllEnabled),
		"mailAutoRefresh":                 strconv.FormatBool(cfg.MailAutoRefresh),
		"mailRefreshSeconds":              strconv.Itoa(cfg.MailRefreshSeconds),
		"userMailboxApplyEnabled":         strconv.FormatBool(cfg.UserMailboxApplyEnabled),
		"userMailboxDomainIds":            strings.Join(cleanIDList(strings.Split(cfg.UserMailboxDomainIDs, ",")), ","),
		"reservedMailboxPrefixes":         strings.Join(parseReservedPrefixes(cfg.ReservedMailboxPrefixes), ","),
		"externalImapEnabled":             strconv.FormatBool(cfg.ExternalIMAPEnabled),
		"externalImapSecretKey":           cfg.ExternalIMAPSecretKey,
		"externalImapSyncSeconds":         strconv.Itoa(cfg.ExternalIMAPSyncSeconds),
		"externalImapAllowPrivateHosts":   strconv.FormatBool(cfg.ExternalIMAPAllowPrivateHosts),
		"externalImapGmailClientId":       cfg.ExternalIMAPGmailClientID,
		"externalImapGmailClientSecret":   cfg.ExternalIMAPGmailClientSecret,
		"externalImapOutlookClientId":     cfg.ExternalIMAPOutlookClientID,
		"externalImapOutlookClientSecret": cfg.ExternalIMAPOutlookClientSecret,
		"telegramMailEnabled":             strconv.FormatBool(cfg.TelegramMailEnabled),
		"telegramBotToken":                cfg.TelegramBotToken,
		"telegramPrivateChatId":           cfg.TelegramPrivateChatID,
		"telegramBodyMode":                normalizeTelegramBodyMode(cfg.TelegramBodyMode),
		"telegramMailboxIds":              strings.Join(cleanIDList(strings.Split(cfg.TelegramMailboxIDs, ",")), ","),
		"telegramIncludeUnregistered":     strconv.FormatBool(cfg.TelegramIncludeUnregistered),
	}
	now := a.now().UTC().Format(time.RFC3339Nano)
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for key, value := range values {
		if _, err := tx.ExecContext(ctx, `INSERT INTO system_settings(key,value,updated_at) VALUES(?,?,?)
			ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, key, value, now); err != nil {
			return err
		}
	}
	if clearPendingTelegram {
		if _, err := tx.ExecContext(ctx, `DELETE FROM telegram_mail_outbox WHERE delivered_at IS NULL`); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func cleanIDList(items []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func parseReservedPrefixes(value string) []string {
	value = strings.NewReplacer("\r", "\n", ",", "\n", ";", "\n", "，", "\n", "；", "\n").Replace(value)
	seen := map[string]bool{}
	out := []string{}
	for _, item := range strings.Split(value, "\n") {
		item = normalizeLocalPart(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func normalizeHostname(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimSuffix(value, ".")
	value = strings.TrimPrefix(value, "http://")
	value = strings.TrimPrefix(value, "https://")
	if i := strings.Index(value, "/"); i >= 0 {
		value = value[:i]
	}
	return value
}
