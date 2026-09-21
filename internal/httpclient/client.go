// Package httpclient 封装对服务端 /api/cli/... REST API 的访问.
// 统一处理 Bearer 认证, Idempotency-Key 与错误信封解析,
// 让命令层只面对结构化的 APIError 与普通网络错误两类结果.
package httpclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// maxBodyBytes 限制单次响应体读取量, 防御异常服务端拖垮 CLI 进程.
const maxBodyBytes = 10 << 20

// APIError 表示服务端返回的非 2xx 响应, 已按统一错误信封解析.
// Code 对应服务端 error.code, 是稳定分支依据.
type APIError struct {
	Status    int
	Code      string
	Message   string
	Details   map[string]any
	RequestID string // 服务端回写的 X-Request-Id, 便于日志对账
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("服务端返回 %d (%s): %s", e.Status, e.Code, e.Message)
	if e.RequestID != "" {
		msg += " (request_id: " + e.RequestID + ")"
	}
	return msg
}

// Client 是面向 /api/cli/... 的 REST 客户端, 零值不可用, 需经 New 创建.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New 创建客户端. timeout 为整体请求超时, 0 表示不设超时 (不建议).
func New(serverURL, apiKey string, timeoutSeconds int) *Client {
	return &Client{
		baseURL: strings.TrimRight(serverURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second},
	}
}

// Request 描述一次 API 调用. Path 是以 /api/cli/ 开头的完整路径.
type Request struct {
	Method string
	Path   string
	Body   any // nil 表示无请求体, 非 nil 时按 JSON 编码

	// IdempotencyKey 为空且方法非 GET 时自动生成;
	// 网络超时等不确定结果重试时, 调用方必须传入同一 Key 复用,
	// 避免服务端把同一次业务写入当成两次请求.
	IdempotencyKey string
}

// Do 执行请求并把 2xx 响应体解码到 out (out 为 nil 时丢弃).
// 非 2xx 响应返回 *APIError, 网络层故障返回普通 error.
func (c *Client) Do(ctx context.Context, req Request, out any) error {
	raw, err := c.DoRaw(ctx, req)
	if err != nil {
		return err
	}
	if raw.Status < 200 || raw.Status >= 300 {
		return parseAPIError(raw)
	}
	if out == nil || len(raw.Body) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw.Body, out); err != nil {
		return fmt.Errorf("响应体不是合法 JSON: %w", err)
	}
	return nil
}

// RawResponse 是 DoRaw 的结果, 保留状态码/头部/原始体,
// 供写命令按 202 提案与 2xx 直写两种形态分支处理.
type RawResponse struct {
	Status int
	Header http.Header
	Body   []byte
}

// DoRaw 执行请求并返回原始响应, 不做状态码分支与解码.
// 读命令用 Do 即可; 写命令需要区分 202 提案与直写结果, 用 DoRaw 自行分支.
func (c *Client) DoRaw(ctx context.Context, req Request) (*RawResponse, error) {
	var reader io.Reader
	if req.Body != nil {
		data, err := json.Marshal(req.Body)
		if err != nil {
			return nil, fmt.Errorf("编码请求体失败: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, c.baseURL+req.Path, reader)
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}
	if req.Body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	if key := req.IdempotencyKey; key != "" {
		httpReq.Header.Set("Idempotency-Key", key)
	} else if req.Method != http.MethodGet && req.Method != http.MethodHead {
		httpReq.Header.Set("Idempotency-Key", NewUUID())
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("请求服务端失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("读取响应体失败: %w", err)
	}
	return &RawResponse{Status: resp.StatusCode, Header: resp.Header, Body: body}, nil
}

// DoWrite 执行写请求并返回 2xx 原始响应.
// 非 2xx 统一转为 *APIError; 命令层按 Status==202 区分提案与直写两种形态,
// 不必再关心错误分支.
func (c *Client) DoWrite(ctx context.Context, req Request) (*RawResponse, error) {
	raw, err := c.DoRaw(ctx, req)
	if err != nil {
		return nil, err
	}
	if raw.Status < 200 || raw.Status >= 300 {
		return nil, parseAPIError(raw)
	}
	return raw, nil
}

// errorEnvelope 对应服务端统一非 2xx 响应体.
type errorEnvelope struct {
	Error struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	} `json:"error"`
}

// parseAPIError 把非 2xx 响应体解析为 APIError.
// 服务端承诺所有非 2xx 都走统一信封, 解析失败时保留状态码与原始片段,
// 不静默吞掉异常响应.
func parseAPIError(raw *RawResponse) *APIError {
	apiErr := &APIError{
		Status:    raw.Status,
		RequestID: raw.Header.Get("X-Request-Id"),
	}
	var env errorEnvelope
	if err := json.Unmarshal(raw.Body, &env); err != nil || env.Error.Code == "" {
		apiErr.Code = "invalid_response"
		apiErr.Message = fmt.Sprintf("服务端返回了无法解析的错误响应: %s", truncate(raw.Body, 200))
		return apiErr
	}
	apiErr.Code = env.Error.Code
	apiErr.Message = env.Error.Message
	apiErr.Details = env.Error.Details
	return apiErr
}

// truncate 截断字节片段用于错误展示, 避免刷屏.
func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

// NewUUID 生成 RFC 4122 版本 4 的 UUID 字符串, 用作 Idempotency-Key.
// 服务端要求 CLI 的非 GET 请求携带该头, 用 crypto/rand 保证不可预测.
func NewUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand 失败意味着系统熵源故障, 直接 panic 让进程立即暴露.
		panic("crypto/rand 不可用: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10xx
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
