package commands

import (
	"net/http"
	"strings"
	"testing"
)

func TestCardMergeDirectWrite(t *testing.T) {
	ts := newCardTestServer(t, func(w http.ResponseWriter, path string) {
		if path == "/api/cli/cards/merge" {
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(cardDetailJSON))
			return
		}
		w.Write([]byte(`{}`))
	})
	setupCardConfig(t, ts)

	var out strings.Builder
	if err := RunCard([]string{"merge", "5", "6"}, &out); err != nil {
		t.Fatalf("merge: %v", err)
	}
	req := ts.requests[0]
	if req.Method != http.MethodPost || req.Path != "/api/cli/cards/merge" {
		t.Fatalf("请求不符: %s %s", req.Method, req.Path)
	}
	// source_card_ids 是 []any, 逐项断言.
	ids, _ := req.Body["source_card_ids"].([]any)
	if len(ids) != 2 || ids[0].(float64) != 5 || ids[1].(float64) != 6 {
		t.Fatalf("source_card_ids 不符: %v", req.Body)
	}
	for _, key := range []string{"front", "back", "topic_id", "enable_embedding"} {
		if _, ok := req.Body[key]; ok {
			t.Fatalf("未显式设置的 %s 不应进请求体: %v", key, req.Body)
		}
	}
	if !strings.Contains(out.String(), "schedule:") {
		t.Fatalf("直写应输出新卡详情: %q", out.String())
	}
}

func TestCardMergeExplicitFields(t *testing.T) {
	ts := newCardTestServer(t, func(w http.ResponseWriter, path string) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(cardDetailJSON))
	})
	setupCardConfig(t, ts)

	var out strings.Builder
	err := RunCard([]string{"merge", "5", "6", "--front", "合并正面", "--topic-id", "0", "--no-embedding"}, &out)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	body := ts.requests[0].Body
	if body["front"] != "合并正面" {
		t.Fatalf("front 不符: %v", body)
	}
	if v, ok := body["topic_id"]; !ok || v != nil {
		t.Fatalf("--topic-id 0 应传 null: %v", body)
	}
	if v := body["enable_embedding"]; v != false {
		t.Fatalf("--no-embedding 应传 false: %v", body)
	}
}

func TestCardMergeConflictHints(t *testing.T) {
	codes := []struct {
		code string
		want int
		hint string
	}{
		{"merge_topic_required", exitMergeTopicReqd, "--topic-id"},
		{"merge_embedding_required", exitMergeEmbeddingReq, "--no-embedding"},
	}
	for _, c := range codes {
		ts := newCardTestServer(t, func(w http.ResponseWriter, path string) {
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(`{"error":{"code":"` + c.code + `","message":"必须显式指定"}}`))
		})
		setupCardConfig(t, ts)

		var out strings.Builder
		err := RunCard([]string{"merge", "5", "6"}, &out)
		if err == nil {
			t.Fatalf("%s 应返回错误", c.code)
		}
		if !strings.Contains(err.Error(), c.hint) {
			t.Fatalf("%s 应附行动指引: %v", c.code, err)
		}
		if got := ExitCode(err); got != c.want {
			t.Fatalf("%s 退出码应为 %d, got %d", c.code, c.want, got)
		}
	}
}

func TestCardMergeUsageErrors(t *testing.T) {
	setupCardConfig(t, newCardTestServer(t, func(w http.ResponseWriter, path string) {}))
	var out strings.Builder
	for _, args := range [][]string{
		{"merge"},
		{"merge", "5"},
		{"merge", "5", "5"},
		{"merge", "5", "6", "--enable-embedding", "--no-embedding"},
		{"merge", "x", "6"},
	} {
		if err := RunCard(args, &out); err == nil {
			t.Fatalf("参数 %v 应报用法错误", args)
		}
	}
}

func TestTrashListAllEntities(t *testing.T) {
	ts := newCardTestServer(t, func(w http.ResponseWriter, path string) {
		switch path {
		case "/api/cli/trash/cards":
			w.Write([]byte(`{"items":[{"id":31,"front":"批次3直写联调卡-改","back":"更新后的背面",` +
				`"enable_embedding":false,"version":3,"created_at":"2026-09-21T12:00:00Z",` +
				`"trashed_at":"2026-09-21T12:05:00Z"}],"total":1,"page":1,"page_size":20}`))
		case "/api/cli/trash/topics":
			w.Write([]byte(`{"items":[{"id":10,"name":"批次2直写联调","description":"",` +
				`"card_count":0,"version":4,"trashed_at":"2026-09-21T11:40:00Z",` +
				`"created_at":"2026-09-21T11:36:00Z","updated_at":"2026-09-21T11:40:00Z"}],` +
				`"total":1,"page":1,"page_size":20}`))
		case "/api/cli/trash/glossary":
			w.Write([]byte(`{"items":[{"id":2,"term":"FSRS","definition":"间隔重复算法",` +
				`"version":1,"trashed_at":"2026-09-21T10:00:00Z","created_at":"2026-09-20T10:00:00Z",` +
				`"updated_at":"2026-09-21T10:00:00Z"}],"total":1,"page":1,"page_size":20}`))
		}
	})
	setupCardConfig(t, ts)

	var out strings.Builder
	if err := RunTrash([]string{"cards", "list"}, &out); err != nil {
		t.Fatalf("trash cards list: %v", err)
	}
	if !strings.Contains(out.String(), "31") || !strings.Contains(out.String(), "批次3直写联调卡-改") {
		t.Fatalf("回收站卡列表不符: %q", out.String())
	}

	out.Reset()
	if err := RunTrash([]string{"topics", "list"}, &out); err != nil {
		t.Fatalf("trash topics list: %v", err)
	}
	if !strings.Contains(out.String(), "批次2直写联调") {
		t.Fatalf("回收站 Topic 列表不符: %q", out.String())
	}

	out.Reset()
	if err := RunTrash([]string{"glossary", "list", "--json"}, &out); err != nil {
		t.Fatalf("trash glossary list: %v", err)
	}
	if !strings.Contains(out.String(), `"term": "FSRS"`) {
		t.Fatalf("回收站 Glossary --json 不符: %q", out.String())
	}
}

func TestTrashRestoreCardDirectWrite(t *testing.T) {
	ts := newCardTestServer(t, func(w http.ResponseWriter, path string) {
		if path == "/api/cli/trash/cards/31/restore" {
			w.Write([]byte(`{"trash_card_id":31,"new_card_id":33}`))
			return
		}
		w.Write([]byte(`{}`))
	})
	setupCardConfig(t, ts)

	var out strings.Builder
	if err := RunTrash([]string{"cards", "restore", "31", "--expected-version", "3", "--topic-id", "2"}, &out); err != nil {
		t.Fatalf("restore: %v", err)
	}
	req := ts.requests[0]
	if req.Method != http.MethodPost || req.Path != "/api/cli/trash/cards/31/restore" {
		t.Fatalf("请求不符: %s %s", req.Method, req.Path)
	}
	if req.Body["topic_id"].(float64) != 2 || req.Body["expected_version"].(float64) != 3 {
		t.Fatalf("请求体不符: %v", req.Body)
	}
	if !strings.Contains(out.String(), "已恢复回收站卡 31 为新卡 33") {
		t.Fatalf("恢复结果不符: %q", out.String())
	}

	// --topic-id 0 应传 null (恢复为无 Topic).
	out.Reset()
	if err := RunTrash([]string{"cards", "restore", "31", "--expected-version", "3", "--topic-id", "0"}, &out); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if v, ok := ts.requests[1].Body["topic_id"]; !ok || v != nil {
		t.Fatalf("--topic-id 0 应传 null: %v", ts.requests[1].Body)
	}
}

func TestTrashRestoreTopicDirectWrite(t *testing.T) {
	ts := newCardTestServer(t, func(w http.ResponseWriter, path string) {
		if path == "/api/cli/trash/topics/10/restore" {
			w.Write([]byte(`{"id":10,"name":"批次2直写联调","description":"d","card_count":0,` +
				`"version":5,"created_at":"2026-09-21T11:36:00Z","updated_at":"2026-09-21T12:00:00Z"}`))
			return
		}
		w.Write([]byte(`{}`))
	})
	setupCardConfig(t, ts)

	var out strings.Builder
	if err := RunTrash([]string{"topics", "restore", "10", "--expected-version", "4"}, &out); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !strings.Contains(out.String(), "name:        批次2直写联调") {
		t.Fatalf("恢复结果不符: %q", out.String())
	}
}

func TestTrashRestoreProposal(t *testing.T) {
	ts := newCardTestServer(t, func(w http.ResponseWriter, path string) {
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"approval":{"id":30,"status":"pending"}}`))
	})
	setupCardConfig(t, ts)

	var out strings.Builder
	if err := RunTrash([]string{"glossary", "restore", "2", "--expected-version", "1"}, &out); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !strings.Contains(out.String(), "未生效") || !strings.Contains(out.String(), "approval_id: 30") {
		t.Fatalf("提案输出不符: %q", out.String())
	}
}

func TestTrashUsageErrors(t *testing.T) {
	setupCardConfig(t, newCardTestServer(t, func(w http.ResponseWriter, path string) {}))
	var out strings.Builder
	for _, args := range [][]string{
		{},
		{"bogus", "list"},
		{"cards"},
		{"cards", "bogus"},
		{"cards", "restore"},
		{"cards", "restore", "31"},
		{"topics", "restore", "10", "--expected-version", "1", "--topic-id", "2"},
		{"cards", "list", "--page", "0"},
	} {
		if err := RunTrash(args, &out); err == nil {
			t.Fatalf("参数 %v 应报用法错误", args)
		}
	}
}
