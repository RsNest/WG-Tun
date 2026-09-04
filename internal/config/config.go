package config

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type ControllerConfig struct {
	Listen    string          `yaml:"listen"`
	DataDir   string          `yaml:"data_dir"`
	TLS       TLSConfig       `yaml:"tls"`
	Auth      AuthConfig      `yaml:"auth"`
	RateLimit RateLimitConfig `yaml:"rate_limit"`
}

type AgentConfig struct {
	NodeName          string   `yaml:"node_name"`
	ControllerURL     string   `yaml:"controller_url"`
	TokenFile         string   `yaml:"token_file"`
	ReconcileInterval Duration `yaml:"reconcile_interval"`
	TLS               AgentTLS `yaml:"tls"`
	StateDir          string   `yaml:"state_dir"`
	HaproxyConfig     string   `yaml:"haproxy_config"`
	HaproxyReload     string   `yaml:"haproxy_reload"`
	DryRunOnly        bool     `yaml:"dry_run_only"`
	MetricsListen     string   `yaml:"metrics_listen"`
}

type TLSConfig struct {
	Required       bool     `yaml:"required"`
	CertFile       string   `yaml:"cert_file"`
	KeyFile        string   `yaml:"key_file"`
	AutoSelfSigned bool     `yaml:"auto_self_signed"`
	DNSNames       []string `yaml:"dns_names"`
}

type AgentTLS struct {
	CAFile             string `yaml:"ca_file"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify"`
}

type AuthConfig struct {
	BootstrapTokenFile string   `yaml:"bootstrap_token_file"`
	HMACRequired       bool     `yaml:"hmac_required"`
	MaxSkew            Duration `yaml:"max_skew"`
}

type RateLimitConfig struct {
	MutatingRPS float64 `yaml:"mutating_rps"`
	Burst       int     `yaml:"burst"`
}

func LoadController(path string) (*ControllerConfig, error) {
	cfg := DefaultController()
	if path != "" {
		if err := loadYAML(path, cfg); err != nil {
			return nil, err
		}
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func LoadAgent(path string) (*AgentConfig, error) {
	cfg := DefaultAgent()
	if path != "" {
		if err := loadYAML(path, cfg); err != nil {
			return nil, err
		}
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func DefaultController() *ControllerConfig {
	return &ControllerConfig{
		Listen:  "127.0.0.1:8443",
		DataDir: "./data",
		TLS: TLSConfig{
			Required:       true,
			CertFile:       "./certs/server.crt",
			KeyFile:        "./certs/server.key",
			AutoSelfSigned: true,
		},
		Auth: AuthConfig{
			BootstrapTokenFile: "./data/bootstrap.token",
			HMACRequired:       true,
			MaxSkew:            Duration(5 * time.Minute),
		},
		RateLimit: RateLimitConfig{MutatingRPS: 10, Burst: 20},
	}
}

func DefaultAgent() *AgentConfig {
	return &AgentConfig{
		ReconcileInterval: Duration(10 * time.Second),
		StateDir:          "/run/transitforge",
		HaproxyConfig:     "/etc/haproxy/haproxy.cfg",
		HaproxyReload:     "systemctl",
		TLS:               AgentTLS{},
		MetricsListen:     "127.0.0.1:9101",
	}
}

func (c *ControllerConfig) Validate() error {
	if c.Listen == "" {
		return fmt.Errorf("listen is required")
	}
	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	if c.DataDir == "" {
		return fmt.Errorf("data_dir is required")
	}
	if c.TLS.Required && (c.TLS.CertFile == "" || c.TLS.KeyFile == "") && !c.TLS.AutoSelfSigned {
		return fmt.Errorf("tls cert_file and key_file are required when tls.required")
	}
	if c.Auth.MaxSkew == 0 {
		c.Auth.MaxSkew = Duration(5 * time.Minute)
	}
	if c.RateLimit.MutatingRPS <= 0 {
		c.RateLimit.MutatingRPS = 10
	}
	if c.RateLimit.Burst <= 0 {
		c.RateLimit.Burst = 20
	}
	return nil
}

func (c *AgentConfig) Validate() error {
	if strings.TrimSpace(c.NodeName) == "" {
		return fmt.Errorf("node_name is required")
	}
	if strings.TrimSpace(c.ControllerURL) == "" {
		return fmt.Errorf("controller_url is required")
	}
	if strings.TrimSpace(c.TokenFile) == "" {
		return fmt.Errorf("token_file is required")
	}
	if c.ReconcileInterval.Duration() < time.Second {
		c.ReconcileInterval = Duration(10 * time.Second)
	}
	switch strings.ToLower(strings.TrimSpace(c.HaproxyReload)) {
	case "", "systemctl":
		c.HaproxyReload = "systemctl"
	case "external":
		c.HaproxyReload = "external"
	default:
		return fmt.Errorf("haproxy_reload must be systemctl or external")
	}
	return nil
}

const (
	dbFileName       = "transitforge.db"
	legacyDBFileName = "proxyctl.db" // one-time rename of the pre-rebrand SQLite file
)

func (c *ControllerConfig) DBPath() string {
	return ResolveDBPath(c.DataDir)
}

// ResolveDBPath returns the SQLite path under dataDir.
// If only the pre-rebrand filename exists, it is renamed (including WAL/SHM).
func ResolveDBPath(dataDir string) string {
	newPath := filepath.Join(dataDir, dbFileName)
	oldPath := filepath.Join(dataDir, legacyDBFileName)
	if fileExists(newPath) {
		return newPath
	}
	if fileExists(oldPath) {
		if err := renameSQLiteFile(oldPath, newPath); err != nil {
			return oldPath
		}
	}
	return newPath
}

func renameSQLiteFile(oldPath, newPath string) error {
	if err := os.Rename(oldPath, newPath); err != nil {
		return err
	}
	for _, suf := range []string{"-wal", "-shm"} {
		from, to := oldPath+suf, newPath+suf
		if fileExists(from) {
			_ = os.Rename(from, to)
		}
	}
	return nil
}

func loadYAML(path string, out any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config %s: %w", path, err)
	}
	if err := yaml.Unmarshal(b, out); err != nil {
		return fmt.Errorf("parse config %s: %w", path, err)
	}
	return nil
}

func EnsureSelfSigned(certFile, keyFile, listen string, extraDNS []string) error {
	if fileExists(certFile) && fileExists(keyFile) {
		return nil
	}
	if fileExists(certFile) != fileExists(keyFile) {
		return fmt.Errorf("TLS cert/key pair is incomplete (%s / %s)", certFile, keyFile)
	}
	if err := os.MkdirAll(filepath.Dir(certFile), 0o750); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(keyFile), 0o750); err != nil {
		return err
	}
	host, _, _ := net.SplitHostPort(listen)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	dns := uniqueDNS(append([]string{"localhost"}, extraDNS...))
	tpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "transitforge-controller", Organization: []string{"TransitForge"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dns,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	if ip := net.ParseIP(host); ip != nil {
		tpl.IPAddresses = append(tpl.IPAddresses, ip)
	} else if host != "" && host != "0.0.0.0" && host != "::" {
		tpl.DNSNames = uniqueDNS(append(tpl.DNSNames, host))
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	certOut, err := os.OpenFile(certFile, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o640)
	if err != nil {
		return err
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		return err
	}
	keyOut, err := os.OpenFile(keyFile, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer keyOut.Close()
	return pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

func uniqueDNS(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, n := range in {
		n = strings.ToLower(strings.TrimSpace(n))
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
