package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"

	"github.com/Pi-Teacher/cli/internal/httpclient"
)

// RunSystem 实现 system 子命令组, 当前只有 info.
func RunSystem(args []string, out io.Writer) error {
	if len(args) == 0 {
		return NewUsageError("缺少子命令, 可用: info")
	}
	switch args[0] {
	case "info":
		fs := flag.NewFlagSet("system info", flag.ContinueOnError)
		jsonOut := fs.Bool("json", false, "以 JSON 输出")
		if err := fs.Parse(args[1:]); err != nil {
			return NewUsageError("system info: %v", err)
		}
		if fs.NArg() > 0 {
			return NewUsageError("system info 不接受额外参数: %s", fs.Arg(0))
		}
		return systemInfo(*jsonOut, out)
	default:
		return NewUsageError("未知子命令 %q, 可用: info", args[0])
	}
}

// systemInfoResult 对应 GET /api/cli/system/info 的响应体.
type systemInfoResult struct {
	Version       string `json:"version"`
	GoVersion     string `json:"go_version"`
	DBDriver      string `json:"db_driver"`
	UptimeSeconds int64  `json:"uptime_seconds"`
}

func systemInfo(jsonOut bool, out io.Writer) error {
	client, err := newAPIClient()
	if err != nil {
		return err
	}
	ctx, cancel := requestContext()
	defer cancel()

	var info systemInfoResult
	err = client.Do(ctx, httpclient.Request{
		Method: http.MethodGet,
		Path:   "/api/cli/system/info",
	}, &info)
	if err != nil {
		return err
	}
	if jsonOut {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	}
	fmt.Fprintf(out, "version:        %s\n", info.Version)
	fmt.Fprintf(out, "go_version:     %s\n", info.GoVersion)
	fmt.Fprintf(out, "db_driver:      %s\n", info.DBDriver)
	fmt.Fprintf(out, "uptime_seconds: %d\n", info.UptimeSeconds)
	return nil
}
