package commands

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// topicTestServer 启动一个模拟 topic API 的测试服务端,
// 记录收到的请求供断言.
type topicTestServer struct {
	*httptest.Server
	requests []recordedRequest
}

type recordedRequest struct {
	Method string
	Path   string
	Query  url.Values
	Body   map[string]any
}

// newTopicTestServer 用 handler 生成响应; handler 可通过 t.requests 断言请求.
func newTopicTestServer(t *testing.T, respond func(w http.ResponseWriter, r *http.Request)) *topicTestServer {
	t.Helper()
	ts := &topicTestServer{}
	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		if r.Body != nil {
			data, _ := io.ReadAll(r.Body)
			if len(data) > 0 {
				json.Unmarshal(data, &body)
			}
		}
		ts.requests = append(ts.requests, recordedRequest{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(), Body: body})
		respond(w, r)
	}))
	t.Cleanup(ts.Close)
	return ts
}

// setupTopicConfig 把 CLI 配置指向测试服务端.
func setupTopicConfig(t *testing.T, ts *topicTestServer) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PI_TEACHER_SERVER_URL", ts.URL)
	t.Setenv("PI_TEACHER_API_KEY", "ptk_testkey")
}

const topicJSON = `{"id":7,"name":"Go","description":"golang","card_count":3,"version":2,` +
	`"created_at":"2026-09-20T10:00:00Z","updated_at":"2026-09-21T11:00:00Z"}`

func TestTopicListHumanAndJSON(t *testing.T) {
	ts := newTopicTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"items":[` + topicJSON + `],"total":1,"page":2,"page_size":10}`))
	})
	setupTopicConfig(t, ts)

	var out strings.Builder
	if err := RunTopic([]string{"list", "--page", "2", "--page-size", "10", "--q", "g o"}, &out); err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(ts.requests) != 1 {
		t.Fatalf("应只发一次请求, got %d", len(ts.requests))
	}
	req := ts.requests[0]
	if req.Method != http.MethodGet || req.Path != "/api/cli/topics" {
		t.Fatalf("请求不符: %s %s", req.Method, req.Path)
	}
	// q 含空格, 应被 URL 编码后仍能被服务端按原始值解回.
	if got := ts.requests[0].Query.Get("q"); got != "g o" {
		t.Fatalf("q 参数应保留空格, got %q", got)
	}
	if got := ts.requests[0].Query.Get("page"); got != "2" {
		t.Fatalf("page 参数不符: %q", got)
	}
	for _, want := range []string{"7", "Go", "3", "共 1 条, 第 2/1 页"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("输出应包含 %q: %q", want, out.String())
		}
	}

	out.Reset()
	if err := RunTopic([]string{"list", "--json"}, &out); err != nil {
		t.Fatalf("list --json: %v", err)
	}
	if !strings.Contains(out.String(), `"card_count": 3`) {
		t.Fatalf("--json 输出不符: %q", out.String())
	}
}

func TestTopicGet(t *testing.T) {
	ts := newTopicTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/cli/topics/7" {
			w.Write([]byte(topicJSON))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":{"code":"not_found","message":"topic 不存在"}}`))
	})
	setupTopicConfig(t, ts)

	var out strings.Builder
	if err := RunTopic([]string{"get", "7"}, &out); err != nil {
		t.Fatalf("get: %v", err)
	}
	for _, want := range []string{"id:          7", "name:        Go", "version:     2"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("输出应包含 %q: %q", want, out.String())
		}
	}

	err := RunTopic([]string{"get", "999"}, &out)
	if err == nil {
		t.Fatal("404 应返回错误")
	}
}

func TestTopicCreateDirectWrite(t *testing.T) {
	ts := newTopicTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(topicJSON))
	})
	setupTopicConfig(t, ts)

	var out strings.Builder
	if err := RunTopic([]string{"create", "--name", "Go", "--description", "golang"}, &out); err != nil {
		t.Fatalf("create: %v", err)
	}
	req := ts.requests[0]
	if req.Method != http.MethodPost || req.Path != "/api/cli/topics" {
		t.Fatalf("请求不符: %s %s", req.Method, req.Path)
	}
	if req.Body["name"] != "Go" || req.Body["description"] != "golang" {
		t.Fatalf("请求体不符: %v", req.Body)
	}
	if !strings.Contains(out.String(), "name:        Go") {
		t.Fatalf("直写结果应输出详情: %q", out.String())
	}
	if strings.Contains(out.String(), "提案") {
		t.Fatalf("直写不应显示提案文案: %q", out.String())
	}
}

func TestTopicCreateProposal(t *testing.T) {
	ts := newTopicTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"approval":{"id":15,"status":"pending"}}`))
	})
	setupTopicConfig(t, ts)

	var out strings.Builder
	if err := RunTopic([]string{"create", "--name", "Go"}, &out); err != nil {
		t.Fatalf("create: %v", err)
	}
	for _, want := range []string{"未生效", "approval_id: 15", "status:     pending", "approvals get"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("提案输出应包含 %q: %q", want, out.String())
		}
	}

	out.Reset()
	if err := RunTopic([]string{"create", "--name", "Go", "--json"}, &out); err != nil {
		t.Fatalf("create --json: %v", err)
	}
	if !strings.Contains(out.String(), `"outcome": "proposal"`) {
		t.Fatalf("--json 应标注 proposal: %q", out.String())
	}
}

func TestTopicUpdateOnlySendsSetFlags(t *testing.T) {
	ts := newTopicTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(topicJSON))
	})
	setupTopicConfig(t, ts)

	var out strings.Builder
	// 只设置 --name: description 不应出现在请求体.
	if err := RunTopic([]string{"update", "7", "--expected-version", "2", "--name", "New"}, &out); err != nil {
		t.Fatalf("update: %v", err)
	}
	req := ts.requests[0]
	if req.Method != http.MethodPatch || req.Path != "/api/cli/topics/7" {
		t.Fatalf("请求不符: %s %s", req.Method, req.Path)
	}
	if req.Body["name"] != "New" {
		t.Fatalf("name 应在请求体: %v", req.Body)
	}
	if _, ok := req.Body["description"]; ok {
		t.Fatalf("未设置的 description 不应进请求体: %v", req.Body)
	}
	if req.Body["expected_version"].(float64) != 2 {
		t.Fatalf("expected_version 不符: %v", req.Body)
	}

	// 显式传空串 description 表示清空, 应进入请求体.
	if err := RunTopic([]string{"update", "7", "--expected-version", "2", "--description", ""}, &out); err != nil {
		t.Fatalf("update 清空描述: %v", err)
	}
	if v, ok := ts.requests[1].Body["description"]; !ok || v != "" {
		t.Fatalf("显式空串应进请求体: %v", ts.requests[1].Body)
	}
}

func TestTopicUpdateRequiresAtLeastOneField(t *testing.T) {
	setupTopicConfig(t, newTopicTestServer(t, func(w http.ResponseWriter, r *http.Request) {}))
	var out strings.Builder
	err := RunTopic([]string{"update", "7", "--expected-version", "2"}, &out)
	if err == nil {
		t.Fatal("无字段可更新应报用法错误")
	}
}

func TestTopicUpdateVersionConflictHint(t *testing.T) {
	ts := newTopicTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":{"code":"version_conflict","message":"版本不一致"}}`))
	})
	setupTopicConfig(t, ts)

	var out strings.Builder
	err := RunTopic([]string{"update", "7", "--expected-version", "1", "--name", "New"}, &out)
	if err == nil {
		t.Fatal("409 应返回错误")
	}
	if !strings.Contains(err.Error(), "version_conflict") || !strings.Contains(err.Error(), "重新读取最新 version") {
		t.Fatalf("冲突错误应附重试指引: %v", err)
	}
}

func TestTopicTrashDirectWriteAndProposal(t *testing.T) {
	ts := newTopicTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/trash") {
			w.Write([]byte(`{"trashed_topic_id":7,"affected_cards":4}`))
			return
		}
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"approval":{"id":16,"status":"pending"}}`))
	})
	setupTopicConfig(t, ts)

	var out strings.Builder
	if err := RunTopic([]string{"trash", "7", "--expected-version", "2", "--include-cards"}, &out); err != nil {
		t.Fatalf("trash: %v", err)
	}
	req := ts.requests[0]
	if req.Method != http.MethodPost || req.Path != "/api/cli/topics/7/trash" {
		t.Fatalf("请求不符: %s %s", req.Method, req.Path)
	}
	if req.Body["include_cards"] != true || req.Body["expected_version"].(float64) != 2 {
		t.Fatalf("请求体不符: %v", req.Body)
	}
	if !strings.Contains(out.String(), "已回收 topic 7, 关联卡 4 张一并回收") {
		t.Fatalf("直写结果不符: %q", out.String())
	}

	// 不带 --include-cards 时缺省 false, 文案应说明是解除关联而非回收卡.
	out.Reset()
	if err := RunTopic([]string{"trash", "7", "--expected-version", "2"}, &out); err != nil {
		t.Fatalf("trash: %v", err)
	}
	if ts.requests[1].Body["include_cards"] != false {
		t.Fatalf("缺省 include_cards 应为 false: %v", ts.requests[1].Body)
	}
	if !strings.Contains(out.String(), "已解除关联") {
		t.Fatalf("缺省文案应说明解除关联: %q", out.String())
	}
}

func TestTopicUsageErrors(t *testing.T) {
	setupTopicConfig(t, newTopicTestServer(t, func(w http.ResponseWriter, r *http.Request) {}))
	var out strings.Builder
	cases := [][]string{
		{},
		{"bogus"},
		{"get"},
		{"get", "abc"},
		{"create"},
		{"create", "--name", ""},
		{"update", "7"},
		{"update", "7", "--name", "x"},
		{"trash", "7"},
		{"list", "--page", "0"},
		{"list", "--page-size", "101"},
	}
	for _, args := range cases {
		if err := RunTopic(args, &out); err == nil {
			t.Fatalf("参数 %v 应报用法错误", args)
		}
	}
}
