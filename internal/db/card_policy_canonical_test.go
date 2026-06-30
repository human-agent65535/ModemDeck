package db

import (
	"testing"
	"time"
)

func TestCanonicalICCID(t *testing.T) {
	cases := map[string]string{
		"894921007608768278f":     "894921007608768278",
		"894921007608768278F":     "894921007608768278",
		" 894921007608768278F ":   "894921007608768278",
		"\"894921007608768278f\"": "894921007608768278",
		"89012804332291621963":    "89012804332291621963", // 20 位无 F，原样
		"":                        "",
	}
	for in, want := range cases {
		if got := CanonicalICCID(in); got != want {
			t.Fatalf("CanonicalICCID(%q)=%q, want %q", in, got, want)
		}
	}
}

// 带 F 的 ICCID 与不带 F 的必须命中同一行（修复 eSIM profile(无F) 与运行时身份(带F) 读到不同行）。
func TestGetCardPolicyCanonicalLookup(t *testing.T) {
	openTestDB(t)
	// 用户用"无 F"形态落库（eSIM profile 形态）
	if err := UpsertCardPolicy(CardPolicy{ICCID: "894921007608768278", VoWiFiEnabled: true, IPVersion: "v4", Source: "user"}); err != nil {
		t.Fatal(err)
	}
	// 用"带 F"形态查询（运行时身份形态）应命中同一行
	got, err := GetCardPolicy("894921007608768278F")
	if err != nil {
		t.Fatalf("带 F 查询未命中: %v", err)
	}
	if !got.VoWiFiEnabled {
		t.Fatalf("命中了错误的行: %+v", got)
	}
	// 反向：带 F 落库，无 F 查询
	if err := UpsertCardPolicy(CardPolicy{ICCID: "8964240002094346553F", AirplaneEnabled: true, IPVersion: "v4", Source: "user"}); err != nil {
		t.Fatal(err)
	}
	got2, err := GetCardPolicy("8964240002094346553")
	if err != nil || !got2.AirplaneEnabled {
		t.Fatalf("无 F 查询带 F 落库行失败: %+v err=%v", got2, err)
	}
	// 落库 key 应是 canonical（无 F）
	if got2.ICCID != "8964240002094346553" {
		t.Fatalf("落库 key 未规整: %q", got2.ICCID)
	}
}

// 迁移：同一卡的 ...f 与无 f 两行合并为一行 canonical，按 user 优先 / 新优先取胜。
func TestMigrateCardPolicyCanonicalICCID(t *testing.T) {
	openTestDB(t)
	older := time.Now().Add(-2 * time.Hour)
	newer := time.Now().Add(-1 * time.Hour)
	// 直接写两行（绕过 UpsertCardPolicy 的规整，模拟历史脏数据）
	if err := DB.Exec(
		"INSERT INTO card_policies (iccid, network_enabled, vowifi_enabled, airplane_enabled, ip_version, apn, source, created_at, updated_at) VALUES "+
			"('894921007608768278f',0,0,0,'v4','','auto',?,?),"+
			"('894921007608768278',0,1,0,'v4','','user',?,?)",
		older, older, older, newer,
	).Error; err != nil {
		t.Fatal(err)
	}

	if err := MigrateCardPolicyCanonicalICCID(DB); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	var rows []CardPolicy
	if err := DB.Where("iccid LIKE '894921007608768278%'").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("应合并为 1 行，实得 %d: %+v", len(rows), rows)
	}
	r := rows[0]
	if r.ICCID != "894921007608768278" {
		t.Fatalf("幸存行 key 应 canonical: %q", r.ICCID)
	}
	if r.Source != "user" || !r.VoWiFiEnabled {
		t.Fatalf("应保留 user 行的值: %+v", r)
	}
}

// 迁移幂等：重复运行不报错、不再改动。
func TestMigrateCardPolicyCanonicalICCIDIdempotent(t *testing.T) {
	openTestDB(t)
	if err := UpsertCardPolicy(CardPolicy{ICCID: "8960192510382231416", VoWiFiEnabled: true, IPVersion: "v4", Source: "user"}); err != nil {
		t.Fatal(err)
	}
	if err := MigrateCardPolicyCanonicalICCID(DB); err != nil {
		t.Fatal(err)
	}
	if err := MigrateCardPolicyCanonicalICCID(DB); err != nil {
		t.Fatalf("二次迁移应幂等: %v", err)
	}
	var cnt int64
	DB.Model(&CardPolicy{}).Where("iccid = ?", "8960192510382231416").Count(&cnt)
	if cnt != 1 {
		t.Fatalf("幂等性破坏，行数=%d", cnt)
	}
}
