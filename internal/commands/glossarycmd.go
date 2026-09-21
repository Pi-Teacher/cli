package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"text/tabwriter"
	"time"

	"github.com/Pi-Teacher/cli/internal/httpclient"
)

// RunGlossary 实现 glossary 子命令组: list / get / create / update / trash.
// 回收站恢复属于 trash 命令组.
func RunGlossary(args []string, out io.Writer) error {
	if len(args) == 0 {
		return NewUsageError("缺少子命令, 可用: list / get <id> / create / update <id> / trash <id>")
	}
	switch args[0] {
	case "list":
		return listGlossary(args[1:], out)
	case "get":
		return getGlossary(args[1:], out)
	case "create":
		return createGlossary(args[1:], out)
	case "update":
		return updateGlossary(args[1:], out)
	case "trash":
		return trashGlossary(args[1:], out)
	default:
		return NewUsageError("未知子命令 %q, 可用: list / get <id> / create / update <id> / trash <id>", args[0])
	}
}

// glossaryResult 对应服务端 glossaryResponse.
type glossaryResult struct {
	ID         int64     `json:"id"`
	Term       string    `json:"term"`
	Definition string    `json:"definition"`
	Version    int64     `json:"version"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// glossaryListResult 对应服务端分页响应.
type glossaryListResult struct {
	Items    []glossaryResult `json:"items"`
	Total    int64            `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
}

// --- list ---

func listGlossary(args []string, out io.Writer) error {
	_, flagArgs := splitArgs(args)
	fs := flag.NewFlagSet("glossary list", flag.ContinueOnError)
	page := fs.Int("page", 1, "页码, 从 1 开始")
	pageSize := fs.Int("page-size", 20, "每页条数, 上限 100")
	q := fs.String("q", "", "按词条/释义模糊搜索")
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	if err := fs.Parse(flagArgs); err != nil {
		return NewUsageError("glossary list: %v", err)
	}
	if fs.NArg() > 0 {
		return NewUsageError("glossary list 不接受额外参数: %s", fs.Arg(0))
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

	var result glossaryListResult
	err = client.Do(ctx, httpclient.Request{
		Method: http.MethodGet,
		Path: fmt.Sprintf("/api/cli/glossary?page=%d&page_size=%d&q=%s",
			*page, *pageSize, url.QueryEscape(*q)),
	}, &result)
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(out, result)
	}

	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTERM\tVERSION\tUPDATED_AT")
	for _, g := range result.Items {
		fmt.Fprintf(tw, "%d\t%s\t%d\t%s\n",
			g.ID, truncateRunes(g.Term, 30), g.Version, g.UpdatedAt.Local().Format("2006-01-02 15:04"))
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

func getGlossary(args []string, out io.Writer) error {
	positional, flagArgs := splitArgs(args)
	fs := flag.NewFlagSet("glossary get", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	if err := fs.Parse(flagArgs); err != nil {
		return NewUsageError("glossary get: %v", err)
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 {
		return NewUsageError("用法: glossary get <id>")
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

	var glossary glossaryResult
	err = client.Do(ctx, httpclient.Request{
		Method: http.MethodGet,
		Path:   fmt.Sprintf("/api/cli/glossary/%d", id),
	}, &glossary)
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(out, glossary)
	}
	printGlossary(out, glossary)
	return nil
}

// printGlossary 以键值对输出词条详情.
func printGlossary(out io.Writer, g glossaryResult) {
	fmt.Fprintf(out, "id:         %d\n", g.ID)
	fmt.Fprintf(out, "term:       %s\n", g.Term)
	fmt.Fprintf(out, "definition: %s\n", g.Definition)
	fmt.Fprintf(out, "version:    %d\n", g.Version)
	fmt.Fprintf(out, "created_at: %s\n", g.CreatedAt.Local().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(out, "updated_at: %s\n", g.UpdatedAt.Local().Format("2006-01-02 15:04:05"))
}

// --- create ---

func createGlossary(args []string, out io.Writer) error {
	_, flagArgs := splitArgs(args)
	fs := flag.NewFlagSet("glossary create", flag.ContinueOnError)
	term := fs.String("term", "", "词条术语 (必填)")
	definition := fs.String("definition", "", "词条释义 (必填)")
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	if err := fs.Parse(flagArgs); err != nil {
		return NewUsageError("glossary create: %v", err)
	}
	if fs.NArg() > 0 {
		return NewUsageError("glossary create 不接受额外参数: %s", fs.Arg(0))
	}
	if *term == "" {
		return NewUsageError("--term 不能为空")
	}
	if *definition == "" {
		return NewUsageError("--definition 不能为空")
	}

	client, err := newAPIClient()
	if err != nil {
		return err
	}
	ctx, cancel := requestContext()
	defer cancel()

	raw, err := client.DoWrite(ctx, httpclient.Request{
		Method: http.MethodPost,
		Path:   "/api/cli/glossary",
		Body:   map[string]string{"term": *term, "definition": *definition},
	})
	if err != nil {
		return err
	}
	return handleGlossaryWrite(raw, *jsonOut, out)
}

// --- update ---

func updateGlossary(args []string, out io.Writer) error {
	positional, flagArgs := splitArgs(args)
	fs := flag.NewFlagSet("glossary update", flag.ContinueOnError)
	expectedVersion := fs.Int64("expected-version", 0, "乐观锁版本号, 取自 glossary get 的 version (必填)")
	term := fs.String("term", "", "新词条, 省略表示不修改")
	definition := fs.String("definition", "", "新释义, 省略表示不修改, 传空串表示清空")
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	if err := fs.Parse(flagArgs); err != nil {
		return NewUsageError("glossary update: %v", err)
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 {
		return NewUsageError("用法: glossary update <id> --expected-version <v> [--term t] [--definition d]")
	}
	id, err := parseID(positional[0])
	if err != nil {
		return err
	}
	if *expectedVersion < 1 {
		return NewUsageError("--expected-version 必填且为正整数")
	}

	// 与 topic update 相同的三态语义: 只有显式设置的字段进入请求体.
	setFlags := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })
	if !setFlags["term"] && !setFlags["definition"] {
		return NewUsageError("至少提供 --term 或 --definition 之一")
	}
	body := map[string]any{"expected_version": *expectedVersion}
	if setFlags["term"] {
		body["term"] = *term
	}
	if setFlags["definition"] {
		body["definition"] = *definition
	}

	client, err := newAPIClient()
	if err != nil {
		return err
	}
	ctx, cancel := requestContext()
	defer cancel()

	raw, err := client.DoWrite(ctx, httpclient.Request{
		Method: http.MethodPatch,
		Path:   fmt.Sprintf("/api/cli/glossary/%d", id),
		Body:   body,
	})
	if err != nil {
		return wrapVersionConflict(err)
	}
	return handleGlossaryWrite(raw, *jsonOut, out)
}

// --- trash ---

func trashGlossary(args []string, out io.Writer) error {
	positional, flagArgs := splitArgs(args)
	fs := flag.NewFlagSet("glossary trash", flag.ContinueOnError)
	expectedVersion := fs.Int64("expected-version", 0, "乐观锁版本号, 取自 glossary get 的 version (必填)")
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	if err := fs.Parse(flagArgs); err != nil {
		return NewUsageError("glossary trash: %v", err)
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 {
		return NewUsageError("用法: glossary trash <id> --expected-version <v>")
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
		Path:   fmt.Sprintf("/api/cli/glossary/%d/trash", id),
		Body:   map[string]any{"expected_version": *expectedVersion},
	})
	if err != nil {
		return wrapVersionConflict(err)
	}
	if raw.Status == http.StatusAccepted {
		return printProposalOutcome(raw, *jsonOut, out)
	}
	var trashed struct {
		TrashedGlossaryID int64 `json:"trashed_glossary_id"`
	}
	if err := json.Unmarshal(raw.Body, &trashed); err != nil {
		return fmt.Errorf("响应体不是合法 JSON: %w", err)
	}
	if *jsonOut {
		return writeJSON(out, map[string]any{"outcome": "applied", "trashed_glossary_id": trashed.TrashedGlossaryID})
	}
	fmt.Fprintf(out, "已回收 glossary %d\n", trashed.TrashedGlossaryID)
	return nil
}

// --- 写结果公共处理 ---

// handleGlossaryWrite 处理 create/update 的双形态响应:
// 202 为提案 (未生效), 2xx 为直写结果 (词条详情).
func handleGlossaryWrite(raw *httpclient.RawResponse, jsonOut bool, out io.Writer) error {
	if raw.Status == http.StatusAccepted {
		return printProposalOutcome(raw, jsonOut, out)
	}
	var glossary glossaryResult
	if err := json.Unmarshal(raw.Body, &glossary); err != nil {
		return fmt.Errorf("响应体不是合法 JSON: %w", err)
	}
	if jsonOut {
		return writeJSON(out, map[string]any{"outcome": "applied", "glossary": glossary})
	}
	printGlossary(out, glossary)
	return nil
}
