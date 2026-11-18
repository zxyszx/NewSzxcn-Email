package app

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/idna"
)

func smtpRelayProviders(cfg Config) []string {
	raw := strings.TrimSpace(cfg.SMTPRelayProviders)
	if raw == "" {
		return []string{"all"}
	} // Existing installations retain global SMTP behavior.
	if raw == "none" {
		return []string{}
	}
	return strings.Split(raw, ",")
}

func smtpDirectConfig(cfg Config) Config {
	cfg.SMTPHost = strings.TrimSpace(cfg.SMTPDirectHost)
	if cfg.SMTPHost == "" {
		cfg.SMTPHost = "127.0.0.1"
	}
	cfg.SMTPPort = strings.TrimSpace(cfg.SMTPDirectPort)
	if cfg.SMTPPort == "" {
		cfg.SMTPPort = "25"
	}
	cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPRequireTLS = "", "", false
	return cfg
}

func normalizeSMTPRoutingConfig(cfg *Config) error {
	seen := map[string]bool{}
	providers := []string{}
	for _, value := range smtpRelayProviders(*cfg) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "all" && value != "custom" && !smtpProviderExists(value) {
			return errors.New("不支持的收件邮箱厂商")
		}
		if !seen[value] {
			providers = append(providers, value)
			seen[value] = true
		}
	}
	if seen["all"] {
		providers = []string{"all"}
	}
	cfg.SMTPRelayProviders = strings.Join(providers, ",")
	if len(providers) == 0 {
		cfg.SMTPRelayProviders = "none"
	}
	domains := []string{}
	for _, value := range strings.FieldsFunc(cfg.SMTPRelayDomains, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' || r == ';' || r == ' ' }) {
		domain, err := idna.Lookup.ToASCII(strings.TrimSuffix(strings.ToLower(value), "."))
		if err != nil || len(domain) > 253 || !strings.Contains(domain, ".") || net.ParseIP(domain) != nil {
			return errors.New("自定义收件域名无效，请填写完整域名")
		}
		for _, label := range strings.Split(domain, ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return errors.New("自定义收件域名无效")
			}
			for _, c := range label {
				if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
					return errors.New("自定义收件域名无效")
				}
			}
		}
		domains = append(domains, domain)
	}
	cfg.SMTPRelayDomains = strings.Join(dedupeStrings(domains), ",")
	if seen["custom"] && !seen["all"] && len(domains) == 0 {
		return errors.New("请选择自定义收件域名")
	}
	direct := smtpDirectConfig(*cfg)
	port, err := strconv.Atoi(direct.SMTPPort)
	if err != nil || port < 1 || port > 65535 {
		return errors.New("直发端口必须为 1–65535")
	}
	if strings.ContainsAny(direct.SMTPHost, "\r\n/ ") {
		return errors.New("直发主机无效")
	}
	cfg.SMTPDirectHost, cfg.SMTPDirectPort = direct.SMTPHost, direct.SMTPPort
	return nil
}

func dedupeStrings(values []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		if !seen[value] {
			out = append(out, value)
			seen[value] = true
		}
	}
	return out
}

// One public catalog supplies both backend recognition and frontend options.
//
//go:embed smtp_providers.json
var smtpProviderFiles embed.FS

type smtpProviderDefinition struct {
	Key        string   `json:"key"`
	Label      string   `json:"label"`
	Domains    []string `json:"domains"`
	MXSuffixes []string `json:"mxSuffixes"`
}

var smtpProviderCatalog = func() []smtpProviderDefinition {
	data, err := smtpProviderFiles.ReadFile("smtp_providers.json")
	if err != nil {
		panic(err)
	}
	var providers []smtpProviderDefinition
	if err = json.Unmarshal(data, &providers); err != nil {
		panic(err)
	}
	return providers
}()

func smtpProviderExists(key string) bool {
	for _, provider := range smtpProviderCatalog {
		if provider.Key == key {
			return true
		}
	}
	return false
}

// Exact domain boundaries avoid matching lookalikes such as outlook.com.attacker.test.
func smtpProviderForDomain(domain string) string {
	for _, provider := range smtpProviderCatalog {
		for _, match := range provider.Domains {
			if domain == match {
				return provider.Key
			}
		}
	}
	return ""
}

func smtpProviderForMX(host string) string {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	for _, provider := range smtpProviderCatalog {
		for _, suffix := range provider.MXSuffixes {
			if host == suffix || strings.HasSuffix(host, "."+suffix) {
				return provider.Key
			}
		}
	}
	return ""
}

func (a *App) smtpRecipientUsesRelay(ctx context.Context, cfg Config, recipient string) (bool, error) {
	selected := map[string]bool{}
	for _, provider := range smtpRelayProviders(cfg) {
		selected[provider] = true
	}
	if selected["all"] {
		return true, nil
	}
	if len(selected) == 0 || selected["none"] {
		return false, nil
	}
	at := strings.LastIndexByte(recipient, '@')
	if at < 0 {
		return false, errors.New("invalid recipient address")
	}
	domain, err := idna.Lookup.ToASCII(strings.ToLower(strings.TrimSuffix(recipient[at+1:], ".")))
	if err != nil {
		return false, errors.New("invalid recipient domain")
	}
	if selected["custom"] {
		for _, match := range strings.Split(cfg.SMTPRelayDomains, ",") {
			if domain == match {
				return true, nil
			}
		}
	}
	if provider := smtpProviderForDomain(domain); provider != "" {
		return selected[provider], nil
	}
	if len(selected) == 1 && selected["custom"] {
		return false, nil
	}
	lookup := a.lookupMX
	if lookup == nil {
		lookup = net.DefaultResolver.LookupMX
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	records, err := lookup(lookupCtx, domain)
	if err != nil {
		return false, fmt.Errorf("无法识别收件域名的邮件厂商，请稍后重试: %w", err)
	}
	// All MX records must belong to the same provider; mixed hosting uses the direct route.
	provider := ""
	for _, record := range records {
		p := smtpProviderForMX(record.Host)
		if p == "" || provider != "" && provider != p {
			return false, nil
		}
		provider = p
	}
	return selected[provider], nil
}

// Persist successful envelopes so failure of the other route does not resend them.
func (a *App) sendRoutedQueueItem(ctx context.Context, item sendQueueItem, mimeBytes []byte) error {
	cfg := a.config()
	rows, err := a.db.QueryContext(ctx, `SELECT recipient FROM send_queue_recipient_receipts WHERE queue_id=?`, item.ID)
	if err != nil {
		return err
	}
	delivered := map[string]bool{}
	for rows.Next() {
		var recipient string
		if err = rows.Scan(&recipient); err != nil {
			rows.Close()
			return err
		}
		delivered[recipient] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	planCtx, cancelPlan := context.WithTimeout(ctx, 30*time.Second)
	defer cancelPlan()
	direct, relay := []string{}, []string{}
	decisions := map[string]bool{}
	for _, recipient := range dedupeEmails(item.Recipients) {
		if delivered[recipient] {
			continue
		}
		domain := domainPart(recipient)
		useRelay, ok := decisions[domain]
		if !ok {
			// Selective routing must not require public MX for locally hosted mail.
			// Keep the legacy global channel unchanged when all is selected.
			var local int
			if cfg.SMTPRelayProviders != "all" && cfg.SMTPRelayProviders != "" {
				if err := a.db.QueryRowContext(planCtx, `SELECT COUNT(1) FROM domains WHERE lower(name)=lower(?) AND status='active'`, domain).Scan(&local); err != nil {
					return err
				}
			}
			if local > 0 {
				decisions[domain] = false
				direct = append(direct, recipient)
				continue
			}
			useRelay, err = a.smtpRecipientUsesRelay(planCtx, cfg, recipient)
			if err != nil {
				return err
			}
			decisions[domain] = useRelay
		}
		if useRelay {
			relay = append(relay, recipient)
		} else {
			direct = append(direct, recipient)
		}
	}
	for _, group := range []struct {
		config     Config
		recipients []string
	}{{smtpDirectConfig(cfg), direct}, {cfg, relay}} {
		if len(group.recipients) == 0 {
			continue
		}
		if err = sendSMTPWithConfig(group.config, item.MailFrom, group.recipients, mimeBytes); err != nil {
			return err
		}
		tx, err := a.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		for _, recipient := range group.recipients {
			if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO send_queue_recipient_receipts(queue_id,recipient) VALUES(?,?)`, item.ID, recipient); err != nil {
				tx.Rollback()
				return err
			}
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
