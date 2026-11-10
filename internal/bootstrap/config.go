// Package bootstrap reads process settings. Routing never comes from YAML.
package bootstrap

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/kmpoltorak/routetus/internal/config"
	"go.yaml.in/yaml/v4"
)

type Config struct {
	Version    int           `yaml:"version"`
	Server     Server        `yaml:"server"`
	Admin      Admin         `yaml:"admin"`
	Management Management    `yaml:"management"`
	Logging    Logging       `yaml:"logging"`
	Postgres   Postgres      `yaml:"postgres"`
	Sync       Sync          `yaml:"config_sync"`
	Limits     config.Limits `yaml:"limits"`
}
type Server struct {
	Listen            string `yaml:"listen"`
	ReadHeaderTimeout string `yaml:"read_header_timeout"`
	IdleTimeout       string `yaml:"idle_timeout"`
	RequestTimeout    string `yaml:"request_timeout"`
	ShutdownTimeout   string `yaml:"shutdown_timeout"`
	MaxHeaderBytes    int    `yaml:"max_header_bytes"`
	MaxBodyBytes      int64  `yaml:"max_body_bytes"`
	MaxInFlight       int    `yaml:"max_in_flight"`
	CertFile          string `yaml:"cert_file"`
	KeyFile           string `yaml:"key_file"`
}
type Admin struct {
	Listen       string `yaml:"listen"`
	Metrics      bool   `yaml:"metrics"`
	Pprof        bool   `yaml:"pprof"`
	AuthTokenEnv string `yaml:"auth_token_env"`
}
type Management struct {
	Listen         string `yaml:"listen"`
	AuthTokenEnv   string `yaml:"auth_token_env"`
	Principal      string `yaml:"principal"`
	RequestTimeout string `yaml:"request_timeout"`
	MaxBodyBytes   int64  `yaml:"max_body_bytes"`
	MaxInFlight    int    `yaml:"max_in_flight"`
	CertFile       string `yaml:"cert_file"`
	KeyFile        string `yaml:"key_file"`
}
type Logging struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}
type Postgres struct {
	DSNEnv          string `yaml:"dsn_env"`
	MaxConns        int32  `yaml:"max_conns"`
	MinConns        int32  `yaml:"min_conns"`
	ConnectTimeout  string `yaml:"connect_timeout"`
	QueryTimeout    string `yaml:"query_timeout"`
	MaxConnLifetime string `yaml:"max_conn_lifetime"`
}
type Sync struct {
	StartupTimeout string `yaml:"startup_timeout"`
	PollInterval   string `yaml:"poll_interval"`
	MaxStaleness   string `yaml:"max_staleness"`
	Notifications  bool   `yaml:"notifications"`
}

func Defaults() Config {
	return Config{
		Version:    1,
		Server:     Server{Listen: ":8080", ReadHeaderTimeout: "5s", IdleTimeout: "60s", RequestTimeout: "15s", ShutdownTimeout: "10s", MaxHeaderBytes: 16384, MaxBodyBytes: 16777216, MaxInFlight: 2048},
		Admin:      Admin{Listen: "127.0.0.1:9090", Metrics: true},
		Management: Management{Listen: "127.0.0.1:9080", AuthTokenEnv: "ROUTETUS_MANAGEMENT_TOKEN", Principal: "management-token", RequestTimeout: "10s", MaxBodyBytes: 4194304, MaxInFlight: 32},
		Logging:    Logging{Level: "info", Format: "json"},
		Postgres:   Postgres{DSNEnv: "DATABASE_URL", MaxConns: 4, ConnectTimeout: "3s", QueryTimeout: "2s", MaxConnLifetime: "30m"},
		Sync:       Sync{StartupTimeout: "30s", PollInterval: "10s", MaxStaleness: "5m", Notifications: true},
		Limits:     config.DefaultLimits,
	}
}

func Load(path string) (Config, error) {
	c := Defaults()
	f, err := os.Open(path)
	if err != nil {
		return c, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		return c, err
	}
	if len(data) > 1<<20 {
		return c, fmt.Errorf("bootstrap exceeds 1 MiB")
	}
	var node yaml.Node
	if err = yaml.Unmarshal(data, &node); err != nil {
		return c, fmt.Errorf("invalid bootstrap YAML")
	}
	if err = checkNodes(&node); err != nil {
		return c, err
	}
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err = d.Decode(&c); err != nil {
		return c, fmt.Errorf("bootstrap YAML: %w", err)
	}
	var extra any
	if err = d.Decode(&extra); err != io.EOF {
		return c, fmt.Errorf("bootstrap must contain exactly one YAML document")
	}
	return c, c.Validate()
}
func checkNodes(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode || n.Tag == "!!null" || n.Tag == "!!merge" {
		return fmt.Errorf("bootstrap does not accept aliases, merges, or null values")
	}
	for _, child := range n.Content {
		if err := checkNodes(child); err != nil {
			return err
		}
	}
	return nil
}
func Duration(s string) time.Duration { d, _ := time.ParseDuration(s); return d }
func (c Config) Validate() error {
	if c.Version != 1 {
		return fmt.Errorf("version must be 1")
	}
	for name, address := range map[string]string{"server.listen": c.Server.Listen, "admin.listen": c.Admin.Listen, "management.listen": c.Management.Listen} {
		if _, _, err := net.SplitHostPort(address); err != nil {
			return fmt.Errorf("%s must be host:port", name)
		}
	}
	if c.Server.Listen == c.Admin.Listen || c.Server.Listen == c.Management.Listen || c.Admin.Listen == c.Management.Listen {
		return fmt.Errorf("listeners must be distinct")
	}
	for name, s := range map[string]string{
		"server.read_header_timeout": c.Server.ReadHeaderTimeout, "server.idle_timeout": c.Server.IdleTimeout, "server.request_timeout": c.Server.RequestTimeout, "server.shutdown_timeout": c.Server.ShutdownTimeout,
		"management.request_timeout": c.Management.RequestTimeout, "postgres.connect_timeout": c.Postgres.ConnectTimeout, "postgres.query_timeout": c.Postgres.QueryTimeout, "postgres.max_conn_lifetime": c.Postgres.MaxConnLifetime,
		"config_sync.startup_timeout": c.Sync.StartupTimeout, "config_sync.poll_interval": c.Sync.PollInterval, "config_sync.max_staleness": c.Sync.MaxStaleness,
	} {
		d, err := time.ParseDuration(s)
		if err != nil || d <= 0 || d > 24*time.Hour {
			return fmt.Errorf("%s must be greater than 0s and at most 24h", name)
		}
	}
	if c.Server.MaxHeaderBytes < 1024 || c.Server.MaxHeaderBytes > 1048576 || c.Server.MaxBodyBytes < 1 || c.Server.MaxBodyBytes > 1<<30 || c.Server.MaxInFlight < 1 || c.Server.MaxInFlight > 1000000 {
		return fmt.Errorf("server resource limits are outside supported ranges")
	}
	if c.Management.MaxBodyBytes < 1 || c.Management.MaxBodyBytes > 64<<20 || c.Management.MaxInFlight < 1 || c.Management.MaxInFlight > 10000 {
		return fmt.Errorf("management resource limits are outside supported ranges")
	}
	if c.Postgres.MaxConns < 1 || c.Postgres.MaxConns > 1000 || c.Postgres.MinConns < 0 || c.Postgres.MinConns > c.Postgres.MaxConns {
		return fmt.Errorf("invalid postgres pool bounds")
	}
	if !EnvName(c.Postgres.DSNEnv) || !EnvName(c.Management.AuthTokenEnv) || (c.Admin.AuthTokenEnv != "" && !EnvName(c.Admin.AuthTokenEnv)) {
		return fmt.Errorf("invalid environment reference")
	}
	if c.Management.Principal == "" || len(c.Management.Principal) > 128 {
		return fmt.Errorf("management.principal must contain 1-128 characters")
	}
	if !Loopback(c.Admin.Listen) && c.Admin.AuthTokenEnv == "" {
		return fmt.Errorf("non-loopback admin listener requires auth_token_env")
	}
	if c.Admin.Pprof && c.Admin.AuthTokenEnv == "" {
		return fmt.Errorf("pprof requires admin.auth_token_env")
	}
	if !Loopback(c.Management.Listen) && c.Management.CertFile == "" {
		return fmt.Errorf("non-loopback management listener requires TLS")
	}
	if (c.Server.CertFile == "") != (c.Server.KeyFile == "") || (c.Management.CertFile == "") != (c.Management.KeyFile == "") {
		return fmt.Errorf("TLS cert_file and key_file must be specified together")
	}
	if c.Logging.Format != "json" && c.Logging.Format != "text" {
		return fmt.Errorf("logging.format must be json or text")
	}
	switch c.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("logging.level must be debug, info, warn, or error")
	}
	return c.Limits.Validate()
}
func Loopback(address string) bool {
	host, _, _ := net.SplitHostPort(address)
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
func EnvName(s string) bool {
	if s == "" || strings.Contains(s, "***") {
		return false
	}
	for i, r := range s {
		if !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || i > 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}
