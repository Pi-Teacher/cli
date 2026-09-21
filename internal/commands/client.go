package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/Pi-Teacher/cli/internal/config"
	"github.com/Pi-Teacher/cli/internal/httpclient"
)

// requestTimeoutSeconds 是单次 API 请求的整体超时.
// CLI 是交互式工具, 长时间挂起不如快速失败让用户重试.
const requestTimeoutSeconds = 30

// newAPIClient 从合并后的有效配置构建 API 客户端.
// 缺少 server_url 或 api_key 时返回带设置指引的错误,
// 所有远程命令统一走这里, 保证提示一致.
func newAPIClient() (*httpclient.Client, error) {
	r, err := config.Resolve()
	if err != nil {
		return nil, err
	}
	if r.ServerURL == "" || r.APIKey == "" {
		return nil, fmt.Errorf("缺少服务端配置: 请先运行 config set-server <url> 与 config set-api-key <key>, 或设置环境变量 %s / %s",
			config.EnvServerURL, config.EnvAPIKey)
	}
	return httpclient.New(r.ServerURL, r.APIKey, requestTimeoutSeconds), nil
}

// requestContext 返回带整体超时的请求上下文, 与客户端超时互为双保险.
func requestContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), requestTimeoutSeconds*time.Second)
}
