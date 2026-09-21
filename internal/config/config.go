// Package config 管理 pi-teacher-cli 的本地配置.
// 配置文件位于 ~/.config/pi-teacher/config.json, 保存 server_url 与 api_key;
// 环境变量 PI_TEACHER_SERVER_URL / PI_TEACHER_API_KEY 优先于配置文件,
// 命令行参数优先级最高, 由命令层在调用 Resolve 前自行覆盖.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// 环境变量名, 取值非空时覆盖配置文件中的对应字段.
const (
	EnvServerURL = "PI_TEACHER_SERVER_URL"
	EnvAPIKey    = "PI_TEACHER_API_KEY"
)

// Config 是配置文件的内存表示, 也是运行时合并后的有效配置载体.
type Config struct {
	ServerURL string `json:"server_url"`
	APIKey    string `json:"api_key"`
}

// Source 标记某字段的实际来源, 供 show 命令向用户解释配置生效路径.
type Source string

const (
	SourceDefault Source = "default"
	SourceFile    Source = "config_file"
	SourceEnv     Source = "env"
)

// Resolved 是按优先级合并后的有效配置, 附带各字段的来源.
type Resolved struct {
	Config
	ServerURLSource Source
	APIKeySource    Source
}

// Path 返回配置文件路径, 依赖 os.UserConfigDir (Linux 下 ~/.config).
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("无法定位用户配置目录: %w", err)
	}
	return filepath.Join(dir, "pi-teacher", "config.json"), nil
}

// Load 读取配置文件. 文件不存在时返回零值配置而非错误,
// 让命令层在缺少配置时给出更准确的设置指引.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	return LoadFrom(path)
}

// LoadFrom 从指定路径读取配置, 独立于 Path 以便测试注入临时路径.
func LoadFrom(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("读取配置文件失败: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("配置文件不是合法 JSON (%s): %w", path, err)
	}
	return cfg, nil
}

// Save 写入配置文件, 目录权限 0700, 文件权限 0600,
// 避免 API Key 被同机其他用户读取.
func Save(cfg Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	return SaveTo(path, cfg)
}

// SaveTo 写入指定路径, 独立于 Path 以便测试注入临时路径.
func SaveTo(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("编码配置失败: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}
	return nil
}

// Resolve 按优先级合并配置: 环境变量 > 配置文件, 并记录各字段来源.
func Resolve() (Resolved, error) {
	cfg, err := Load()
	if err != nil {
		return Resolved{}, err
	}
	r := Resolved{Config: cfg, ServerURLSource: SourceDefault, APIKeySource: SourceDefault}
	if cfg.ServerURL != "" {
		r.ServerURLSource = SourceFile
	}
	if cfg.APIKey != "" {
		r.APIKeySource = SourceFile
	}
	if v := os.Getenv(EnvServerURL); v != "" {
		r.ServerURL, r.ServerURLSource = v, SourceEnv
	}
	if v := os.Getenv(EnvAPIKey); v != "" {
		r.APIKey, r.APIKeySource = v, SourceEnv
	}
	return r, nil
}

// MaskKey 脱敏 API Key, 保留前 4 位 (即 ptk_ 前缀) 与末 4 位便于人工核对.
// key 过短时整体替换为星号, 避免泄漏任何片段.
func MaskKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return strings.Repeat("*", len(key))
	}
	return key[:4] + "****" + key[len(key)-4:]
}
