package api

import (
	"fmt"
	"unicode"
)

// 密码复杂度策略（企业级）：≥10 位，且同时包含大写字母、小写字母、数字、特殊字符。
// 仅在设置新密码（创建用户/重置密码）时校验；不影响已有账号登录。
const (
	passwordMinLength = 10
)

// validatePasswordPolicy 校验新密码是否满足复杂度要求；不满足返回中文原因。
func validatePasswordPolicy(pw string) error {
	if len([]rune(pw)) < passwordMinLength {
		return fmt.Errorf("密码需至少 %d 位，并包含大写字母、小写字母、数字和特殊字符", passwordMinLength)
	}
	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for _, r := range pw {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			hasSpecial = true
		}
	}
	if !hasUpper || !hasLower || !hasDigit || !hasSpecial {
		return fmt.Errorf("密码需至少 %d 位，并包含大写字母、小写字母、数字和特殊字符", passwordMinLength)
	}
	return nil
}
