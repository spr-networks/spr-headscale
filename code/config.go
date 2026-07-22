package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"text/template"

	_ "embed"
)

var TEST_PREFIX = os.Getenv("TEST_PREFIX")

var (
	ConfigFile          = TEST_PREFIX + "/configs/spr-headscale/config.json"
	NoiseKeyPath        = TEST_PREFIX + "/configs/spr-headscale/noise_private.key"
	HeadscaleConfigFile = TEST_PREFIX + "/etc/headscale/config.yaml"
	HeadscaleDBPath     = TEST_PREFIX + "/state/plugins/spr-headscale/data/db.sqlite"
	HeadscaleSocketDir  = TEST_PREFIX + "/var/run/headscale"
	HeadscaleSocketPath = TEST_PREFIX + "/var/run/headscale/headscale.sock"
)

// Config is the plugin configuration persisted at /configs/spr-headscale/config.json.
// It contains no secrets: keys and the headscale database live in files owned by
// the headscale daemon itself.
type Config struct {
	// ServerURL is the URL tailscale clients use to reach headscale
	// (headscale's server_url). Empty means "derive from the container IP":
	// http://<container-ip>:8080 — reachable from the SPR LAN only.
	ServerURL string
	// BaseDomain is the MagicDNS base domain (must differ from the ServerURL host).
	BaseDomain string
	// MagicDNS toggles headscale's magic_dns.
	MagicDNS    bool
	DERPEnabled bool
}

var (
	Configmtx sync.RWMutex
	gConfig   = defaultConfig()
)

func defaultConfig() Config {
	return Config{
		ServerURL:   "",
		BaseDomain:  "headscale.internal",
		MagicDNS:    true,
		DERPEnabled: true,
	}
}

var (
	// hostname / FQDN labels: RFC 1123, lowercase only (headscale lowercases domains)
	rDomain = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)
	rHost   = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9.-]{0,251}[a-zA-Z0-9])?$`)
	rPort   = regexp.MustCompile(`^[0-9]{1,5}$`)
	// Prometheus-style durations as accepted by headscale's --expiration flag
	rDuration = regexp.MustCompile(`^([0-9]+(ms|s|m|h|d|w|y))+$`)
	rUsername = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9._@-]{1,62}$`)
	rDigits   = regexp.MustCompile(`^[0-9]{1,19}$`)
)

// validateServerURL checks and canonicalizes a headscale server_url.
// Only http/https URLs with a plain host[:port] and no path/query/userinfo
// are accepted; the returned string is rebuilt from the parsed parts so no
// unvetted characters can reach the generated YAML.
func validateServerURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid server_url: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("server_url scheme must be http or https")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", fmt.Errorf("server_url must be scheme://host[:port] only")
	}
	host := u.Hostname()
	if host == "" || (!rHost.MatchString(host) && net.ParseIP(host) == nil) {
		return "", fmt.Errorf("server_url has an invalid host")
	}
	out := u.Scheme + "://" + host
	if p := u.Port(); p != "" {
		if !rPort.MatchString(p) {
			return "", fmt.Errorf("server_url has an invalid port")
		}
		out += ":" + p
	}
	return out, nil
}

func validateBaseDomain(domain string) error {
	if !rDomain.MatchString(domain) {
		return fmt.Errorf("base_domain must be a lowercase FQDN (e.g. headscale.internal)")
	}
	return nil
}

// validateConfig validates cross-field constraints and canonicalizes ServerURL.
func validateConfig(c *Config) error {
	if c.BaseDomain == "" {
		c.BaseDomain = defaultConfig().BaseDomain
	}
	if err := validateBaseDomain(c.BaseDomain); err != nil {
		return err
	}
	if c.ServerURL != "" {
		canon, err := validateServerURL(c.ServerURL)
		if err != nil {
			return err
		}
		c.ServerURL = canon
		// mirror headscale's isSafeServerURL check: MagicDNS taking over
		// base_domain must not shadow the control server itself
		host := strings.ToLower(hostOfURL(c.ServerURL))
		if host == c.BaseDomain || strings.HasSuffix(host, "."+c.BaseDomain) {
			return fmt.Errorf("server_url host cannot be within base_domain (it would become unreachable over MagicDNS)")
		}
	}
	return nil
}

func hostOfURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func validateUsername(name string) error {
	if !rUsername.MatchString(name) {
		return fmt.Errorf("invalid username: 2-63 chars, must start with a letter, then letters, digits, '.', '-', '_' or one '@'")
	}
	if strings.Count(name, "@") > 1 {
		return fmt.Errorf("invalid username: at most one '@'")
	}
	return nil
}

func validateExpiration(exp string) error {
	if !rDuration.MatchString(exp) {
		return fmt.Errorf("invalid expiration: use durations like 30m, 24h, 90d")
	}
	return nil
}

func validateID(id string) error {
	if !rDigits.MatchString(id) || strings.TrimLeft(id, "0") == "" {
		return fmt.Errorf("invalid id")
	}
	return nil
}

func loadConfig() error {
	Configmtx.Lock()
	defer Configmtx.Unlock()
	data, err := os.ReadFile(ConfigFile)
	if err != nil {
		if os.IsNotExist(err) {
			// first boot: persist the defaults
			return writeConfigLocked()
		}
		return err
	}
	cfg := defaultConfig()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}
	if err := validateConfig(&cfg); err != nil {
		return err
	}
	if !cfg.DERPEnabled {
		cfg.DERPEnabled = true
		gConfig = cfg
		return writeConfigLocked()
	}
	gConfig = cfg
	return nil
}

// writeConfigLocked atomically persists gConfig (callers hold Configmtx).
func writeConfigLocked() error {
	data, err := json.MarshalIndent(gConfig, "", " ")
	if err != nil {
		return err
	}
	return atomicWrite(ConfigFile, data, 0600)
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

//go:embed templates/config.yaml.tmpl
var headscaleConfigTemplate string

type headscaleTemplateParams struct {
	ServerURL    string
	ListenAddr   string
	BaseDomain   string
	MagicDNS     bool
	DERPEnabled  bool
	NoiseKeyPath string
	DBPath       string
	SocketPath   string
}

// renderHeadscaleConfig produces the headscale config.yaml for cfg.
// listenIP is the container IP on the spr-headscale docker bridge.
func renderHeadscaleConfig(cfg Config, listenIP string) ([]byte, error) {
	if err := validateConfig(&cfg); err != nil {
		return nil, err
	}
	if listenIP == "" || net.ParseIP(listenIP) == nil {
		return nil, fmt.Errorf("invalid listen IP %q", listenIP)
	}
	serverURL := cfg.ServerURL
	if serverURL == "" {
		serverURL = "http://" + listenIP + ":8080"
	}
	params := headscaleTemplateParams{
		ServerURL:    serverURL,
		ListenAddr:   listenIP + ":8080",
		BaseDomain:   cfg.BaseDomain,
		MagicDNS:     cfg.MagicDNS,
		DERPEnabled:  true,
		NoiseKeyPath: NoiseKeyPath,
		DBPath:       HeadscaleDBPath,
		SocketPath:   HeadscaleSocketPath,
	}
	tmpl, err := template.New("config.yaml").Parse(headscaleConfigTemplate)
	if err != nil {
		return nil, err
	}
	var out strings.Builder
	if err := tmpl.Execute(&out, params); err != nil {
		return nil, err
	}
	return []byte(out.String()), nil
}

// writeHeadscaleConfig renders and atomically installs config.yaml.
func writeHeadscaleConfig(cfg Config, listenIP string) error {
	data, err := renderHeadscaleConfig(cfg, listenIP)
	if err != nil {
		return err
	}
	return atomicWrite(HeadscaleConfigFile, data, 0600)
}
