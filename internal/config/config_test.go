package config

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTestConfig 在临时目录写入一份配置并返回路径.
func writeTestConfig(t *testing.T, path string, cfg Config) {
	t.Helper()
	if err := SaveTo(path, cfg); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	want := Config{ServerURL: "http://127.0.0.1:33333", APIKey: "ptk_0123456789abcdef"}
	writeTestConfig(t, path, want)

	got, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if got != want {
		t.Fatalf("round trip 后配置不一致: got %+v, want %+v", got, want)
	}
}

func TestLoadFromNotExist(t *testing.T) {
	cfg, err := LoadFrom(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("文件不存在应返回零值配置而非错误: %v", err)
	}
	if cfg != (Config{}) {
		t.Fatalf("文件不存在时应返回零值配置, got %+v", cfg)
	}
}

func TestLoadFromInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("写入测试文件: %v", err)
	}
	if _, err := LoadFrom(path); err == nil {
		t.Fatal("非法 JSON 应返回错误")
	}
}

// 权限约束依赖 POSIX 权限位, 仅在类 Unix 系统上断言.
// 配置文件放在临时目录的子目录里, 因为 SaveTo 只对自己新建的目录
// 保证 0700, 而 t.TempDir 本身的权限在不同环境下并不固定.
func TestSaveToFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pi-teacher", "config.json")
	writeTestConfig(t, path, Config{ServerURL: "http://x", APIKey: "ptk_y"})

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("配置文件权限应为 0600, got %o", perm)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Fatalf("配置目录权限应为 0700, got %o", perm)
	}
}

func TestResolveEnvOverridesFile(t *testing.T) {
	t.Setenv(EnvServerURL, "http://env.example:33333")
	t.Setenv(EnvAPIKey, "ptk_envkey")

	r, err := Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.ServerURL != "http://env.example:33333" || r.ServerURLSource != SourceEnv {
		t.Fatalf("环境变量应覆盖 server_url, got %+v", r)
	}
	if r.APIKey != "ptk_envkey" || r.APIKeySource != SourceEnv {
		t.Fatalf("环境变量应覆盖 api_key, got %+v", r)
	}
}

func TestResolveDefaultWhenNothingSet(t *testing.T) {
	// 清空可能存在的真实配置干扰: 指向一个不存在的路径.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	r, err := Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.ServerURL != "" || r.APIKey != "" {
		t.Fatalf("无配置时应为零值, got %+v", r)
	}
	if r.ServerURLSource != SourceDefault || r.APIKeySource != SourceDefault {
		t.Fatalf("无配置时来源应为 default, got %+v", r)
	}
}

func TestMaskKey(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"空值", "", ""},
		{"过短整体打码", "ptk_123", "*******"},
		{"正常长度保留首末", "ptk_0123456789abcdef", "ptk_****cdef"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := MaskKey(c.in); got != c.want {
				t.Fatalf("MaskKey(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
