package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// TLSConfig contains the optional certificate and private-key file paths.
type TLSConfig struct {
	Cert string `mapstructure:"cert"`
	Key  string `mapstructure:"key" redact:"true"`
}

// Config is the effective hivemind runtime configuration.
type Config struct {
	Addr          string        `mapstructure:"addr"`
	DataDir       string        `mapstructure:"data_dir"`
	WorkspaceName string        `mapstructure:"workspace_name"`
	BaseURL       string        `mapstructure:"base_url"`
	BehindProxy   bool          `mapstructure:"behind_proxy"`
	Signup        string        `mapstructure:"signup"`
	MaxUploadSize int64         `mapstructure:"max_upload_size"`
	SessionTTL    time.Duration `mapstructure:"session_ttl"`
	LogLevel      string        `mapstructure:"log_level"`
	LogFormat     string        `mapstructure:"log_format"`
	TLS           TLSConfig     `mapstructure:"tls"`
	Dev           bool          `mapstructure:"dev"`
	DevProxy      string        `mapstructure:"dev_proxy"`
	// AllowInsecureWebhooks disables the SSRF guard's https-only/public-host requirement for
	// outgoing webhooks and slash commands, permitting http:// and loopback/private-network
	// targets. Development/testing only — never enable this in production (see
	// internal/store/outgoing_webhook_ssrf.go's AllowInsecureWebhookTargets doc comment).
	AllowInsecureWebhooks bool `mapstructure:"allow_insecure_webhooks"`
}

// Source identifies the layer that supplied a configuration value.
type Source string

const (
	SourceDefault Source = "default"
	SourceFile    Source = "file"
	SourceEnv     Source = "env"
	SourceFlag    Source = "flag"
)

// Loaded is a configuration together with source information for diagnostics.
type Loaded struct {
	Config     Config
	Sources    map[string]Source
	ConfigFile string
}

var defaults = map[string]any{
	"addr":                    ":8080",
	"data_dir":                "./data",
	"workspace_name":          "Hivemind",
	"base_url":                "",
	"behind_proxy":            false,
	"signup":                  "invite",
	"max_upload_size":         "25MB",
	"session_ttl":             "720h",
	"log_level":               "info",
	"log_format":              "",
	"tls.cert":                "",
	"tls.key":                 "",
	"dev":                     false,
	"dev_proxy":               "http://localhost:5173",
	"allow_insecure_webhooks": false,
}

var configKeys = []string{"addr", "data_dir", "workspace_name", "base_url", "behind_proxy", "signup", "max_upload_size", "session_ttl", "log_level", "log_format", "tls.cert", "tls.key", "dev", "dev_proxy", "allow_insecure_webhooks"}

// Load reads configuration from flags, environment, the first available config
// file, and defaults, in that precedence order.
func Load(flags *pflag.FlagSet) (*Loaded, error) {
	v := viper.New()
	v.SetEnvPrefix("HIVEMIND")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	v.AllowEmptyEnv(true)
	for key, value := range defaults {
		v.SetDefault(key, value)
	}
	if flags != nil {
		for _, key := range configKeys {
			f := flags.Lookup(flagNameForKey(key))
			if f == nil {
				continue
			}
			if err := v.BindPFlag(key, f); err != nil {
				return nil, fmt.Errorf("bind config flag %s: %w", f.Name, err)
			}
		}
	}

	configPath := ""
	if flags != nil {
		configPath, _ = flags.GetString("config")
	}
	path, err := findConfigFile(configPath)
	if err != nil {
		return nil, err
	}
	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("read config file %s: %w", path, err)
		}
	}

	c, err := decode(v)
	if err != nil {
		return nil, err
	}
	sources := make(map[string]Source, len(configKeys))
	for _, key := range configKeys {
		sources[key] = valueSource(v, flags, key)
	}
	return &Loaded{Config: c, Sources: sources, ConfigFile: path}, nil
}

func findConfigFile(explicit string) (string, error) {
	candidates := make([]string, 0, 4)
	if explicit != "" {
		candidates = append(candidates, explicit)
	}
	candidates = append(candidates, "./hivemind.yaml")
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		candidates = append(candidates, filepath.Join(xdg, "hivemind", "config.yaml"))
	}
	candidates = append(candidates, "/etc/hivemind/config.yaml")
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return "", fmt.Errorf("inspect config file %s: %w", candidate, err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("config file %s is a directory", candidate)
		}
		return candidate, nil
	}
	return "", nil
}

func decode(v *viper.Viper) (Config, error) {
	max, err := ParseBytes(v.GetString("max_upload_size"))
	if err != nil {
		return Config{}, fmt.Errorf("max_upload_size: %w", err)
	}
	ttl, err := time.ParseDuration(v.GetString("session_ttl"))
	if err != nil {
		return Config{}, fmt.Errorf("session_ttl: %w", err)
	}
	return Config{
		Addr: v.GetString("addr"), DataDir: v.GetString("data_dir"), WorkspaceName: v.GetString("workspace_name"),
		BaseURL: v.GetString("base_url"), BehindProxy: v.GetBool("behind_proxy"), Signup: v.GetString("signup"),
		MaxUploadSize: max, SessionTTL: ttl, LogLevel: v.GetString("log_level"), LogFormat: v.GetString("log_format"),
		TLS: TLSConfig{Cert: v.GetString("tls.cert"), Key: v.GetString("tls.key")}, Dev: v.GetBool("dev"), DevProxy: v.GetString("dev_proxy"),
		AllowInsecureWebhooks: v.GetBool("allow_insecure_webhooks"),
	}, nil
}

// flagNameForKey maps a viper config key (dot-nested, snake_case) to the
// dash-separated flag name it's registered under, e.g. "data_dir" ->
// "data-dir" and "tls.cert" -> "tls-cert".
func flagNameForKey(key string) string {
	return strings.ReplaceAll(strings.ReplaceAll(key, ".", "-"), "_", "-")
}

func valueSource(v *viper.Viper, flags *pflag.FlagSet, key string) Source {
	flagName := flagNameForKey(key)
	if flags != nil {
		if flag := flags.Lookup(flagName); flag != nil && flag.Changed {
			return SourceFlag
		}
	}
	if _, ok := os.LookupEnv("HIVEMIND_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))); ok {
		return SourceEnv
	}
	if v.InConfig(key) {
		return SourceFile
	}
	return SourceDefault
}

// ParseBytes parses a positive byte count such as 25MB using binary units.
func ParseBytes(value string) (int64, error) {
	s := strings.TrimSpace(strings.ToUpper(value))
	if s == "" {
		return 0, fmt.Errorf("value is empty")
	}
	units := []struct {
		suffix     string
		multiplier int64
	}{{"TB", 1 << 40}, {"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10}, {"B", 1}}
	for _, unit := range units {
		if strings.HasSuffix(s, unit.suffix) {
			n := strings.TrimSpace(strings.TrimSuffix(s, unit.suffix))
			if n == "" {
				return 0, fmt.Errorf("missing number")
			}
			base, err := strconv.ParseInt(n, 10, 64)
			if err != nil || base < 0 {
				return 0, fmt.Errorf("invalid byte count %q", value)
			}
			if base > (1<<63-1)/unit.multiplier {
				return 0, fmt.Errorf("byte count overflows int64")
			}
			return base * unit.multiplier, nil
		}
	}
	return 0, fmt.Errorf("invalid byte suffix in %q", value)
}

// Validate checks that the effective configuration can be used to start the server.
func (c Config) Validate() error {
	var problems []error
	_, _, err := splitHostPort(c.Addr)
	if err != nil {
		problems = append(problems, fmt.Errorf("addr: %w", err))
	}
	if c.DataDir == "" {
		problems = append(problems, fmt.Errorf("data_dir: must not be empty"))
	} else if err := validateDataDir(c.DataDir); err != nil {
		problems = append(problems, fmt.Errorf("data_dir: %w", err))
	}
	if c.Signup != "invite" && c.Signup != "closed" && c.Signup != "open" {
		problems = append(problems, fmt.Errorf("signup: must be invite, closed, or open"))
	}
	if (c.TLS.Cert == "") != (c.TLS.Key == "") {
		problems = append(problems, fmt.Errorf("tls.cert and tls.key must both be set or both be empty"))
	}
	if c.BaseURL != "" {
		u, err := url.Parse(c.BaseURL)
		if err != nil || u.Scheme == "" || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			problems = append(problems, fmt.Errorf("base_url: must be an absolute http or https URL"))
		}
	}
	return errors.Join(problems...)
}

func splitHostPort(addr string) (string, string, error) {
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return "", "", fmt.Errorf("must be host:port")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", "", fmt.Errorf("invalid port")
	}
	return host, portText, nil
}

func validateDataDir(path string) error {
	if err := os.MkdirAll(path, 0o750); err != nil {
		return err
	}
	f, err := os.CreateTemp(path, ".hivemind-write-test-*")
	if err != nil {
		return fmt.Errorf("not writable: %w", err)
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Remove(name); err != nil {
		return err
	}
	return nil
}

// Redacted returns a deterministic configuration representation safe for startup logs.
func (c Config) Redacted() string {
	return renderYAML(c, nil, true)
}

// YAML returns stable YAML with a source comment on every configuration line.
func (l *Loaded) YAML() string { return renderYAML(l.Config, l.Sources, false) }

func renderYAML(c Config, sources map[string]Source, redact bool) string {
	source := func(key string) string {
		if sources == nil {
			return ""
		}
		return " # source: " + string(sources[key])
	}
	quote := func(v string) string { return strconv.Quote(v) }
	max := formatBytes(c.MaxUploadSize)
	ttl := formatDuration(c.SessionTTL)
	key := c.TLS.Key
	if redact {
		key = "[REDACTED]"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "addr: %s%s\n", quote(c.Addr), source("addr"))
	fmt.Fprintf(&b, "data_dir: %s%s\n", quote(c.DataDir), source("data_dir"))
	fmt.Fprintf(&b, "workspace_name: %s%s\n", quote(c.WorkspaceName), source("workspace_name"))
	fmt.Fprintf(&b, "base_url: %s%s\n", quote(c.BaseURL), source("base_url"))
	fmt.Fprintf(&b, "behind_proxy: %t%s\n", c.BehindProxy, source("behind_proxy"))
	fmt.Fprintf(&b, "signup: %s%s\n", quote(c.Signup), source("signup"))
	fmt.Fprintf(&b, "max_upload_size: %s%s\n", quote(max), source("max_upload_size"))
	fmt.Fprintf(&b, "session_ttl: %s%s\n", quote(ttl), source("session_ttl"))
	fmt.Fprintf(&b, "log_level: %s%s\n", quote(c.LogLevel), source("log_level"))
	fmt.Fprintf(&b, "log_format: %s%s\n", quote(c.LogFormat), source("log_format"))
	fmt.Fprintf(&b, "tls:%s\n", source("tls.cert"))
	fmt.Fprintf(&b, "  cert: %s%s\n", quote(c.TLS.Cert), source("tls.cert"))
	fmt.Fprintf(&b, "  key: %s%s\n", quote(key), source("tls.key"))
	fmt.Fprintf(&b, "dev: %t%s\n", c.Dev, source("dev"))
	fmt.Fprintf(&b, "dev_proxy: %s%s\n", quote(c.DevProxy), source("dev_proxy"))
	fmt.Fprintf(&b, "allow_insecure_webhooks: %t%s\n", c.AllowInsecureWebhooks, source("allow_insecure_webhooks"))
	return b.String()
}

func formatBytes(n int64) string {
	for _, unit := range []struct {
		suffix string
		size   int64
	}{{"TB", 1 << 40}, {"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10}} {
		if n > 0 && n%unit.size == 0 {
			return fmt.Sprintf("%d%s", n/unit.size, unit.suffix)
		}
	}
	return fmt.Sprintf("%dB", n)
}

func formatDuration(d time.Duration) string {
	if d > 0 && d%time.Hour == 0 {
		return fmt.Sprintf("%dh", d/time.Hour)
	}
	return d.String()
}
