package sim

import (
	"encoding/hex"
	"strings"
)

// maskHexBytes 将输入的字节切片格式化为 hex 字符串，并对中间部分进行掩码脱敏处理以防泄露敏感卡片鉴权数据
func maskHexBytes(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return maskHexString(hex.EncodeToString(b))
}

// maskHexString 对 Hex 编码的敏感密文/响应字符串进行部分遮罩脱敏（前后保留 12 字符，中间以 "..." 遮罩）
func maskHexString(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) <= 24 {
		return s
	}
	return s[:12] + "..." + s[len(s)-12:]
}
