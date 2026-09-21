package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/Pi-Teacher/cli/internal/config"
)

// RunConfig 实现 config 子命令组:
// set-server / set-api-key 写入配置文件, show 展示合并后的有效配置.
func RunConfig(args []string, out io.Writer) error {
	if len(args) == 0 {
		return NewUsageError("缺少子命令, 可用: set-server <url> / set-api-key <key> / show")
	}
	switch args[0] {
	case "set-server":
		if len(args) != 2 {
			return NewUsageError("用法: config set-server <url>")
		}
		return setServer(args[1], out)
	case "set-api-key":
		if len(args) != 2 {
			return NewUsageError("用法: config set-api-key <key>")
		}
		return setAPIKey(args[1], out)
	case "show":
		fs := flag.NewFlagSet("config show", flag.ContinueOnError)
		jsonOut := fs.Bool("json", false, "以 JSON 输出")
		if err := fs.Parse(args[1:]); err != nil {
			return NewUsageError("config show: %v", err)
		}
		if fs.NArg() > 0 {
			return NewUsageError("config show 不接受额外参数: %s", fs.Arg(0))
		}
		return showConfig(*jsonOut, out)
	default:
		return NewUsageError("未知子命令 %q, 可用: set-server <url> / set-api-key <key> / show", args[0])
	}
}

// setServer 校验并保存 server_url, 保留配置文件中的其他字段.
func setServer(rawURL string, out io.Writer) error {
	rawURL = strings.TrimSpace(rawURL)
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return NewUsageError("server_url 必须是合法的 http/https 地址, 例如 http://127.0.0.1:33333")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	cfg.ServerURL = rawURL
	if err := config.Save(cfg); err != nil {
		return err
	}
	fmt.Fprintf(out, "已保存 server_url: %s\n", rawURL)
	return nil
}

// setAPIKey 保存 api_key. 回显始终脱敏, 避免 Key 进入终端回滚记录.
func setAPIKey(key string, out io.Writer) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return NewUsageError("api_key 不能为空")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	cfg.APIKey = key
	if err := config.Save(cfg); err != nil {
		return err
	}
	fmt.Fprintf(out, "已保存 api_key: %s\n", config.MaskKey(key))
	return nil
}

// configShowJSON 是 config show --json 的输出结构.
// api_key 只输出脱敏形式, 机器模式也不例外, 避免完整 Key 进入
// 管道与日志; 需要真实 Key 的场景请直接读配置文件或环境变量.
type configShowJSON struct {
	ServerURL       string `json:"server_url"`
	ServerURLSource string `json:"server_url_source"`
	APIKeyMasked    string `json:"api_key_masked"`
	APIKeySource    string `json:"api_key_source"`
}

func showConfig(jsonOut bool, out io.Writer) error {
	r, err := config.Resolve()
	if err != nil {
		return err
	}
	if jsonOut {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(configShowJSON{
			ServerURL:       r.ServerURL,
			ServerURLSource: string(r.ServerURLSource),
			APIKeyMasked:    config.MaskKey(r.APIKey),
			APIKeySource:    string(r.APIKeySource),
		})
	}
	fmt.Fprintf(out, "server_url:  %s (%s)\n", displayValue(r.ServerURL), sourceLabel(r.ServerURLSource))
	fmt.Fprintf(out, "api_key:     %s (%s)\n", displayValue(config.MaskKey(r.APIKey)), sourceLabel(r.APIKeySource))
	return nil
}

// displayValue 把空值显示为占位符, 避免输出空行造成误解.
func displayValue(v string) string {
	if v == "" {
		return "(未设置)"
	}
	return v
}

// sourceLabel 把来源常量转为中文说明.
func sourceLabel(s config.Source) string {
	switch s {
	case config.SourceFile:
		return "来源: 配置文件"
	case config.SourceEnv:
		return "来源: 环境变量"
	default:
		return "未设置"
	}
}
