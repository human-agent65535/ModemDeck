package db

import (
	"gorm.io/gorm"
)

// MigrateCardPolicyCanonicalICCID 把历史脏数据里"带尾 F"与"无 F"重复的 card_policies 行
// 按 canonical ICCID 合并成一行。冲突取胜规则：source=user 优先于 auto；同 source 取
// updated_at 较新者。幂等：已规整无重复时不改动。
//
// 背景：ICCID 历史上有两种来源形态——eSIM profile（BCD 解码已剥 F）与运行时身份
// （QMI DMS 原样保留 F），未规整时同一卡落成两行，UI 不同入口读到不同行。
func MigrateCardPolicyCanonicalICCID(tx *gorm.DB) error {
	if tx == nil || !tx.Migrator().HasTable(&CardPolicy{}) {
		return nil
	}

	var all []CardPolicy
	if err := tx.Find(&all).Error; err != nil {
		return err
	}

	// 按 canonical key 分组
	groups := map[string][]CardPolicy{}
	for _, p := range all {
		key := CanonicalICCID(p.ICCID)
		if key == "" {
			continue
		}
		groups[key] = append(groups[key], p)
	}

	for canonical, rows := range groups {
		// 已规整且唯一：无需处理（幂等快速路径）
		if len(rows) == 1 && rows[0].ICCID == canonical {
			continue
		}

		winner := rows[0]
		for _, r := range rows[1:] {
			if betterCardPolicyRow(r, winner) {
				winner = r
			}
		}

		// 删掉该组所有原始行（key 可能带 F 或重复），再以 canonical key 重写幸存者
		for _, r := range rows {
			if err := tx.Where("iccid = ?", r.ICCID).Delete(&CardPolicy{}).Error; err != nil {
				return err
			}
		}
		winner.ICCID = canonical
		if err := tx.Create(&winner).Error; err != nil {
			return err
		}
	}
	return nil
}

// betterCardPolicyRow 报告 cand 是否比 cur 更应作为合并幸存者：
// source=user 优先于 auto；同 source 取 updated_at 较新者。
func betterCardPolicyRow(cand, cur CardPolicy) bool {
	candUser := cand.Source == "user"
	curUser := cur.Source == "user"
	if candUser != curUser {
		return candUser
	}
	return cand.UpdatedAt.After(cur.UpdatedAt)
}
