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

// RunApprovals 实现 approvals 子命令组: list / get.
// CLI 只能查看本 API Key 发起的提案, 非本人发起的详情返回 404;
// 审批与拒绝动作属于 WebUI, 不在 CLI 范围.
func RunApprovals(args []string, out io.Writer) error {
	if len(args) == 0 {
		return NewUsageError("缺少子命令, 可用: list / get <id>")
	}
	switch args[0] {
	case "list":
		return listApprovals(args[1:], out)
	case "get":
		return getApproval(args[1:], out)
	default:
		return NewUsageError("未知子命令 %q, 可用: list / get <id>", args[0])
	}
}

// approvalResult 对应服务端 approvalResponse.
// Payload 两个原始 JSON 保留原样, --json 时透传, 人类模式摘要展示.
type approvalResult struct {
	ID                  int64            `json:"id"`
	Operation           string           `json:"operation"`
	EntityType          string           `json:"entity_type"`
	Status              string           `json:"status"`
	RequestedByAPIKeyID *int64           `json:"requested_by_api_key_id"`
	OriginalPayload     json.RawMessage  `json:"original_payload"`
	ApprovedPayload     json.RawMessage  `json:"approved_payload"`
	Reason              *string          `json:"reason"`
	Targets             []approvalTarget `json:"targets,omitempty"`
	CreatedAt           time.Time        `json:"created_at"`
	ProcessedAt         *time.Time       `json:"processed_at"`
}

// approvalTarget 对应提案影响的对象与基准版本.
type approvalTarget struct {
	EntityType  string `json:"entity_type"`
	EntityID    int64  `json:"entity_id"`
	BaseVersion int64  `json:"base_version"`
	Role        string `json:"role"`
}

// approvalListResult 对应服务端分页响应.
type approvalListResult struct {
	Items    []approvalResult `json:"items"`
	Total    int64            `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
}

// approvalStatuses 是 status 筛选的合法取值, 与服务端 ParseApprovalStatus 一致.
var approvalStatuses = map[string]bool{
	"pending": true, "approved": true, "rejected": true,
	"cancelled": true, "stale": true,
}

// --- list ---

func listApprovals(args []string, out io.Writer) error {
	_, flagArgs := splitArgs(args)
	fs := flag.NewFlagSet("approvals list", flag.ContinueOnError)
	page := fs.Int("page", 1, "页码, 从 1 开始")
	pageSize := fs.Int("page-size", 20, "每页条数, 上限 100")
	status := fs.String("status", "", "按状态筛选: pending / approved / rejected / cancelled / stale")
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	if err := fs.Parse(flagArgs); err != nil {
		return NewUsageError("approvals list: %v", err)
	}
	if fs.NArg() > 0 {
		return NewUsageError("approvals list 不接受额外参数: %s", fs.Arg(0))
	}
	if *page < 1 {
		return NewUsageError("--page 必须从 1 开始")
	}
	if *pageSize < 1 || *pageSize > 100 {
		return NewUsageError("--page-size 必须在 1 到 100 之间")
	}
	if *status != "" && !approvalStatuses[*status] {
		return NewUsageError("--status 取值: pending / approved / rejected / cancelled / stale")
	}

	query := url.Values{}
	query.Set("page", fmt.Sprint(*page))
	query.Set("page_size", fmt.Sprint(*pageSize))
	if *status != "" {
		query.Set("status", *status)
	}

	client, err := newAPIClient()
	if err != nil {
		return err
	}
	ctx, cancel := requestContext()
	defer cancel()

	var result approvalListResult
	err = client.Do(ctx, httpclient.Request{
		Method: http.MethodGet,
		Path:   "/api/cli/approvals?" + query.Encode(),
	}, &result)
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(out, result)
	}

	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tOPERATION\tENTITY\tSTATUS\tCREATED_AT")
	for _, a := range result.Items {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n",
			a.ID, a.Operation, a.EntityType, a.Status,
			a.CreatedAt.Local().Format("2006-01-02 15:04"))
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

func getApproval(args []string, out io.Writer) error {
	positional, flagArgs := splitArgs(args)
	fs := flag.NewFlagSet("approvals get", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	if err := fs.Parse(flagArgs); err != nil {
		return NewUsageError("approvals get: %v", err)
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 {
		return NewUsageError("用法: approvals get <id>")
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

	var detail approvalResult
	err = client.Do(ctx, httpclient.Request{
		Method: http.MethodGet,
		Path:   fmt.Sprintf("/api/cli/approvals/%d", id),
	}, &detail)
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(out, detail)
	}

	fmt.Fprintf(out, "id:            %d\n", detail.ID)
	fmt.Fprintf(out, "operation:     %s\n", detail.Operation)
	fmt.Fprintf(out, "entity_type:   %s\n", detail.EntityType)
	fmt.Fprintf(out, "status:        %s\n", detail.Status)
	fmt.Fprintf(out, "created_at:    %s\n", detail.CreatedAt.Local().Format("2006-01-02 15:04:05"))
	if detail.ProcessedAt != nil {
		fmt.Fprintf(out, "processed_at:  %s\n", detail.ProcessedAt.Local().Format("2006-01-02 15:04:05"))
	}
	if detail.Reason != nil {
		fmt.Fprintf(out, "reason:        %s\n", *detail.Reason)
	}
	// payload 可能较大, 人类模式只展示原始提案内容摘要, 完整内容用 --json.
	if len(detail.OriginalPayload) > 0 && string(detail.OriginalPayload) != "null" {
		fmt.Fprintf(out, "original_payload:\n%s\n", indentJSON(detail.OriginalPayload))
	}
	if len(detail.ApprovedPayload) > 0 && string(detail.ApprovedPayload) != "null" {
		fmt.Fprintf(out, "approved_payload:\n%s\n", indentJSON(detail.ApprovedPayload))
	}
	if len(detail.Targets) > 0 {
		fmt.Fprintln(out, "targets:")
		for _, t := range detail.Targets {
			fmt.Fprintf(out, "  %s #%d (base_version %d, role %s)\n",
				t.EntityType, t.EntityID, t.BaseVersion, t.Role)
		}
	}
	return nil
}

// indentJSON 把 JSON 片段格式化为缩进文本, 失败时原样返回.
func indentJSON(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	pretty, err := json.MarshalIndent(v, "  ", "  ")
	if err != nil {
		return string(raw)
	}
	return "  " + string(pretty)
}
