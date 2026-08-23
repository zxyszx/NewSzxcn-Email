package app

import (
	"strings"
	"testing"
)

func TestCheckDMARCRecords(t *testing.T) {
	tests := []struct {
		name       string
		records    []string
		ok         bool
		messageHas string
	}{
		{name: "valid", records: []string{"v=DMARC1; p=quarantine; rua=mailto:postmaster@example.test"}, ok: true, messageHas: "唯一且语法有效"},
		{name: "two valid records", records: []string{"v=DMARC1; p=none", "v=DMARC1; p=reject"}, messageHas: "检测到多条 DMARC 记录"},
		{name: "missing", records: []string{"google-site-verification=token"}, messageHas: "未找到 DMARC 记录"},
		{name: "missing policy", records: []string{"v=DMARC1; rua=mailto:postmaster@example.test"}, messageHas: "缺少 p 策略"},
		{name: "invalid policy", records: []string{"v=DMARC1; p=invalid"}, messageHas: "只能是 none、quarantine 或 reject"},
		{name: "duplicate tag", records: []string{"v=DMARC1; p=none; P=reject"}, messageHas: "重复标签：p"},
		{name: "version not first", records: []string{"p=none; v=DMARC1"}, messageHas: "必须以 v=DMARC1 开头"},
		{name: "invalid rua", records: []string{"v=DMARC1; p=reject; rua=https://example.test/report"}, messageHas: "rua 格式无效"},
		{name: "monitoring policy", records: []string{" V=dmarc1 ; P = NONE "}, ok: true, messageHas: "当前为监控策略"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := checkDMARCRecords(tt.records)
			if status.OK != tt.ok || !strings.Contains(status.Message, tt.messageHas) {
				t.Fatalf("status=%+v", status)
			}
		})
	}
}

func TestCheckDMARCRecordSetsJoinsTXTFragments(t *testing.T) {
	status := checkDMARCRecordSets([][]string{{"v=DMARC1; ", "p=none; rua=mailto:", "postmaster@example.test"}})
	if !status.OK || !strings.Contains(status.Message, "监控策略") {
		t.Fatalf("status=%+v", status)
	}
	if len(status.Found) != 1 || status.Found[0] != "v=DMARC1; p=none; rua=mailto:postmaster@example.test" {
		t.Fatalf("found=%q", status.Found)
	}
}
