package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

// uuidPattern 校验生成的 Idempotency-Key 是标准 UUID v4 文本格式.
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestDoGetAuthAndDecode(t *testing.T) {
	var gotAuth, gotIdem string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotIdem = r.Header.Get("Idempotency-Key")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"version":"v1","go_version":"go1.26.7","db_driver":"sqlite","uptime_seconds":42}`))
	}))
	defer ts.Close()

	client := New(ts.URL, "ptk_testkey", 5)
	var out struct {
		Version string `json:"version"`
	}
	if err := client.Do(context.Background(), Request{Method: http.MethodGet, Path: "/api/cli/system/info"}, &out); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if out.Version != "v1" {
		t.Fatalf("响应解码失败, got %+v", out)
	}
	if gotAuth != "Bearer ptk_testkey" {
		t.Fatalf("应携带 Bearer 认证头, got %q", gotAuth)
	}
	if gotIdem != "" {
		t.Fatalf("GET 请求不应携带 Idempotency-Key, got %q", gotIdem)
	}
}

func TestDoPostAutoIdempotencyKey(t *testing.T) {
	keys := make([]string, 0, 2)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("写请求应携带 JSON Content-Type")
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{}`))
	}))
	defer ts.Close()

	client := New(ts.URL, "ptk_testkey", 5)
	req := Request{Method: http.MethodPost, Path: "/api/cli/topics", Body: map[string]string{"name": "x"}}
	for i := 0; i < 2; i++ {
		if err := client.Do(context.Background(), req, nil); err != nil {
			t.Fatalf("Do: %v", err)
		}
	}
	if len(keys) != 2 {
		t.Fatalf("应有两次请求, got %d", len(keys))
	}
	if !uuidPattern.MatchString(keys[0]) || !uuidPattern.MatchString(keys[1]) {
		t.Fatalf("自动生成的 Key 应为 UUID v4, got %q / %q", keys[0], keys[1])
	}
	if keys[0] == keys[1] {
		t.Fatalf("未显式传 Key 时每次调用应生成新 Key, got 两次相同 %q", keys[0])
	}
}

func TestDoPostReuseExplicitIdempotencyKey(t *testing.T) {
	keys := make([]string, 0, 2)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{}`))
	}))
	defer ts.Close()

	client := New(ts.URL, "ptk_testkey", 5)
	req := Request{
		Method:         http.MethodPost,
		Path:           "/api/cli/topics",
		Body:           map[string]string{"name": "x"},
		IdempotencyKey: "11111111-2222-4333-8444-555555555555",
	}
	for i := 0; i < 2; i++ {
		if err := client.Do(context.Background(), req, nil); err != nil {
			t.Fatalf("Do: %v", err)
		}
	}
	if keys[0] != req.IdempotencyKey || keys[1] != req.IdempotencyKey {
		t.Fatalf("显式传入的 Key 应被复用, got %q / %q", keys[0], keys[1])
	}
}

func TestDoAPIErrorEnvelope(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "req-abc")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"code":"unauthorized","message":"API Key 无效"}}`))
	}))
	defer ts.Close()

	client := New(ts.URL, "ptk_badkey", 5)
	err := client.Do(context.Background(), Request{Method: http.MethodGet, Path: "/api/cli/system/info"}, nil)
	if err == nil {
		t.Fatal("401 应返回错误")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("应返回 *APIError, got %T", err)
	}
	if apiErr.Status != http.StatusUnauthorized || apiErr.Code != "unauthorized" || apiErr.Message != "API Key 无效" {
		t.Fatalf("错误信封解析不符: %+v", apiErr)
	}
	if apiErr.RequestID != "req-abc" {
		t.Fatalf("应记录 X-Request-Id, got %q", apiErr.RequestID)
	}
}

func TestDoNonJSONErrorBody(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("gateway exploded"))
	}))
	defer ts.Close()

	client := New(ts.URL, "ptk_testkey", 5)
	err := client.Do(context.Background(), Request{Method: http.MethodGet, Path: "/api/cli/system/info"}, nil)
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("应返回 *APIError, got %T", err)
	}
	if apiErr.Status != http.StatusInternalServerError || apiErr.Code != "invalid_response" {
		t.Fatalf("异常响应应保留状态码并标记 invalid_response: %+v", apiErr)
	}
}

func TestDoNetworkError(t *testing.T) {
	// 立即关闭的服务端, 连接必然失败.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	ts.Close()

	client := New(ts.URL, "ptk_testkey", 5)
	err := client.Do(context.Background(), Request{Method: http.MethodGet, Path: "/api/cli/system/info"}, nil)
	if err == nil {
		t.Fatal("网络故障应返回错误")
	}
	if _, ok := err.(*APIError); ok {
		t.Fatalf("网络故障不应是 *APIError, got %T", err)
	}
}
