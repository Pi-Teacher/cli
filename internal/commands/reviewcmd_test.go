package commands

import (
	"net/http"
	"strings"
	"testing"
)

const dueJSON = `{"items":[{"card_id":5,"front":"二分查找的时间复杂度?","back":"O(log n)",` +
	`"topic_id":null,"card_version":3,"due":"2026-09-21T10:00:00Z","state":"review",` +
	`"schedule_version":2,"reps":4,"lapses":1}],"total":1}`

const submitJSON = `{"card_id":5,"rating":"good","review_log_id":77,` +
	`"schedule":{"due":"2026-09-24T10:00:00Z","state":"review","stability":5.5,` +
	`"difficulty":5.0,"scheduled_days":3,"reps":5,"lapses":1,` +
	`"last_review_at":"2026-09-21T10:00:00Z","version":3}}`

func TestReviewDue(t *testing.T) {
	ts := newCardTestServer(t, func(w http.ResponseWriter, path string) {
		w.Write([]byte(dueJSON))
	})
	setupCardConfig(t, ts)

	var out strings.Builder
	if err := RunReview([]string{"due", "--topic-id", "0", "--limit", "10"}, &out); err != nil {
		t.Fatalf("due: %v", err)
	}
	query := ts.requests[0].Query
	if query.Get("topic_id") != "0" || query.Get("limit") != "10" {
		t.Fatalf("查询参数不符: %v", query)
	}
	for _, want := range []string{"5", "二分查找的时间复杂度?", "review", "共 1 张到期"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("输出应包含 %q: %q", want, out.String())
		}
	}

	// 未设置 --topic-id / --limit 时不应出现在查询串.
	out.Reset()
	if err := RunReview([]string{"due"}, &out); err != nil {
		t.Fatalf("due: %v", err)
	}
	if got := ts.requests[1].Query.Encode(); got != "" {
		t.Fatalf("未设置筛选时不应有查询参数: %q", got)
	}
}

func TestReviewSubmit(t *testing.T) {
	ts := newCardTestServer(t, func(w http.ResponseWriter, path string) {
		w.Write([]byte(submitJSON))
	})
	setupCardConfig(t, ts)

	var out strings.Builder
	if err := RunReview([]string{"submit", "5", "--rating", "good",
		"--expected-card-version", "3", "--expected-schedule-version", "2"}, &out); err != nil {
		t.Fatalf("submit: %v", err)
	}
	req := ts.requests[0]
	if req.Method != http.MethodPost || req.Path != "/api/cli/review/5/submit" {
		t.Fatalf("请求不符: %s %s", req.Method, req.Path)
	}
	if req.Body["rating"] != "good" || req.Body["expected_card_version"].(float64) != 3 ||
		req.Body["expected_schedule_version"].(float64) != 2 {
		t.Fatalf("请求体不符: %v", req.Body)
	}
	for _, want := range []string{"rating:        good", "review_log_id: 77", "scheduled_days: 3", "version:        3"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("输出应包含 %q: %q", want, out.String())
		}
	}
}

func TestReviewSubmitConflictHint(t *testing.T) {
	ts := newCardTestServer(t, func(w http.ResponseWriter, path string) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":{"code":"version_conflict","message":"版本不一致"}}`))
	})
	setupCardConfig(t, ts)

	var out strings.Builder
	err := RunReview([]string{"submit", "5", "--rating", "good",
		"--expected-card-version", "3", "--expected-schedule-version", "2"}, &out)
	if err == nil {
		t.Fatal("409 应返回错误")
	}
	if !strings.Contains(err.Error(), "review due") || !strings.Contains(err.Error(), "CV/SV") {
		t.Fatalf("冲突错误应附重试指引: %v", err)
	}
}

func TestReviewUsageErrors(t *testing.T) {
	setupCardConfig(t, newCardTestServer(t, func(w http.ResponseWriter, path string) {}))
	var out strings.Builder
	for _, args := range [][]string{
		{},
		{"bogus"},
		{"due", "--topic-id", "-1"},
		{"due", "--limit", "0"},
		{"submit"},
		{"submit", "5"},
		{"submit", "5", "--rating", "bogus", "--expected-card-version", "1", "--expected-schedule-version", "1"},
		{"submit", "5", "--rating", "good"},
		{"submit", "5", "--rating", "good", "--expected-card-version", "1"},
	} {
		if err := RunReview(args, &out); err == nil {
			t.Fatalf("参数 %v 应报用法错误", args)
		}
	}
}

func TestUserProfileGetAndSet(t *testing.T) {
	putCalls := 0
	ts := newCardTestServer(t, func(w http.ResponseWriter, path string) {
		if path == "/api/cli/user-profile" {
			putCalls++
			w.Write([]byte(`{"profile":"Go 学习者, 偏好简洁解释","version":2}`))
		}
	})
	setupCardConfig(t, ts)

	var out strings.Builder
	if err := RunUserProfile([]string{"get"}, &out); err != nil {
		t.Fatalf("get: %v", err)
	}
	for _, want := range []string{"profile:  Go 学习者", "version: 2"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("输出应包含 %q: %q", want, out.String())
		}
	}

	out.Reset()
	if err := RunUserProfile([]string{"set", "--profile", "新画像", "--expected-version", "2"}, &out); err != nil {
		t.Fatalf("set: %v", err)
	}
	req := ts.requests[1]
	if req.Method != http.MethodPut || req.Path != "/api/cli/user-profile" {
		t.Fatalf("请求不符: %s %s", req.Method, req.Path)
	}
	if req.Body["profile"] != "新画像" || req.Body["expected_version"].(float64) != 2 {
		t.Fatalf("请求体不符: %v", req.Body)
	}
	if !strings.Contains(out.String(), "已保存画像 (version: 2)") {
		t.Fatalf("set 输出不符: %q", out.String())
	}
	_ = putCalls

	// --profile "" 显式空串是合法的清空操作.
	out.Reset()
	if err := RunUserProfile([]string{"set", "--profile", "", "--expected-version", "0"}, &out); err != nil {
		t.Fatalf("set 清空: %v", err)
	}
	if ts.requests[2].Body["profile"] != "" {
		t.Fatalf("空串应原样传给服务端: %v", ts.requests[2].Body)
	}
}

func TestUserProfileSetRequiresExplicitProfile(t *testing.T) {
	setupCardConfig(t, newCardTestServer(t, func(w http.ResponseWriter, path string) {}))
	var out strings.Builder
	// 未设置 --profile 必须报错, 否则会把"清空"误当缺省.
	if err := RunUserProfile([]string{"set", "--expected-version", "0"}, &out); err == nil {
		t.Fatal("缺少 --profile 应报用法错误")
	}
	// 未设置 --expected-version 必须报错.
	if err := RunUserProfile([]string{"set", "--profile", "x"}, &out); err == nil {
		t.Fatal("缺少 --expected-version 应报用法错误")
	}
}

func TestUserProfileSetConflict(t *testing.T) {
	ts := newCardTestServer(t, func(w http.ResponseWriter, path string) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":{"code":"version_conflict","message":"版本不一致",` +
			`"details":{"current_version":5}}}`))
	})
	setupCardConfig(t, ts)

	var out strings.Builder
	err := RunUserProfile([]string{"set", "--profile", "x", "--expected-version", "1"}, &out)
	if err == nil {
		t.Fatal("409 应返回错误")
	}
	if !strings.Contains(err.Error(), "重新读取最新 version") {
		t.Fatalf("冲突错误应附重试指引: %v", err)
	}
	if got := ExitCode(err); got != exitVersionConflict {
		t.Fatalf("退出码应为 %d, got %d", exitVersionConflict, got)
	}
}
