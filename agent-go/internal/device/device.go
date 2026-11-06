// Package device 设备令牌管理：参数直传或令牌文件（%ProgramData%\LiYouP2P\token.json）。
package device

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// TokenFile 令牌文件结构（GUI 登录后写入）。
type TokenFile struct {
	DeviceToken string `json:"device_token"`
	DeviceUID   string `json:"device_uid,omitempty"`
	Role        string `json:"role,omitempty"`
	Platform    string `json:"platform,omitempty"`
}

// Resolve 获取设备令牌：优先参数直传，其次令牌文件。
func Resolve(flagToken, tokenFile string) (string, error) {
	if flagToken != "" {
		return flagToken, nil
	}
	if data, err := os.ReadFile(tokenFile); err == nil {
		var tf TokenFile
		if json.Unmarshal(data, &tf) == nil && tf.DeviceToken != "" {
			return tf.DeviceToken, nil
		}
	}
	return "", errors.New("未找到设备令牌（--device-token 参数或令牌文件 " + tokenFile + " 均为空），请从 GUI 登录获取设备令牌")
}

// Save 保存令牌到文件（供 GUI 集成调用）。
func Save(path, token string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("创建令牌目录失败: %w", err)
	}
	data, err := json.MarshalIndent(TokenFile{DeviceToken: token}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
