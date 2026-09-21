package commands

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Pi-Teacher/cli/internal/httpclient"
)

// RunTopic 实现 topic 子命令组: list / get / create / update / trash.
func RunTopic(args []string, out io.Writer) error {
	if len(args) == 0 {
		return NewUsageError("缺少子命令, 可用: list / get <id> / create / update <id> / trash <id>")
	}
	switch args[0] {
	case "list":
		return listTopics(args[1:], out)
	case "get":
		return getTopic(args[1:], out)
	case "create":
		return createTopic(args[1:], out)
	case "update":
		return updateTopic(args[1:], out)
	case "trash":
		return trashTopic(args[1:], out)
	default:
		return NewUsageError("未知子命令 %q, 可用: list / get <id> / create / update <id> / trash <id>", args[0])
	}
}

// topicResult 对应服务端 topicResponse.
type topicResult struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CardCount   int64     `json:"card_count"`
	Version     int64     `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// approvalSummary 对应 202 提案响应中的提案摘要.
type approvalSummary struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
}

// topicListResult 对应服务端分页响应 {items, total, page, page_size}.
type topicListResult struct {
	Items    []topicResult `json:"items"`
	Total    int64         `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"page_size"`
}

// splitArgs 把参数切为位置参数与 flag 两段.
// Go 的 flag 包遇到首个非 flag 参数即停止解析, 而用户习惯
// "topic update 7 --expected-version 1" 的顺序, 这里预先把开头的
// 位置参数摘出来, 解析后再与 fs.Args() 合并, 两种顺序都兼容.
func splitArgs(args []string) (positional, flags []string) {
	i := 0
	for i < len(args) && !strings.HasPrefix(args[i], "-") {
		i++
	}
	return args[:i], args[i:]
}

// --- list ---

func listTopics(args []string, out io.Writer) error {
	_, flagArgs := splitArgs(args)
	fs := flag.NewFlagSet("topic list", flag.ContinueOnError)
	page := fs.Int("page", 1, "页码, 从 1 开始")
	pageSize := fs.Int("page-size", 20, "每页条数, 上限 100")
	q := fs.String("q", "", "按名称/描述模糊搜索")
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	if err := fs.Parse(flagArgs); err != nil {
		return NewUsageError("topic list: %v", err)
	}
	if fs.NArg() > 0 {
		return NewUsageError("topic list 不接受额外参数: %s", fs.Arg(0))
	}
	if *page < 1 {
		return NewUsageError("--page 必须从 1 开始")
	}
	if *pageSize < 1 || *pageSize > 100 {
		return NewUsageError("--page-size 必须在 1 到 100 之间")
	}

	client, err := newAPIClient()
	if err != nil {
		return err
	}
	ctx, cancel := requestContext()
	defer cancel()

	var result topicListResult
	err = client.Do(ctx, httpclient.Request{
		Method: http.MethodGet,
		Path:   fmt.Sprintf("/api/cli/topics?page=%d&page_size=%d&q=%s", *page, *pageSize, url.QueryEscape(*q)),
	}, &result)
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(out, result)
	}
	return printTopicList(out, result)
}

// printTopicList 以表格输出 Topic 列表与分页摘要.
func printTopicList(out io.Writer, result topicListResult) error {
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tCARDS\tVERSION\tUPDATED_AT")
	for _, t := range result.Items {
		fmt.Fprintf(tw, "%d\t%s\t%d\t%d\t%s\n",
			t.ID, t.Name, t.CardCount, t.Version, t.UpdatedAt.Local().Format("2006-01-02 15:04"))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	totalPages := (result.Total + int64(result.PageSize) - 1) / int64(result.PageSize)
	if totalPages == 0 {
		totalPages = 1
	}
	fmt.Fprintf(out, "共 %d 条, 第 %d/%d 页 (每页 %d)\n",
		result.Total, result.Page, totalPages, result.PageSize)
	return nil
}

// --- get ---

func getTopic(args []string, out io.Writer) error {
	positional, flagArgs := splitArgs(args)
	fs := flag.NewFlagSet("topic get", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	if err := fs.Parse(flagArgs); err != nil {
		return NewUsageError("topic get: %v", err)
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 {
		return NewUsageError("用法: topic get <id>")
	}
	id, err := parseID(positional[0])
	if err != nil {
		return err
	}

	client, err := newAPIClient()
	if err != nil {
		return err
	}
	ctx, cancel := requestContext()
	defer cancel()

	var topic topicResult
	err = client.Do(ctx, httpclient.Request{
		Method: http.MethodGet,
		Path:   fmt.Sprintf("/api/cli/topics/%d", id),
	}, &topic)
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(out, topic)
	}
	printTopic(out, topic)
	return nil
}

// printTopic 以键值对输出 Topic 详情.
func printTopic(out io.Writer, t topicResult) {
	fmt.Fprintf(out, "id:          %d\n", t.ID)
	fmt.Fprintf(out, "name:        %s\n", t.Name)
	fmt.Fprintf(out, "description: %s\n", displayValue(t.Description))
	fmt.Fprintf(out, "card_count:  %d\n", t.CardCount)
	fmt.Fprintf(out, "version:     %d\n", t.Version)
	fmt.Fprintf(out, "created_at:  %s\n", t.CreatedAt.Local().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(out, "updated_at:  %s\n", t.UpdatedAt.Local().Format("2006-01-02 15:04:05"))
}

// --- create ---

func createTopic(args []string, out io.Writer) error {
	positional, flagArgs := splitArgs(args)
	fs := flag.NewFlagSet("topic create", flag.ContinueOnError)
	name := fs.String("name", "", "Topic 名称 (必填)")
	description := fs.String("description", "", "Topic 描述, 可选")
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	if err := fs.Parse(flagArgs); err != nil {
		return NewUsageError("topic create: %v", err)
	}
	positional = append(positional, fs.Args()...)
	if len(positional) > 0 {
		return NewUsageError("topic create 不接受额外参数: %s", positional[0])
	}
	if *name == "" {
		return NewUsageError("--name 不能为空")
	}

	client, err := newAPIClient()
	if err != nil {
		return err
	}
	ctx, cancel := requestContext()
	defer cancel()

	raw, err := client.DoWrite(ctx, httpclient.Request{
		Method: http.MethodPost,
		Path:   "/api/cli/topics",
		Body:   map[string]string{"name": *name, "description": *description},
	})
	if err != nil {
		return err
	}
	return handleTopicWrite(raw, *jsonOut, out)
}

// --- update ---

func updateTopic(args []string, out io.Writer) error {
	positional, flagArgs := splitArgs(args)
	fs := flag.NewFlagSet("topic update", flag.ContinueOnError)
	expectedVersion := fs.Int64("expected-version", 0, "乐观锁版本号, 取自 topic get 的 version (必填)")
	name := fs.String("name", "", "新名称, 省略表示不修改")
	description := fs.String("description", "", "新描述, 省略表示不修改, 传空串表示清空")
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	if err := fs.Parse(flagArgs); err != nil {
		return NewUsageError("topic update: %v", err)
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 {
		return NewUsageError("用法: topic update <id> --expected-version <v> [--name n] [--description d]")
	}
	id, err := parseID(positional[0])
	if err != nil {
		return err
	}
	if *expectedVersion < 1 {
		return NewUsageError("--expected-version 必填且为正整数")
	}

	// flag.Visit 区分"显式设置"与"默认值": 只有显式设置的字段才进入
	// 请求体, 服务端按字段可选语义只更新提供的字段.
	setFlags := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })
	if !setFlags["name"] && !setFlags["description"] {
		return NewUsageError("至少提供 --name 或 --description 之一")
	}
	body := map[string]any{"expected_version": *expectedVersion}
	if setFlags["name"] {
		body["name"] = *name
	}
	if setFlags["description"] {
		body["description"] = *description
	}

	client, err := newAPIClient()
	if err != nil {
		return err
	}
	ctx, cancel := requestContext()
	defer cancel()

	raw, err := client.DoWrite(ctx, httpclient.Request{
		Method: http.MethodPatch,
		Path:   fmt.Sprintf("/api/cli/topics/%d", id),
		Body:   body,
	})
	if err != nil {
		return wrapVersionConflict(err)
	}
	return handleTopicWrite(raw, *jsonOut, out)
}

// --- trash ---

func trashTopic(args []string, out io.Writer) error {
	positional, flagArgs := splitArgs(args)
	fs := flag.NewFlagSet("topic trash", flag.ContinueOnError)
	expectedVersion := fs.Int64("expected-version", 0, "乐观锁版本号, 取自 topic get 的 version (必填)")
	includeCards := fs.Bool("include-cards", false, "连同 Topic 下的卡一并回收, 缺省只回收 Topic 本身")
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	if err := fs.Parse(flagArgs); err != nil {
		return NewUsageError("topic trash: %v", err)
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 {
		return NewUsageError("用法: topic trash <id> --expected-version <v> [--include-cards]")
	}
	id, err := parseID(positional[0])
	if err != nil {
		return err
	}
	if *expectedVersion < 1 {
		return NewUsageError("--expected-version 必填且为正整数")
	}

	client, err := newAPIClient()
	if err != nil {
		return err
	}
	ctx, cancel := requestContext()
	defer cancel()

	raw, err := client.DoWrite(ctx, httpclient.Request{
		Method: http.MethodPost,
		Path:   fmt.Sprintf("/api/cli/topics/%d/trash", id),
		Body:   map[string]any{"expected_version": *expectedVersion, "include_cards": *includeCards},
	})
	if err != nil {
		return wrapVersionConflict(err)
	}

	// trash 的直写结果不是 Topic 详情, 单独分支处理.
	if raw.Status == http.StatusAccepted {
		return printProposalOutcome(raw, *jsonOut, out)
	}
	var trashed struct {
		TrashedTopicID int64 `json:"trashed_topic_id"`
		AffectedCards  int64 `json:"affected_cards"`
	}
	if err := json.Unmarshal(raw.Body, &trashed); err != nil {
		return fmt.Errorf("响应体不是合法 JSON: %w", err)
	}
	if *jsonOut {
		return writeJSON(out, map[string]any{
			"outcome":          "applied",
			"trashed_topic_id": trashed.TrashedTopicID,
			"affected_cards":   trashed.AffectedCards,
		})
	}
	// include_cards 两种语义的服务端行为不同: true 时关联卡一并进回收站,
	// false 时仅解除关联 (卡变为无 Topic 卡, version 加一), 文案必须区分,
	// 否则会误导用户以为卡已进回收站.
	if *includeCards {
		fmt.Fprintf(out, "已回收 topic %d, 关联卡 %d 张一并回收\n", trashed.TrashedTopicID, trashed.AffectedCards)
	} else {
		fmt.Fprintf(out, "已回收 topic %d, 关联卡 %d 张已解除关联 (变为无 Topic 卡)\n", trashed.TrashedTopicID, trashed.AffectedCards)
	}
	return nil
}

// --- 写结果公共处理 ---

// handleTopicWrite 处理 create/update 的双形态响应:
// 202 为提案 (未生效), 2xx 为直写结果 (Topic 详情).
func handleTopicWrite(raw *httpclient.RawResponse, jsonOut bool, out io.Writer) error {
	if raw.Status == http.StatusAccepted {
		return printProposalOutcome(raw, jsonOut, out)
	}
	var topic topicResult
	if err := json.Unmarshal(raw.Body, &topic); err != nil {
		return fmt.Errorf("响应体不是合法 JSON: %w", err)
	}
	if jsonOut {
		return writeJSON(out, map[string]any{"outcome": "applied", "topic": topic})
	}
	printTopic(out, topic)
	return nil
}

// printProposalOutcome 展示 202 提案结果, 明确标注未生效,
// 避免用户或 agent 把提案误当成已执行.
func printProposalOutcome(raw *httpclient.RawResponse, jsonOut bool, out io.Writer) error {
	var resp struct {
		Approval approvalSummary `json:"approval"`
	}
	if err := json.Unmarshal(raw.Body, &resp); err != nil {
		return fmt.Errorf("提案响应体不是合法 JSON: %w", err)
	}
	if jsonOut {
		return writeJSON(out, map[string]any{"outcome": "proposal", "approval": resp.Approval})
	}
	fmt.Fprintln(out, "已提交提案, 等待审批 (未生效):")
	fmt.Fprintf(out, "  approval_id: %d\n", resp.Approval.ID)
	fmt.Fprintf(out, "  status:     %s\n", resp.Approval.Status)
	fmt.Fprintln(out, "可用 pi-teacher-cli approvals get <id> 查看进度")
	return nil
}

// wrapVersionConflict 给乐观锁冲突附加重试指引.
// 服务端 409 表示数据已被并发修改, CLI 不自动重试,
// 由用户或 agent 重新读取最新 version 后重新决策.
func wrapVersionConflict(err error) error {
	var apiErr *httpclient.APIError
	if errors.As(err, &apiErr) && apiErr.Code == "version_conflict" {
		return fmt.Errorf("%w\n提示: 数据已被其他修改更新, 请重新读取最新 version 后重试", err)
	}
	return err
}

// --- 公共小工具 ---

// parseID 解析正整数路径参数.
func parseID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, NewUsageError("id 必须是正整数, got %q", raw)
	}
	return id, nil
}

// writeJSON 统一以缩进 JSON 输出机器可读结果.
func writeJSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
