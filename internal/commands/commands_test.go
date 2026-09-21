package commands

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pi-Teacher/cli/internal/config"
)

// setupTestConfig 把配置文件指向临时目录, 避免读写真实用户配置.
func setupTestConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return filepath.Join(dir, "pi-teacher", "config.json")
}

func TestSetServerSavesAndKeepsOtherFields(t *testing.T) {
	setupTestConfig(t)
	var out bytes.Buffer

	if err := RunConfig([]string{"set-server", "http://127.0.0.1:33333"}, &out); err != nil {
		t.Fatalf("set-server: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ServerURL != "http://127.0.0.1:33333" {
		t.Fatalf("server_url 未保存: %+v", cfg)
	}

	// 后续写 api_key 不应清掉 server_url.
	if err := RunConfig([]string{"set-api-key", "ptk_0123456789abcdef"}, &out); err != nil {
		t.Fatalf("set-api-key: %v", err)
	}
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ServerURL != "http://127.0.0.1:33333" || cfg.APIKey != "ptk_0123456789abcdef" {
		t.Fatalf("字段应互相保留: %+v", cfg)
	}
}

func TestSetServerRejectsInvalidURL(t *testing.T) {
	setupTestConfig(t)
	for _, bad := range []string{"", "ftp://x", "127.0.0.1:33333", "http://"} {
		var out bytes.Buffer
		if err := RunConfig([]string{"set-server", bad}, &out); err == nil {
			t.Fatalf("非法 URL %q 应被拒绝", bad)
		}
	}
}

func TestSetAPIKeyEchoIsMasked(t *testing.T) {
	setupTestConfig(t)
	var out bytes.Buffer
	if err := RunConfig([]string{"set-api-key", "ptk_0123456789abcdef"}, &out); err != nil {
		t.Fatalf("set-api-key: %v", err)
	}
	echo := out.String()
	if strings.Contains(echo, "ptk_0123456789abcdef") {
		t.Fatalf("回显不应包含完整 Key: %q", echo)
	}
	if !strings.Contains(echo, "ptk_****cdef") {
		t.Fatalf("回显应包含脱敏 Key: %q", echo)
	}
}

func TestShowMasksAPIKeyInBothModes(t *testing.T) {
	setupTestConfig(t)
	var out bytes.Buffer
	if err := RunConfig([]string{"set-server", "http://127.0.0.1:33333"}, &out); err != nil {
		t.Fatalf("set-server: %v", err)
	}
	if err := RunConfig([]string{"set-api-key", "ptk_0123456789abcdef"}, &out); err != nil {
		t.Fatalf("set-api-key: %v", err)
	}

	out.Reset()
	if err := RunConfig([]string{"show"}, &out); err != nil {
		t.Fatalf("show: %v", err)
	}
	if strings.Contains(out.String(), "ptk_0123456789abcdef") {
		t.Fatalf("人类模式不应输出完整 Key: %q", out.String())
	}

	out.Reset()
	if err := RunConfig([]string{"show", "--json"}, &out); err != nil {
		t.Fatalf("show --json: %v", err)
	}
	if strings.Contains(out.String(), "ptk_0123456789abcdef") {
		t.Fatalf("--json 模式不应输出完整 Key: %q", out.String())
	}
	if !strings.Contains(out.String(), `"api_key_masked": "ptk_****cdef"`) {
		t.Fatalf("--json 模式应输出脱敏 Key: %q", out.String())
	}
}

func TestShowReportsEnvSource(t *testing.T) {
	setupTestConfig(t)
	t.Setenv(config.EnvServerURL, "http://env.example:33333")

	var out bytes.Buffer
	if err := RunConfig([]string{"show"}, &out); err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(out.String(), "环境变量") {
		t.Fatalf("应标注来源为环境变量: %q", out.String())
	}
}

func TestSystemInfoRequiresConfig(t *testing.T) {
	setupTestConfig(t)
	var out bytes.Buffer
	err := RunSystem([]string{"info"}, &out)
	if err == nil {
		t.Fatal("缺少配置时应报错并给出设置指引")
	}
	if !strings.Contains(err.Error(), "config set-server") {
		t.Fatalf("错误信息应包含设置指引: %v", err)
	}
}

func TestSystemInfoAgainstTestServer(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/cli/system/info" {
			t.Errorf("请求路径不符: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer ptk_testkey" {
			t.Errorf("应携带 Bearer 认证头")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"version":"v1","go_version":"go1.26.7","db_driver":"sqlite","uptime_seconds":7}`))
	}))
	defer ts.Close()

	setupTestConfig(t)
	var out bytes.Buffer
	if err := RunConfig([]string{"set-server", ts.URL}, &out); err != nil {
		t.Fatalf("set-server: %v", err)
	}
	if err := RunConfig([]string{"set-api-key", "ptk_testkey"}, &out); err != nil {
		t.Fatalf("set-api-key: %v", err)
	}

	out.Reset()
	if err := RunSystem([]string{"info"}, &out); err != nil {
		t.Fatalf("system info: %v", err)
	}
	for _, want := range []string{"v1", "go1.26.7", "sqlite", "7"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("输出应包含 %s: %q", want, out.String())
		}
	}

	out.Reset()
	if err := RunSystem([]string{"info", "--json"}, &out); err != nil {
		t.Fatalf("system info --json: %v", err)
	}
	if !strings.Contains(out.String(), `"db_driver": "sqlite"`) {
		t.Fatalf("--json 输出不符: %q", out.String())
	}
}

func TestExitCodeMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"usage", NewUsageError("x"), 2},
		{"network", errPlain, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ExitCode(c.err); got != c.want {
				t.Fatalf("ExitCode(%v) = %d, want %d", c.err, got, c.want)
			}
		})
	}
}

// errPlain 模拟普通网络错误.
var errPlain = &plainError{}

type plainError struct{}

func (*plainError) Error() string { return "network down" }
