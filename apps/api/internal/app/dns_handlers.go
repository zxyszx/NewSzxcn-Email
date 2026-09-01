package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	netmail "net/mail"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

func (a *App) handleDNSRecords(w http.ResponseWriter, r *http.Request) {
	domain, err := a.domainByID(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusNotFound, "domain not found")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"items": a.dnsRecordsFor(domain)})
}

func (a *App) handleDNSCheck(w http.ResponseWriter, r *http.Request) {
	domain, err := a.domainByID(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusNotFound, "domain not found")
		return
	}
	result := a.checkDNS(r.Context(), domain)
	now := a.now().UTC().Format(time.RFC3339Nano)
	_, _ = a.db.ExecContext(r.Context(), `UPDATE domains SET dns_status=?, dns_checked_at=?, updated_at=? WHERE id=?`, result.Status, now, now, domain.ID)
	respondJSON(w, http.StatusOK, result)
}

func (a *App) dnsRecordsFor(d *Domain) []DNSRecord {
	name := strings.TrimSuffix(d.Name, ".")
	host := strings.TrimSuffix(a.config().PublicHostname, ".") + "."
	return []DNSRecord{
		{Type: "MX", Name: name, Value: fmt.Sprintf("10 %s", host), TTL: 300},
		{Type: "TXT", Name: name, Value: "v=spf1 mx -all", TTL: 300},
		{Type: "TXT", Name: d.DKIMSelector + "._domainkey." + name, Value: "v=DKIM1; k=rsa; p=" + d.DKIMPublicKey, TTL: 300},
		{Type: "TXT", Name: "_dmarc." + name, Value: "v=DMARC1; p=quarantine; rua=mailto:postmaster@" + name, TTL: 300},
	}
}

func (a *App) checkDNS(ctx context.Context, d *Domain) DNSCheckResult {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resolver := net.DefaultResolver
	checks := map[string]DNSCheckStatus{}

	mx, err := resolver.LookupMX(ctx, d.Name)
	if err != nil || len(mx) == 0 {
		checks["mx"] = DNSCheckStatus{OK: false, Message: "未找到 MX 记录"}
	} else {
		found := make([]string, 0, len(mx))
		ok := false
		for _, item := range mx {
			entry := fmt.Sprintf("%d %s", item.Pref, strings.TrimSuffix(item.Host, "."))
			found = append(found, entry)
			if strings.EqualFold(strings.TrimSuffix(item.Host, "."), strings.TrimSuffix(a.config().PublicHostname, ".")) {
				ok = true
			}
		}
		checks["mx"] = DNSCheckStatus{OK: ok, Message: boolMessage(ok, "MX 指向正确", "MX 未指向当前邮件主机"), Found: found}
	}

	rootTXT, _ := resolver.LookupTXT(ctx, d.Name)
	checks["spf"] = txtContains(rootTXT, "v=spf1", "SPF 记录存在", "未找到 SPF 记录")

	dkimName := d.DKIMSelector + "._domainkey." + d.Name
	dkimTXT, _ := resolver.LookupTXT(ctx, dkimName)
	checks["dkim"] = checkDKIMRecord(dkimTXT, d.DKIMPublicKey)

	dmarcTXT, _ := resolver.LookupTXT(ctx, "_dmarc."+d.Name)
	checks["dmarc"] = checkDMARCRecords(dmarcTXT)

	status := "ok"
	for _, c := range checks {
		if !c.OK {
			status = "error"
			break
		}
	}
	return DNSCheckResult{Domain: d.Name, Status: status, Checks: checks}
}

func checkDKIMRecord(records []string, expectedPublicKey string) DNSCheckStatus {
	found := append([]string{}, records...)
	expectedPublicKey = compactDKIMPublicKey(expectedPublicKey)
	dkimFound := false
	for _, record := range records {
		tags := map[string]string{}
		for _, part := range strings.Split(record, ";") {
			key, value, ok := strings.Cut(part, "=")
			if !ok {
				continue
			}
			tags[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
		}
		if !strings.EqualFold(tags["v"], "DKIM1") {
			continue
		}
		dkimFound = true
		if expectedPublicKey != "" && compactDKIMPublicKey(tags["p"]) == expectedPublicKey {
			return DNSCheckStatus{OK: true, Message: "DKIM 公钥匹配", Found: found}
		}
	}
	if dkimFound {
		return DNSCheckStatus{OK: false, Message: "DKIM 公钥与后台生成的记录不一致", Found: found}
	}
	return DNSCheckStatus{OK: false, Message: "未找到 DKIM 记录", Found: found}
}

func compactDKIMPublicKey(value string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			return -1
		}
		return r
	}, value)
}

func checkDMARCRecords(records []string) DNSCheckStatus {
	recordSets := make([][]string, 0, len(records))
	for _, record := range records {
		// net.Resolver.LookupTXT returns each logical TXT record with its
		// character-string fragments already concatenated.
		recordSets = append(recordSets, []string{record})
	}
	return checkDMARCRecordSets(recordSets)
}

func checkDMARCRecordSets(recordSets [][]string) DNSCheckStatus {
	found := make([]string, 0, len(recordSets))
	dmarcRecords := make([]string, 0, len(recordSets))
	for _, fragments := range recordSets {
		record := strings.Join(fragments, "")
		found = append(found, record)
		if containsDMARCVersion(record) {
			dmarcRecords = append(dmarcRecords, record)
		}
	}
	if len(dmarcRecords) == 0 {
		return DNSCheckStatus{OK: false, Message: "未找到 DMARC 记录", Found: found}
	}
	if len(dmarcRecords) > 1 {
		return DNSCheckStatus{OK: false, Message: "检测到多条 DMARC 记录，请删除重复记录，否则可能导致 DMARC 认证失败（记录名称：_dmarc）", Found: found}
	}
	policy, message := validateDMARCRecord(dmarcRecords[0])
	if message != "" {
		return DNSCheckStatus{OK: false, Message: message + "（记录名称：_dmarc）", Found: found}
	}
	if policy == "none" {
		return DNSCheckStatus{OK: true, Message: "DMARC 记录有效（p=none，当前为监控策略）", Found: found}
	}
	return DNSCheckStatus{OK: true, Message: "DMARC 记录唯一且语法有效", Found: found}
}

func containsDMARCVersion(record string) bool {
	for _, part := range strings.Split(record, ";") {
		key, value, ok := strings.Cut(part, "=")
		if ok && strings.EqualFold(strings.TrimSpace(key), "v") && strings.EqualFold(strings.TrimSpace(value), "DMARC1") {
			return true
		}
	}
	return false
}

func validateDMARCRecord(record string) (string, string) {
	parts := strings.Split(record, ";")
	tags := map[string]string{}
	firstTag := ""
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if !ok || key == "" || value == "" {
			return "", "DMARC 记录包含无效标签"
		}
		if firstTag == "" {
			firstTag = key
			if key != "v" || !strings.EqualFold(value, "DMARC1") {
				return "", "DMARC 记录必须以 v=DMARC1 开头"
			}
		}
		if _, exists := tags[key]; exists {
			return "", "DMARC 记录包含重复标签：" + key
		}
		tags[key] = value
	}
	if firstTag == "" || !strings.EqualFold(tags["v"], "DMARC1") {
		return "", "DMARC 记录必须以 v=DMARC1 开头"
	}
	policy, ok := tags["p"]
	if !ok {
		return "", "DMARC 记录缺少 p 策略"
	}
	policy = strings.ToLower(policy)
	if policy != "none" && policy != "quarantine" && policy != "reject" {
		return "", "DMARC p 策略只能是 none、quarantine 或 reject"
	}
	if rua, ok := tags["rua"]; ok && !validDMARCRUA(rua) {
		return "", "DMARC rua 格式无效，必须使用 mailto: 邮箱地址"
	}
	return policy, ""
}

func validDMARCRUA(value string) bool {
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if len(item) <= len("mailto:") || !strings.EqualFold(item[:len("mailto:")], "mailto:") {
			return false
		}
		address := strings.TrimSpace(item[len("mailto:"):])
		if sizeIndex := strings.LastIndex(address, "!"); sizeIndex > 0 {
			address = strings.TrimSpace(address[:sizeIndex])
		}
		parsed, err := netmail.ParseAddress(address)
		if err != nil || parsed.Address != address || !strings.Contains(address, "@") {
			return false
		}
	}
	return true
}

func txtContains(records []string, needle, okMsg, failMsg string) DNSCheckStatus {
	found := append([]string{}, records...)
	for _, item := range records {
		if strings.Contains(strings.ToLower(item), strings.ToLower(needle)) {
			return DNSCheckStatus{OK: true, Message: okMsg, Found: found}
		}
	}
	return DNSCheckStatus{OK: false, Message: failMsg, Found: found}
}

func boolMessage(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}
