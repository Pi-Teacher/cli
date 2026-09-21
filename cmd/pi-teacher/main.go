// Command pi-teacher 是 Pi Teacher 的远程业务 CLI, 面向用户与 AI agent.
// 它只通过服务端 /api/cli/... REST API 交互, 认证使用服务端地址与 API Key,
// 永不直接连接数据库; 永久删除与系统设置等能力属于 WebUI, 不在本 CLI 范围.
package main

import (
	"fmt"
	"os"

	"github.com/Pi-Teacher/cli/internal/version"
)

// usage 是命令行帮助. 命令分组与服务端 /api/cli 能力清单保持一致,
// 当前仅是骨架, 各命令在后续批次逐步接入.
const usage = `pi-teacher %s - Pi Teacher 业务 CLI (骨架)

用法:
  pi-teacher <command> [subcommand] [flags]

命令分组 (待实现):
  config          set-server / set-api-key / show
  card            list / get / check / create / update / trash / merge
  topic           list / get / create / update / trash
  glossary        list / get / create / update / trash
  review          due / submit
  approvals       list / get
  user-profile    get / set
  system          info

说明:
  CLI 只调用 /api/cli/... 接口; 写命令自动携带 Idempotency-Key.
  永久删除, Embedding 与系统设置等能力属于 WebUI, 不在本 CLI 范围.
`

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "-v", "--version":
			fmt.Println(version.Version)
			return
		}
	}
	fmt.Fprintf(os.Stderr, usage, version.Version)
	os.Exit(2)
}
