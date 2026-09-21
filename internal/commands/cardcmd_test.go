package commands

import (
	"net/http"
	"strings"
	"testing"
)

// cardTestServer 复用 topiccmd_test 的 newTopicTestServer 机制,
// 按 path 分发响应.
func newCardTestServer(t *testing.T, respond func(w http.ResponseWriter, path string)) *topicTestServer {
	return newTopicTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		respond(w, r.URL.Path)
	})
}

func setupCardConfig(t *testing.T, ts *topicTestServer) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PI_TEACHER_SERVER_URL", ts.URL)
	t.Setenv("PI_TEACHER_API_KEY", "ptk_testkey")
}

const cardDetailJSON = `{"id":5,"topic_id":3,"front":"什么是 goroutine?","back":"轻量级线程",` +
	`"enable_embedding":true,"embedding_status":"ready","version":2,` +
	`"created_at":"2026-09-20T10:00:00Z","updated_at":"2026-09-21T11:00:00Z",` +
	`"embedding_error":null,` +
	`"schedule":{"due":"2026-09-22T10:00:00Z","state":"review","stability":1.5,` +
	`"difficulty":5.5,"scheduled_days":1,"reps":3,"lapses":0,` +
	`"last_review_at":"2026-09-21T10:00:00Z","version":2}}`

func TestCardListFilters(t *testing.T) {
	ts := newCardTestServer(t, func(w http.ResponseWriter, path string) {
		w.Write([]byte(`{"items":[` + cardDetailJSON + `],"total":1,"page":1,"page_size":20}`))
	})
	setupCardConfig(t, ts)

	var out strings.Builder
	err := RunCard([]string{"list", "--topic-id", "0", "--embedding-status", "ready",
		"--sort", "updated_at", "--order", "asc", "--q", "go routine"}, &out)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	query := ts.requests[0].Query
	for _, want := range []string{"topic_id", "embedding_status", "sort", "order"} {
		if query.Get(want) == "" {
			t.Fatalf("查询串应包含 %s: %v", want, query)
		}
	}
	if query.Get("topic_id") != "0" || query.Get("embedding_status") != "ready" ||
		query.Get("sort") != "updated_at" || query.Get("order") != "asc" {
		t.Fatalf("筛选参数取值不符: %v", query)
	}
	if query.Get("q") != "go routine" {
		t.Fatalf("q 应被编码后可解回: %v", query)
	}
	for _, want := range []string{"5", "什么是 goroutine?", "ready", "共 1 条"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("输出应包含 %q: %q", want, out.String())
		}
	}

	// 未显式设置 --topic-id 时不应筛选.
	out.Reset()
	if err := RunCard([]string{"list"}, &out); err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := ts.requests[1].Query.Get("topic_id"); got != "" {
		t.Fatalf("未设置 --topic-id 不应出现在查询串: %q", got)
	}
}

func TestCardListInvalidFilters(t *testing.T) {
	setupCardConfig(t, newCardTestServer(t, func(w http.ResponseWriter, path string) {}))
	var out strings.Builder
	for _, args := range [][]string{
		{"list", "--embedding-status", "bogus"},
		{"list", "--sort", "name"},
		{"list", "--order", "up"},
		{"list", "--page", "0"},
	} {
		if err := RunCard(args, &out); err == nil {
			t.Fatalf("参数 %v 应报用法错误", args)
		}
	}
}

func TestCardGetWithSchedule(t *testing.T) {
	ts := newCardTestServer(t, func(w http.ResponseWriter, path string) {
		if path == "/api/cli/cards/5" {
			w.Write([]byte(cardDetailJSON))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":{"code":"not_found","message":"Card 不存在"}}`))
	})
	setupCardConfig(t, ts)

	var out strings.Builder
	if err := RunCard([]string{"get", "5"}, &out); err != nil {
		t.Fatalf("get: %v", err)
	}
	for _, want := range []string{"front:            什么是 goroutine?", "schedule:", "state:          review", "reps:           3"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("输出应包含 %q: %q", want, out.String())
		}
	}
}

func TestCardCreateDirectAndProposal(t *testing.T) {
	ts := newCardTestServer(t, func(w http.ResponseWriter, path string) {
		if path == "/api/cli/cards/check" {
			w.Write([]byte(`{"match_type":"exact","matches":[]}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(cardDetailJSON))
	})
	setupCardConfig(t, ts)

	var out strings.Builder
	if err := RunCard([]string{"create", "--front", "什么是 goroutine?", "--back", "轻量级线程", "--topic-id", "3"}, &out); err != nil {
		t.Fatalf("create: %v", err)
	}
	req := ts.requests[0]
	if req.Method != http.MethodPost || req.Path != "/api/cli/cards" {
		t.Fatalf("请求不符: %s %s", req.Method, req.Path)
	}
	if req.Body["front"] != "什么是 goroutine?" || req.Body["topic_id"].(float64) != 3 {
		t.Fatalf("请求体不符: %v", req.Body)
	}
	if _, ok := req.Body["enable_embedding"]; ok {
		t.Fatalf("未设置 --no-embedding 时不应传 enable_embedding (服务端缺省 true): %v", req.Body)
	}
	if !strings.Contains(out.String(), "schedule:") {
		t.Fatalf("直写应输出详情含 schedule: %q", out.String())
	}

	// --no-embedding 显式传 false.
	out.Reset()
	if err := RunCard([]string{"create", "--front", "f", "--back", "b", "--no-embedding"}, &out); err != nil {
		t.Fatalf("create: %v", err)
	}
	if v := ts.requests[1].Body["enable_embedding"]; v != false {
		t.Fatalf("--no-embedding 应传 false: %v", ts.requests[1].Body)
	}
}

func TestCardUpdateThreeStateTopicID(t *testing.T) {
	ts := newCardTestServer(t, func(w http.ResponseWriter, path string) {
		w.Write([]byte(cardDetailJSON))
	})
	setupCardConfig(t, ts)

	var out strings.Builder
	// 未设置 --topic-id: 不进请求体.
	if err := RunCard([]string{"update", "5", "--expected-version", "2", "--front", "新问题"}, &out); err != nil {
		t.Fatalf("update: %v", err)
	}
	body := ts.requests[0].Body
	if _, ok := body["topic_id"]; ok {
		t.Fatalf("未设置的 topic_id 不应进请求体: %v", body)
	}
	if body["front"] != "新问题" || body["expected_version"].(float64) != 2 {
		t.Fatalf("请求体不符: %v", body)
	}

	// --topic-id 0: 设为无 Topic, 应传 null.
	if err := RunCard([]string{"update", "5", "--expected-version", "2", "--topic-id", "0"}, &out); err != nil {
		t.Fatalf("update: %v", err)
	}
	if v, ok := ts.requests[1].Body["topic_id"]; !ok || v != nil {
		t.Fatalf("--topic-id 0 应传 null: %v", ts.requests[1].Body)
	}

	// --topic-id 3: 指定 Topic.
	if err := RunCard([]string{"update", "5", "--expected-version", "2", "--topic-id", "3"}, &out); err != nil {
		t.Fatalf("update: %v", err)
	}
	if v := ts.requests[2].Body["topic_id"]; v.(float64) != 3 {
		t.Fatalf("--topic-id 3 应传 3: %v", ts.requests[2].Body)
	}
}

func TestCardUpdateEmbeddingFlags(t *testing.T) {
	ts := newCardTestServer(t, func(w http.ResponseWriter, path string) {
		w.Write([]byte(cardDetailJSON))
	})
	setupCardConfig(t, ts)

	var out strings.Builder
	if err := RunCard([]string{"update", "5", "--expected-version", "2", "--no-embedding"}, &out); err != nil {
		t.Fatalf("update: %v", err)
	}
	if v := ts.requests[0].Body["enable_embedding"]; v != false {
		t.Fatalf("--no-embedding 应传 false: %v", ts.requests[0].Body)
	}

	// 两个 embedding flag 同时设置应报错.
	if err := RunCard([]string{"update", "5", "--expected-version", "2", "--no-embedding", "--enable-embedding"}, &out); err == nil {
		t.Fatal("两个 embedding flag 同设应报错")
	}
}

func TestCardTrash(t *testing.T) {
	ts := newCardTestServer(t, func(w http.ResponseWriter, path string) {
		if strings.HasSuffix(path, "/trash") {
			w.Write([]byte(`{"trashed_card_id":5}`))
			return
		}
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"approval":{"id":20,"status":"pending"}}`))
	})
	setupCardConfig(t, ts)

	var out strings.Builder
	if err := RunCard([]string{"trash", "5", "--expected-version", "2"}, &out); err != nil {
		t.Fatalf("trash: %v", err)
	}
	req := ts.requests[0]
	if req.Method != http.MethodPost || req.Path != "/api/cli/cards/5/trash" {
		t.Fatalf("请求不符: %s %s", req.Method, req.Path)
	}
	if !strings.Contains(out.String(), "已回收 card 5") {
		t.Fatalf("直写结果不符: %q", out.String())
	}
}

func TestCardCheckExactAndSemantic(t *testing.T) {
	checkCalls := 0
	ts := newCardTestServer(t, func(w http.ResponseWriter, path string) {
		if path == "/api/cli/cards/check" {
			checkCalls++
			if checkCalls == 1 {
				w.Write([]byte(`{"match_type":"semantic","coverage":{"total_enabled":10,"ready":8,` +
					`"pending":1,"processing":1,"failed":0,"ready_percent":80.0},` +
					`"matches":[{"id":7,"front":"goroutine 是什么?","back":"轻量级线程","topic_id":3,"similarity":0.92}]}`))
				return
			}
			w.Write([]byte(`{"match_type":"exact","matches":[]}`))
			return
		}
	})
	setupCardConfig(t, ts)

	var out strings.Builder
	if err := RunCard([]string{"check", "--front", "goroutine 是什么?"}, &out); err != nil {
		t.Fatalf("check: %v", err)
	}
	req := ts.requests[0]
	if req.Method != http.MethodPost || req.Path != "/api/cli/cards/check" {
		t.Fatalf("请求不符: %s %s", req.Method, req.Path)
	}
	if req.Body["top_k"].(float64) != 5 {
		t.Fatalf("top_k 缺省应为 5: %v", req.Body)
	}
	for _, want := range []string{"match_type: semantic", "coverage: ready 8/10 (80.0%)", "0.92", "goroutine 是什么?"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("输出应包含 %q: %q", want, out.String())
		}
	}

	// --no-embedding 传 enable_embedding=false, 只做精确查重.
	out.Reset()
	if err := RunCard([]string{"check", "--front", "x", "--no-embedding", "--top-k", "10"}, &out); err != nil {
		t.Fatalf("check: %v", err)
	}
	body := ts.requests[1].Body
	if body["enable_embedding"] != false || body["top_k"].(float64) != 10 {
		t.Fatalf("请求体不符: %v", body)
	}
	if !strings.Contains(out.String(), "matches: (无)") {
		t.Fatalf("无命中应显示 (无): %q", out.String())
	}
}

func TestCardCheckErrorHints(t *testing.T) {
	ts := newCardTestServer(t, func(w http.ResponseWriter, path string) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":{"code":"similarity_disabled","message":"相似度未开放"}}`))
	})
	setupCardConfig(t, ts)

	var out strings.Builder
	err := RunCard([]string{"check", "--front", "x"}, &out)
	if err == nil {
		t.Fatal("409 应返回错误")
	}
	if !strings.Contains(err.Error(), "--no-embedding") {
		t.Fatalf("similarity_disabled 应附行动指引: %v", err)
	}
	if ExitCode(err) != exitSimilarityDisabled {
		t.Fatalf("退出码应为 %d, got %d", exitSimilarityDisabled, ExitCode(err))
	}
}

func TestCardUsageErrors(t *testing.T) {
	setupCardConfig(t, newCardTestServer(t, func(w http.ResponseWriter, path string) {}))
	var out strings.Builder
	for _, args := range [][]string{
		{},
		{"bogus"},
		{"get"},
		{"get", "x"},
		{"create"},
		{"create", "--front", "f"},
		{"create", "--front", "f", "--back", "b", "--topic-id", "0"},
		{"update", "5"},
		{"update", "5", "--expected-version", "1"},
		{"trash", "5"},
		{"check"},
		{"check", "--front", "x", "--top-k", "51"},
	} {
		if err := RunCard(args, &out); err == nil {
			t.Fatalf("参数 %v 应报用法错误", args)
		}
	}
}
