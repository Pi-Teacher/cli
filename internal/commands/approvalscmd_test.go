package commands

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Pi-Teacher/cli/internal/httpclient"
)

const approvalListJSON = `{"items":[{"id":14,"operation":"card_create","entity_type":"card",` +
	`"status":"pending","requested_by_api_key_id":1,` +
	`"original_payload":{"front":"x"},"approved_payload":null,"reason":null,` +
	`"created_at":"2026-09-21T12:00:00Z","processed_at":null}],` +
	`"total":1,"page":1,"page_size":20}`

const approvalDetailJSON = `{"id":14,"operation":"card_create","entity_type":"card",` +
	`"status":"approved","requested_by_api_key_id":1,` +
	`"original_payload":{"front":"x","back":"y"},` +
	`"approved_payload":{"front":"x","back":"y"},"reason":"同意",` +
	`"targets":[{"entity_type":"card","entity_id":0,"base_version":0,"role":"create"}],` +
	`"created_at":"2026-09-21T12:00:00Z","processed_at":"2026-09-21T12:05:00Z"}`

func TestApprovalsList(t *testing.T) {
	ts := newCardTestServer(t, func(w http.ResponseWriter, path string) {
		w.Write([]byte(approvalListJSON))
	})
	setupCardConfig(t, ts)

	var out strings.Builder
	if err := RunApprovals([]string{"list", "--status", "pending"}, &out); err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := ts.requests[0].Query.Get("status"); got != "pending" {
		t.Fatalf("status 参数不符: %q", got)
	}
	for _, want := range []string{"14", "card_create", "card", "pending", "共 1 条"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("输出应包含 %q: %q", want, out.String())
		}
	}

	// 未设置 --status 时不应筛选.
	out.Reset()
	if err := RunApprovals([]string{"list"}, &out); err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := ts.requests[1].Query.Get("status"); got != "" {
		t.Fatalf("未设置 status 不应出现在查询串: %q", got)
	}
}

func TestApprovalsGet(t *testing.T) {
	ts := newCardTestServer(t, func(w http.ResponseWriter, path string) {
		if path == "/api/cli/approvals/14" {
			w.Write([]byte(approvalDetailJSON))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":{"code":"not_found","message":"提案不存在或非本人发起"}}`))
	})
	setupCardConfig(t, ts)

	var out strings.Builder
	if err := RunApprovals([]string{"get", "14"}, &out); err != nil {
		t.Fatalf("get: %v", err)
	}
	for _, want := range []string{
		"operation:     card_create",
		"status:        approved",
		"reason:        同意",
		"original_payload:",
		"card #0 (base_version 0, role create)",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("输出应包含 %q: %q", want, out.String())
		}
	}

	err := RunApprovals([]string{"get", "999"}, &out)
	if err == nil {
		t.Fatal("404 应返回错误")
	}
	if !strings.Contains(err.Error(), "非本人发起") {
		t.Fatalf("404 信息应说明权限语义: %v", err)
	}
}

func TestApprovalsUsageErrors(t *testing.T) {
	setupCardConfig(t, newCardTestServer(t, func(w http.ResponseWriter, path string) {}))
	var out strings.Builder
	for _, args := range [][]string{
		{},
		{"bogus"},
		{"list", "--status", "bogus"},
		{"list", "--page", "0"},
		{"get"},
		{"get", "x"},
	} {
		if err := RunApprovals(args, &out); err == nil {
			t.Fatalf("参数 %v 应报用法错误", args)
		}
	}
}

// TestExitCodeFullMapping 覆盖完整退出码映射表,
// 服务端新增错误码而映射表漏配时, 该测试的未知 code 分支提醒补录.
func TestExitCodeFullMapping(t *testing.T) {
	cases := []struct {
		code string
		want int
	}{
		{"unauthorized", 3},
		{"version_conflict", 4},
		{"merge_topic_required", 10},
		{"merge_embedding_required", 11},
		{"similarity_disabled", 9},
		{"forbidden", 5},
		{"not_found", 6},
		{"rate_limited", 7},
		{"embedding_unavailable", 8},
		{"validation_error", 1},     // 未细分, 走通用
		{"idempotency_conflict", 1}, // 未细分, 走通用
		{"internal_error", 1},       // 未细分, 走通用
		{"totally_unknown", 1},
	}
	for _, c := range cases {
		err := &httpclient.APIError{Status: 400, Code: c.code, Message: "x"}
		if got := ExitCode(err); got != c.want {
			t.Fatalf("code %q 退出码应为 %d, got %d", c.code, c.want, got)
		}
	}
	if got := ExitCode(nil); got != 0 {
		t.Fatalf("nil 退出码应为 0, got %d", got)
	}
	if got := ExitCode(NewUsageError("x")); got != 2 {
		t.Fatalf("usage 错误退出码应为 2, got %d", got)
	}
	if got := ExitCode(errPlain); got != 1 {
		t.Fatalf("网络错误退出码应为 1, got %d", got)
	}
}
