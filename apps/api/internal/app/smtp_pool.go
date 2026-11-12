package app

// External IMAP is the product entry; outbound routing uses SMTP explicitly.
import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	netmail "net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

type smtpPoolAccount struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	From        string `json:"from"`
	Enabled     bool   `json:"enabled"`
	RotateAfter int    `json:"rotateAfter"`
	Password    string `json:"password,omitempty"`
	PasswordSet bool   `json:"passwordSet"`
	Ciphertext  string `json:"ciphertext,omitempty"` // persisted only; scrubbed on every response
}
type smtpPoolSettings struct {
	Source       string            `json:"source"` // inherit or personal
	Enabled      bool              `json:"enabled"`
	AllProviders bool              `json:"allProviders"`
	Providers    []string          `json:"providers"`
	Domains      []string          `json:"domains"`
	Strategy     string            `json:"strategy"` // rotate or single
	AccountID    string            `json:"accountId"`
	Accounts     []smtpPoolAccount `json:"accounts"`
}
type recipientProvider struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Domains []string `json:"domains"`
}

var smtpRecipientProviders = []recipientProvider{
	{"microsoft", "微软 Outlook / Hotmail / Live", []string{"outlook.com", "hotmail.com", "live.com", "msn.com", "outlook.cn", "hotmail.co.uk", "hotmail.fr", "hotmail.de", "hotmail.it", "live.cn", "live.co.uk"}},
	{"yahoo", "Yahoo / Ymail / Rocketmail", []string{"yahoo.com", "ymail.com", "rocketmail.com", "yahoo.co.uk", "yahoo.ca", "yahoo.com.au", "yahoo.fr", "yahoo.de", "yahoo.it", "yahoo.es", "yahoo.com.hk", "yahoo.com.tw", "yahoo.co.jp"}},
	{"google", "谷歌 Gmail", []string{"gmail.com", "googlemail.com"}},
	{"qq", "腾讯 QQ / Foxmail", []string{"qq.com", "foxmail.com", "vip.qq.com"}},
	{"netease", "网易 163 / 126 / Yeah", []string{"163.com", "126.com", "yeah.net", "vip.163.com", "vip.126.com", "188.com"}},
	{"sina", "新浪", []string{"sina.com", "sina.cn", "vip.sina.com"}},
	{"sohu", "搜狐", []string{"sohu.com"}},
	{"aliyun", "阿里云", []string{"aliyun.com", "alibaba.com"}},
	{"apple", "Apple iCloud", []string{"icloud.com", "me.com", "mac.com"}},
	{"aol", "AOL", []string{"aol.com", "aim.com"}},
	{"proton", "Proton", []string{"proton.me", "protonmail.com", "pm.me", "protonmail.ch"}},
	{"zoho", "Zoho", []string{"zoho.com", "zohomail.com", "zoho.eu"}},
	{"gmx", "GMX", []string{"gmx.com", "gmx.net", "gmx.de"}},
	{"mailru", "Mail.ru", []string{"mail.ru", "inbox.ru", "list.ru", "bk.ru"}},
	{"yandex", "Yandex", []string{"yandex.com", "yandex.ru", "ya.ru"}},
	{"naver", "Naver / Daum", []string{"naver.com", "daum.net", "hanmail.net"}},
	{"other", "其他邮箱厂商 / 自建域名", nil},
}

func smtpProvider(address string) string {
	domain := domainPart(normalizeEmail(address))
	for _, p := range smtpRecipientProviders {
		for _, d := range p.Domains {
			if domain == d {
				return p.ID
			}
		}
	}
	return "other"
}
func (p smtpPoolSettings) matches(address string) bool {
	if !p.Enabled {
		return false
	}
	if p.AllProviders {
		return true
	}
	for _, id := range p.Providers {
		if id == smtpProvider(address) {
			return true
		}
	}
	for _, d := range p.Domains {
		if d == domainPart(normalizeEmail(address)) {
			return true
		}
	}
	return false
}
func (a *App) loadSMTPPool(ctx context.Context, scope string) (smtpPoolSettings, bool, error) {
	var raw string
	err := a.db.QueryRowContext(ctx, `SELECT settings FROM smtp_pools WHERE scope=?`, scope).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return smtpPoolSettings{Source: "inherit", Strategy: "rotate", Accounts: []smtpPoolAccount{}, Providers: []string{}, Domains: []string{}}, false, nil
	}
	if err != nil {
		return smtpPoolSettings{}, false, err
	}
	var p smtpPoolSettings
	err = json.Unmarshal([]byte(raw), &p)
	return p, true, err
}
func publicSMTPPool(p smtpPoolSettings) smtpPoolSettings {
	p.Accounts = append([]smtpPoolAccount{}, p.Accounts...)
	for i := range p.Accounts {
		p.Accounts[i].PasswordSet = p.Accounts[i].Ciphertext != ""
		p.Accounts[i].Ciphertext = ""
		p.Accounts[i].Password = ""
	}
	return p
}
func (a *App) handleGetSMTPPool(w http.ResponseWriter, r *http.Request) {
	admin := strings.HasPrefix(r.URL.Path, "/api/admin/")
	scope := currentUser(r).ID
	if admin {
		scope = "admin"
	}
	p, _, err := a.loadSMTPPool(r.Context(), scope)
	if err != nil {
		respondError(w, 500, "无法读取外部发信配置")
		return
	}
	response := map[string]any{"settings": publicSMTPPool(p), "providers": smtpRecipientProviders}
	if !admin {
		shared, _, err := a.loadSMTPPool(r.Context(), "admin")
		if err != nil {
			respondError(w, 500, "无法读取后台发信配置")
			return
		}
		count := 0
		for _, v := range shared.Accounts {
			if v.Enabled {
				count++
			}
		}
		// Personal users may see the policy summary, never shared addresses or secrets.
		response["shared"] = map[string]any{"enabled": shared.Enabled, "accountCount": count, "allProviders": shared.AllProviders, "providers": shared.Providers, "domains": shared.Domains, "strategy": shared.Strategy}
	}
	respondJSON(w, 200, response)
}
func (a *App) validateSMTPPool(ctx context.Context, p *smtpPoolSettings, old smtpPoolSettings) error {
	if p.Source != "inherit" && p.Source != "personal" {
		return errors.New("请选择使用后台配置或个人配置")
	}
	if p.Strategy != "rotate" && p.Strategy != "single" {
		return errors.New("请选择轮换或单账号")
	}
	if len(p.Accounts) > 50 || len(p.Domains) > 100 {
		return errors.New("账号最多 50 个，自定义域名最多 100 个")
	}
	known := map[string]bool{}
	for _, v := range smtpRecipientProviders {
		known[v.ID] = true
	}
	seenProvider := map[string]bool{}
	for _, v := range p.Providers {
		if !known[v] || seenProvider[v] {
			return errors.New("收件厂商选项无效或重复")
		}
		seenProvider[v] = true
	}
	for i, d := range p.Domains {
		d = strings.ToLower(strings.TrimSpace(d))
		if len(d) > 253 || strings.ContainsAny(d, " /:@\\\r\n") || !strings.Contains(d, ".") || strings.HasPrefix(d, ".") || strings.HasSuffix(d, ".") {
			return errors.New("自定义域名格式无效")
		}
		for _, c := range d {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '.') {
				return errors.New("自定义域名请使用 ASCII / Punycode")
			}
		}
		p.Domains[i] = d
	}
	existing := map[string]smtpPoolAccount{}
	for _, v := range old.Accounts {
		existing[v.ID] = v
	}
	ids := map[string]bool{}
	enabled := 0
	selected := false
	for i := range p.Accounts {
		v := &p.Accounts[i]
		if v.ID == "" {
			v.ID = newID("smtp")
		} else if _, ok := existing[v.ID]; !ok {
			return errors.New("账号不存在或不属于当前配置")
		}
		if p.AccountID == fmt.Sprintf("new:%d", i) {
			p.AccountID = v.ID
		}
		if ids[v.ID] {
			return errors.New("账号重复")
		}
		ids[v.ID] = true
		v.Name = strings.TrimSpace(v.Name)
		v.Host = strings.ToLower(strings.TrimSpace(v.Host))
		v.Username = strings.TrimSpace(v.Username)
		v.From = normalizeEmail(v.From)
		if len(v.Name) > 100 || v.Name == "" || v.Host == "" || len(v.Host) > 253 || strings.ContainsAny(v.Host, " /:@\\\r\n") || v.Username == "" || len(v.Username) > 320 || strings.ContainsAny(v.Username, "\r\n") {
			return errors.New("请填写有效的账号名称、SMTP 主机和登录账号")
		}
		if v.Port != 465 && v.Port != 587 {
			return errors.New("SMTP 仅支持 TLS 465 或 STARTTLS 587")
		}
		from, err := netmail.ParseAddress(v.From)
		if err != nil || from.Address != v.From {
			return errors.New("发件地址无效")
		}
		if v.RotateAfter < 1 || v.RotateAfter > 10000 {
			return errors.New("每轮分配人数应在 1 至 10000 之间")
		}
		// Never accept ciphertext supplied by the browser. Preserve only the owner's prior secret.
		previous := existing[v.ID]
		v.Ciphertext = ""
		if v.Password != "" {
			if len(v.Password) > 2048 || strings.ContainsAny(v.Password, "\r\n") {
				return errors.New("应用密码格式无效")
			}
			v.Ciphertext, err = a.encryptExternalIMAPPassword(v.Password)
			if err != nil {
				return errors.New("请先配置后台凭据加密密钥")
			}
		} else if previous.Host == v.Host && previous.Port == v.Port && previous.Username == v.Username && previous.From == v.From {
			v.Ciphertext = previous.Ciphertext
		}
		v.Password = ""
		v.PasswordSet = v.Ciphertext != ""
		if v.Enabled && v.Ciphertext == "" {
			return errors.New("新增或更换账号后需重新填写应用密码")
		}
		if v.Enabled {
			enabled++
			if v.ID == p.AccountID {
				selected = true
			}
		}
	}
	if p.Enabled {
		if enabled == 0 {
			return errors.New("启用前至少添加一个可用 SMTP 账号")
		}
		if p.Strategy == "single" && !selected {
			return errors.New("请选择已启用的单个账号")
		}
		if !p.AllProviders && len(p.Providers) == 0 && len(p.Domains) == 0 {
			return errors.New("请至少勾选一个收件厂商")
		}
	}
	return nil
}
func (a *App) handleSaveSMTPPool(w http.ResponseWriter, r *http.Request) {
	var p smtpPoolSettings
	if err := decodeJSON(r, &p); err != nil {
		badRequest(w, err)
		return
	}
	scope := currentUser(r).ID
	if strings.HasPrefix(r.URL.Path, "/api/admin/") {
		scope = "admin"
		p.Source = "personal"
	}
	old, _, err := a.loadSMTPPool(r.Context(), scope)
	if err != nil {
		respondError(w, 500, "无法读取发信配置")
		return
	}
	if err = a.validateSMTPPool(r.Context(), &p, old); err != nil {
		badRequest(w, err)
		return
	}
	raw, _ := json.Marshal(p)
	_, err = a.db.ExecContext(r.Context(), `INSERT INTO smtp_pools(scope,settings,cursor) VALUES(?,?,0) ON CONFLICT(scope) DO UPDATE SET settings=excluded.settings`, scope, string(raw))
	if err != nil {
		respondError(w, 500, "无法保存外部发信配置")
		return
	}
	a.handleGetSMTPPool(w, r)
}
func (a *App) effectiveSMTPPool(ctx context.Context, userID string) (smtpPoolSettings, string, bool, error) {
	p, personalExists, err := a.loadSMTPPool(ctx, userID)
	if err != nil {
		return p, "", false, err
	}
	if personalExists && p.Source == "personal" {
		return p, userID, true, nil
	}
	p, exists, err := a.loadSMTPPool(ctx, "admin")
	return p, "admin", exists || personalExists, err
}

// Assignment and cursor advancement share one transaction. Retrying a recipient
// keeps its account and consumes no additional rotation slot, across restarts.
func (a *App) assignSMTPPool(ctx context.Context, scope string, item sendQueueItem, recipient string) (string, error) {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	// A write first serializes concurrent allocators on SQLite, including processes.
	var cursor int64
	err = tx.QueryRowContext(ctx, `UPDATE smtp_pools SET cursor=cursor+1 WHERE scope=? RETURNING cursor-1`, scope).Scan(&cursor)
	if err != nil {
		return "", err
	}
	var assigned string
	err = tx.QueryRowContext(ctx, `SELECT account_id FROM smtp_pool_assignments WHERE queue_id=? AND recipient=?`, item.ID, recipient).Scan(&assigned)
	if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE smtp_pools SET cursor=cursor-1 WHERE scope=?`, scope)
		if err != nil {
			return "", err
		}
		return assigned, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	var raw string
	if err = tx.QueryRowContext(ctx, `SELECT settings FROM smtp_pools WHERE scope=?`, scope).Scan(&raw); err != nil {
		return "", err
	}
	var p smtpPoolSettings
	if err = json.Unmarshal([]byte(raw), &p); err != nil {
		return "", err
	}
	accounts := []smtpPoolAccount{}
	total := int64(0)
	for _, v := range p.Accounts {
		if v.Enabled && (p.Strategy != "single" || p.AccountID == v.ID) {
			accounts = append(accounts, v)
			total += int64(v.RotateAfter)
		}
	}
	if !p.Enabled || total == 0 {
		return "", errors.New("外部发信账号池尚未启用或没有可用账号")
	}
	slot := cursor % total
	for _, v := range accounts {
		if slot < int64(v.RotateAfter) {
			assigned = v.ID
			break
		}
		slot -= int64(v.RotateAfter)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO smtp_pool_assignments(queue_id,recipient,scope,account_id) VALUES(?,?,?,?)`, item.ID, recipient, scope, assigned)
	if err != nil {
		return "", err
	}
	return assigned, tx.Commit()
}
func (a *App) sendSMTPPoolRecipient(ctx context.Context, account smtpPoolAccount, recipient string, raw []byte) error {
	password, err := a.decryptExternalIMAPPassword(account.Ciphertext)
	if err != nil {
		return errors.New("外部发信凭据无法解密")
	}
	rewritten, err := personalGmailMIME(raw, account.From)
	if err != nil {
		return err
	}
	cfg := a.config()
	cfg.SMTPHost = account.Host
	cfg.SMTPPort = strconv.Itoa(account.Port)
	cfg.SMTPUsername = account.Username
	cfg.SMTPPassword = password
	cfg.SMTPRequireTLS = true
	if a.personalSMTPSender != nil {
		return a.personalSMTPSender(cfg, account.From, []string{recipient}, rewritten)
	}
	// Resolve once, reject every private answer, and dial only validated IPs.
	var ips []net.IP
	if ip := net.ParseIP(account.Host); ip != nil {
		ips = []net.IP{ip}
	} else {
		lookup := a.lookupIP
		if lookup == nil {
			lookup = net.DefaultResolver.LookupIP
		}
		ips, err = lookup(ctx, "ip", account.Host)
	}
	if err != nil || len(ips) == 0 {
		return errors.New("无法解析外部 SMTP 主机")
	}
	for _, ip := range ips {
		if !isPublicExternalIMAPIP(ip) || ip.IsMulticast() {
			return errors.New("外部 SMTP 不允许内网地址")
		}
	}
	var conn net.Conn
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	for _, ip := range ips {
		conn, err = dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), cfg.SMTPPort))
		if err == nil {
			break
		}
	}
	if err != nil {
		return errors.New("无法连接外部 SMTP")
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(smtpSessionTimeout))
	tlsCfg := &tls.Config{ServerName: account.Host, MinVersion: tls.VersionTLS12}
	if account.Port == 465 {
		secure := tls.Client(conn, tlsCfg)
		if err = secure.HandshakeContext(ctx); err != nil {
			return errors.New("外部 SMTP TLS 验证失败")
		}
		conn = secure
	}
	client, err := smtp.NewClient(conn, account.Host)
	if err != nil {
		return errors.New("外部 SMTP 握手失败")
	}
	defer client.Close()
	if account.Port == 587 {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("外部 SMTP 不支持 STARTTLS")
		}
		if err = client.StartTLS(tlsCfg); err != nil {
			return errors.New("外部 SMTP TLS 验证失败")
		}
	}
	// DATA acceptance is delivery to the relay. QUIT failure must not resend it.
	if err = client.Auth(smtp.PlainAuth("", account.Username, password, account.Host)); err != nil {
		return errors.New("外部 SMTP 认证失败")
	}
	if err = client.Mail(account.From); err != nil {
		return errors.New("外部 SMTP 拒绝发件人")
	}
	if err = client.Rcpt(recipient); err != nil {
		return errors.New("外部 SMTP 拒绝收件人")
	}
	writer, err := client.Data()
	if err != nil {
		return errors.New("外部 SMTP 无法提交邮件")
	}
	if _, err = writer.Write(rewritten); err != nil {
		return errors.New("外部 SMTP 传输失败")
	}
	if err = writer.Close(); err != nil {
		return errors.New("外部 SMTP 未确认接收邮件")
	}
	_ = client.Quit()
	return nil
}
func (a *App) sendQueueWithSMTPPool(ctx context.Context, item sendQueueItem, raw []byte) (bool, error) {
	p, scope, exists, err := a.effectiveSMTPPool(ctx, item.UserID)
	if err != nil {
		return true, errors.New("无法读取外部账号池")
	}
	if !exists {
		return false, nil
	} // legacy personal Gmail route remains compatible until migrated
	var errs []error
	for _, recipient := range item.Recipients {
		done, err := a.hasSendQueueDeliveredMarker(personalSubmissionMarker(item.ID, []string{recipient}))
		if err != nil {
			return true, err
		}
		if done {
			continue
		}
		var assigned, assignedScope string
		err = a.db.QueryRowContext(ctx, `SELECT scope,account_id FROM smtp_pool_assignments WHERE queue_id=? AND recipient=?`, item.ID, recipient).Scan(&assignedScope, &assigned)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return true, err
		}
		useExternal := p.matches(recipient) || assigned != ""
		if useExternal {
			if !a.externalIMAPAllowedByID(ctx, item.UserID) {
				errs = append(errs, errors.New("外部 IMAP 使用权限已关闭，外部发信暂停"))
				continue
			}
			if !p.Enabled || (assigned != "" && assignedScope != scope) {
				errs = append(errs, errors.New("待发邮件的外部通道已停用或切换，请检查队列"))
				continue
			}
			if assigned == "" {
				assigned, err = a.assignSMTPPool(ctx, scope, item, recipient)
				if err != nil {
					errs = append(errs, errors.New("无法分配外部发信账号"))
					continue
				}
			}
			var account *smtpPoolAccount
			for i := range p.Accounts {
				if p.Accounts[i].ID == assigned && p.Accounts[i].Enabled {
					account = &p.Accounts[i]
					break
				}
			}
			if account == nil {
				errs = append(errs, errors.New("待发邮件的外部账号已移除或停用，请检查队列"))
				continue
			}
			err = a.sendSMTPPoolRecipient(ctx, *account, recipient, raw)
		} else {
			if a.personalSMTPSender != nil {
				err = a.personalSMTPSender(a.config(), item.MailFrom, []string{recipient}, raw)
			} else {
				err = a.sendSMTP(item.MailFrom, []string{recipient}, raw)
			}
		}
		if err != nil {
			if useExternal {
				err = errors.New("外部 SMTP 提交失败，请检查凭据、配额及连接")
			}
			errs = append(errs, err)
			continue
		}
		if err = a.writeSendQueueDeliveredMarker(personalSubmissionMarker(item.ID, []string{recipient})); err != nil {
			return true, fmt.Errorf("邮件已提交，但提交标记保存失败")
		}
	}
	return true, errors.Join(errs...)
}
