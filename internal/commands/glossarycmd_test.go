package commands

import (
	"net/http"
	"strings"
	"testing"
)

const glossaryJSON = `{"id":3,"term":"FSRS","definition":"自由间隔重复调度算法",` +
	`"version":2,"created_at":"2026-09-20T10:00:00Z","updated_at":"2026-09-21T11:00:00Z"}`

func TestGlossaryList(t *testing.T) {
	ts := newCardTestServer(t, func(w http.ResponseWriter, path string) {
		w.Write([]byte(`{"items":[` + glossaryJSON + `],"total":1,"page":1,"page_size":20}`))
	})
	setupCardConfig(t, ts)

	var out strings.Builder
	if err := RunGlossary([]string{"list", "--q", "间隔重复"}, &out); err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := ts.requests[0].Query.Get("q"); got != "间隔重复" {
		t.Fatalf("q 参数不符: %q", got)
	}
	for _, want := range []string{"3", "FSRS", "共 1 条"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("输出应包含 %q: %q", want, out.String())
		}
	}

	out.Reset()
	if err := RunGlossary([]string{"list", "--json"}, &out); err != nil {
		t.Fatalf("list --json: %v", err)
	}
	if !strings.Contains(out.String(), `"term": "FSRS"`) {
		t.Fatalf("--json 输出不符: %q", out.String())
	}
}

func TestGlossaryGet(t *testing.T) {
	ts := newCardTestServer(t, func(w http.ResponseWriter, path string) {
		if path == "/api/cli/glossary/3" {
			w.Write([]byte(glossaryJSON))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":{"code":"not_found","message":"Glossary 不存在"}}`))
	})
	setupCardConfig(t, ts)

	var out strings.Builder
	if err := RunGlossary([]string{"get", "3"}, &out); err != nil {
		t.Fatalf("get: %v", err)
	}
	for _, want := range []string{"term:       FSRS", "definition: 自由间隔重复调度算法", "version:    2"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("输出应包含 %q: %q", want, out.String())
		}
	}
	if err := RunGlossary([]string{"get", "999"}, &out); err == nil {
		t.Fatal("404 应返回错误")
	}
}

func TestGlossaryCreateDirectAndProposal(t *testing.T) {
	ts := newCardTestServer(t, func(w http.ResponseWriter, path string) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(glossaryJSON))
	})
	setupCardConfig(t, ts)

	var out strings.Builder
	if err := RunGlossary([]string{"create", "--term", "FSRS", "--definition", "调度算法"}, &out); err != nil {
		t.Fatalf("create: %v", err)
	}
	req := ts.requests[0]
	if req.Method != http.MethodPost || req.Path != "/api/cli/glossary" {
		t.Fatalf("请求不符: %s %s", req.Method, req.Path)
	}
	if req.Body["term"] != "FSRS" || req.Body["definition"] != "调度算法" {
		t.Fatalf("请求体不符: %v", req.Body)
	}
	if !strings.Contains(out.String(), "term:       FSRS") {
		t.Fatalf("直写结果不符: %q", out.String())
	}
}

func TestGlossaryUpdateThreeStateFields(t *testing.T) {
	ts := newCardTestServer(t, func(w http.ResponseWriter, path string) {
		w.Write([]byte(glossaryJSON))
	})
	setupCardConfig(t, ts)

	var out strings.Builder
	// 只设置 --term: definition 不进请求体.
	if err := RunGlossary([]string{"update", "3", "--expected-version", "2", "--term", "FSRS-2"}, &out); err != nil {
		t.Fatalf("update: %v", err)
	}
	body := ts.requests[0].Body
	if body["term"] != "FSRS-2" {
		t.Fatalf("term 不符: %v", body)
	}
	if _, ok := body["definition"]; ok {
		t.Fatalf("未设置的 definition 不应进请求体: %v", body)
	}

	// 显式空串 definition 表示清空, 应进入请求体.
	if err := RunGlossary([]string{"update", "3", "--expected-version", "2", "--definition", ""}, &out); err != nil {
		t.Fatalf("update 清空释义: %v", err)
	}
	if v, ok := ts.requests[1].Body["definition"]; !ok || v != "" {
		t.Fatalf("显式空串应进请求体: %v", ts.requests[1].Body)
	}
}

func TestGlossaryUpdateVersionConflict(t *testing.T) {
	ts := newCardTestServer(t, func(w http.ResponseWriter, path string) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":{"code":"version_conflict","message":"版本不一致"}}`))
	})
	setupCardConfig(t, ts)

	var out strings.Builder
	err := RunGlossary([]string{"update", "3", "--expected-version", "1", "--term", "x"}, &out)
	if err == nil {
		t.Fatal("409 应返回错误")
	}
	if !strings.Contains(err.Error(), "重新读取最新 version") {
		t.Fatalf("冲突错误应附重试指引: %v", err)
	}
}

func TestGlossaryTrash(t *testing.T) {
	trashCalls := 0
	ts := newCardTestServer(t, func(w http.ResponseWriter, path string) {
		if strings.HasSuffix(path, "/trash") {
			trashCalls++
			if trashCalls == 1 {
				w.Write([]byte(`{"trashed_glossary_id":3}`))
				return
			}
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"approval":{"id":40,"status":"pending"}}`))
			return
		}
	})
	setupCardConfig(t, ts)

	var out strings.Builder
	if err := RunGlossary([]string{"trash", "3", "--expected-version", "2"}, &out); err != nil {
		t.Fatalf("trash: %v", err)
	}
	req := ts.requests[0]
	if req.Method != http.MethodPost || req.Path != "/api/cli/glossary/3/trash" {
		t.Fatalf("请求不符: %s %s", req.Method, req.Path)
	}
	if !strings.Contains(out.String(), "已回收 glossary 3") {
		t.Fatalf("直写结果不符: %q", out.String())
	}

	out.Reset()
	if err := RunGlossary([]string{"trash", "3", "--expected-version", "2", "--json"}, &out); err != nil {
		t.Fatalf("trash --json: %v", err)
	}
	if !strings.Contains(out.String(), `"outcome": "proposal"`) {
		t.Fatalf("提案信封不符: %q", out.String())
	}
}

func TestGlossaryUsageErrors(t *testing.T) {
	setupCardConfig(t, newCardTestServer(t, func(w http.ResponseWriter, path string) {}))
	var out strings.Builder
	for _, args := range [][]string{
		{},
		{"bogus"},
		{"get"},
		{"get", "x"},
		{"create"},
		{"create", "--term", "t"},
		{"update", "3"},
		{"update", "3", "--expected-version", "1"},
		{"trash", "3"},
		{"list", "--page-size", "0"},
	} {
		if err := RunGlossary(args, &out); err == nil {
			t.Fatalf("参数 %v 应报用法错误", args)
		}
	}
}
