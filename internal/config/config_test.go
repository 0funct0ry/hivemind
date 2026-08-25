package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

func TestLoadPrecedence(t *testing.T) {
	dir := t.TempDir()
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })
	if err := os.WriteFile(filepath.Join(dir, "hivemind.yaml"), []byte("addr: :2222\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HIVEMIND_ADDR", ":3333")
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("addr", ":1111", "")
	loaded, err := Load(flags)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Config.Addr != ":3333" || loaded.Sources["addr"] != SourceEnv {
		t.Fatalf("environment should win: %#v, %#v", loaded.Config.Addr, loaded.Sources["addr"])
	}
	if err := flags.Set("addr", ":4444"); err != nil {
		t.Fatal(err)
	}
	loaded, err = Load(flags)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Config.Addr != ":4444" || loaded.Sources["addr"] != SourceFlag {
		t.Fatalf("flag should win: %#v, %#v", loaded.Config.Addr, loaded.Sources["addr"])
	}
}

func TestConfigSearchOrderAndMissing(t *testing.T) {
	dir := t.TempDir()
	original, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })
	xdg := filepath.Join(dir, "xdg")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if err := os.MkdirAll(filepath.Join(xdg, "hivemind"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xdg, "hivemind", "config.yaml"), []byte("addr: :5555\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(nil)
	if err != nil || loaded.Config.Addr != ":5555" || loaded.Sources["addr"] != SourceFile {
		t.Fatalf("xdg config was not selected: %#v, %v", loaded, err)
	}
	missing := filepath.Join(dir, "does-not-exist.yaml")
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("config", missing, "")
	loaded, err = Load(flags)
	if err != nil || loaded.Config.Addr != ":5555" {
		t.Fatalf("missing explicit config should fall back: %#v, %v", loaded, err)
	}
}

func TestMalformedFileAndNestedEnvironment(t *testing.T) {
	dir := t.TempDir()
	original, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })
	if err := os.WriteFile(filepath.Join(dir, "hivemind.yaml"), []byte("addr: [broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(nil); err == nil {
		t.Fatal("expected malformed YAML error")
	}
	if err := os.Remove(filepath.Join(dir, "hivemind.yaml")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HIVEMIND_TLS_CERT", "env-cert.pem")
	loaded, err := Load(nil)
	if err != nil || loaded.Config.TLS.Cert != "env-cert.pem" || loaded.Sources["tls.cert"] != SourceEnv {
		t.Fatalf("nested environment value was not loaded: %#v, %v", loaded, err)
	}
}

func TestParseBytes(t *testing.T) {
	tests := []struct {
		input string
		want  int64
		ok    bool
	}{
		{"25MB", 25 << 20, true}, {"2gb", 2 << 30, true}, {"10B", 10, true},
		{"-1MB", 0, false}, {"999999999999999999999TB", 0, false}, {"watts", 0, false},
	}
	for _, tt := range tests {
		got, err := ParseBytes(tt.input)
		if (err == nil) != tt.ok || (tt.ok && got != tt.want) {
			t.Errorf("ParseBytes(%q) = %d, %v; want %d, success=%v", tt.input, got, err, tt.want, tt.ok)
		}
	}
}

func TestValidateJoinsProblems(t *testing.T) {
	dir := t.TempDir()
	dataFile := filepath.Join(dir, "data")
	if err := os.WriteFile(dataFile, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := (Config{Addr: "bad", DataDir: dataFile, Signup: "nonsense", TLS: TLSConfig{Cert: "cert"}, BaseURL: "not a URL"}).Validate()
	if err == nil {
		t.Fatal("expected validation errors")
	}
	message := err.Error()
	for _, want := range []string{"addr", "data_dir", "signup", "tls.cert", "base_url"} {
		if !strings.Contains(message, want) {
			t.Errorf("joined validation error missing %q: %s", want, message)
		}
	}
}

func TestYAMLAndRedaction(t *testing.T) {
	loaded := &Loaded{Config: Config{Addr: ":8080", DataDir: "./data", WorkspaceName: "Hivemind", Signup: "invite", MaxUploadSize: 25 << 20, SessionTTL: 720 * 60 * 60 * 1e9, TLS: TLSConfig{Cert: "cert.pem", Key: "private.key"}}, Sources: map[string]Source{"addr": SourceEnv, "tls.cert": SourceFile, "tls.key": SourceFlag}}
	yaml := loaded.YAML()
	if !strings.Contains(yaml, `addr: ":8080" # source: env`) || !strings.Contains(yaml, `key: "private.key" # source: flag`) {
		t.Fatalf("source annotations missing:\n%s", yaml)
	}
	if strings.Contains(loaded.Config.Redacted(), "private.key") || !strings.Contains(loaded.Config.Redacted(), "[REDACTED]") {
		t.Fatalf("TLS key was not redacted: %s", loaded.Config.Redacted())
	}
}
