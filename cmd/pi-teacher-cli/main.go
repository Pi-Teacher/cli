// Command pi-teacher-cli 是 Pi Teacher 的远程业务 CLI, 面向用户与 AI agent.
// 它只通过服务端 /api/cli/... REST API 交互, 认证使用服务端地址与 API Key,
// 永不直接连接数据库; 永久删除与系统设置等能力属于 WebUI, 不在本 CLI 范围.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/Pi-Teacher/cli/internal/commands"
	"github.com/Pi-Teacher/cli/internal/version"
)

// usage 是命令行帮助. 命令分组与服务端 /api/cli 能力清单保持一致,
// 已实现的分组标注命令, 未实现的标注待实现.
const usage = `pi-teacher-cli %s - Pi Teacher 业务 CLI

用法:
  pi-teacher-cli <command> [subcommand] [flags]

命令分组:
  config          set-server / set-api-key / show
  system          info
  topic           list / get / create / update / trash
  card            list / get / create / update / trash / check / merge
  trash           cards|topics|glossary list / restore
  glossary        list / get / create / update / trash
  review          due / submit
  approvals       list / get
  user-profile    get / set

说明:
  CLI 只调用 /api/cli/... 接口; 写命令自动携带 Idempotency-Key.
  永久删除, Embedding 与系统设置等能力属于 WebUI, 不在本 CLI 范围.
  审批开关开启时写命令返回 202 提案 (未生效), 关闭时直写生效.
  退出码: 0 成功 / 1 一般错误 / 2 用法错误 / 3 认证失败 /
  4 版本冲突 / 5 无权限 / 6 不存在 / 7 限流 / 8 embedding 不可用 /
  9 相似度未开放 / 10 合并需指定 Topic / 11 合并需指定 embedding.
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		var usageErr *commands.UsageError
		if errors.As(err, &usageErr) {
			fmt.Fprintf(os.Stderr, "用法错误: %s\n\n", err)
			fmt.Fprintf(os.Stderr, usage, version.Version)
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(commands.ExitCode(err))
	}
}

// run 分发顶层命令. 无参数时打印帮助并以 2 退出, 与 usage 错误一致.
func run(args []string) error {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, usage, version.Version)
		os.Exit(2)
	}
	switch args[0] {
	case "version", "-v", "--version":
		fmt.Println(version.Version)
		return nil
	case "config":
		return commands.RunConfig(args[1:], os.Stdout)
	case "system":
		return commands.RunSystem(args[1:], os.Stdout)
	case "topic":
		return commands.RunTopic(args[1:], os.Stdout)
	case "card":
		return commands.RunCard(args[1:], os.Stdout)
	case "trash":
		return commands.RunTrash(args[1:], os.Stdout)
	case "glossary":
		return commands.RunGlossary(args[1:], os.Stdout)
	case "review":
		return commands.RunReview(args[1:], os.Stdout)
	case "user-profile":
		return commands.RunUserProfile(args[1:], os.Stdout)
	case "approvals":
		return commands.RunApprovals(args[1:], os.Stdout)
	default:
		return commands.NewUsageError("未知命令 %q", args[0])
	}
}
