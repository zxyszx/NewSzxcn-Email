package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
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
	checks["dmarc"] = txtContains(dmarcTXT, "v=DMARC1", "DMARC 记录存在", "未找到 DMARC 记录")

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
