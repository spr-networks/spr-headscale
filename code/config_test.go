package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigMigratesLegacyDERPOff(t *testing.T) {
	oldConfigFile := ConfigFile
	oldConfig := gConfig
	t.Cleanup(func() {
		ConfigFile = oldConfigFile
		gConfig = oldConfig
	})

	ConfigFile = filepath.Join(t.TempDir(), "config.json")
	legacy := Config{
		BaseDomain:  "headscale.internal",
		MagicDNS:    true,
		DERPEnabled: false,
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ConfigFile, data, 0600); err != nil {
		t.Fatal(err)
	}

	gConfig = defaultConfig()
	if err := loadConfig(); err != nil {
		t.Fatal(err)
	}
	if !gConfig.DERPEnabled {
		t.Fatal("legacy DERP-disabled configuration was not migrated")
	}

	persisted, err := os.ReadFile(ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	var got Config
	if err := json.Unmarshal(persisted, &got); err != nil {
		t.Fatal(err)
	}
	if !got.DERPEnabled {
		t.Fatal("migrated DERP setting was not persisted")
	}
}

func TestValidateServerURL(t *testing.T) {
	good := map[string]string{
		"http://100.64.10.2:8080":       "http://100.64.10.2:8080",
		"https://vpn.example.com":       "https://vpn.example.com",
		"https://vpn.example.com/":      "https://vpn.example.com",
		"https://vpn.example.com:8443":  "https://vpn.example.com:8443",
		"http://headscale-lan.internal": "http://headscale-lan.internal",
	}
	for in, want := range good {
		got, err := validateServerURL(in)
		if err != nil {
			t.Errorf("validateServerURL(%q) unexpected error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("validateServerURL(%q) = %q, want %q", in, got, want)
		}
	}

	bad := []string{
		"",
		"ftp://example.com",
		"example.com",
		"http://user:pass@example.com",
		"https://example.com/path",
		"https://example.com?x=1",
		"http://example.com#frag",
		"http://exa mple.com",
		"http://example.com:notaport",
		"http://$(reboot)",
		"http://example.com\nserver_url: evil",
	}
	for _, in := range bad {
		if _, err := validateServerURL(in); err == nil {
			t.Errorf("validateServerURL(%q) expected error, got none", in)
		}
	}
}

func TestValidateConfigBaseDomain(t *testing.T) {
	for _, domain := range []string{"headscale.internal", "ts.lan.example.com", "a1.b2"} {
		cfg := Config{BaseDomain: domain}
		if err := validateConfig(&cfg); err != nil {
			t.Errorf("validateConfig BaseDomain=%q unexpected error: %v", domain, err)
		}
	}
	for _, domain := range []string{"single", "UPPER.CASE", "-bad.start", "bad-.example", "exa mple.com", "inject: yes"} {
		cfg := Config{BaseDomain: domain}
		if err := validateConfig(&cfg); err == nil {
			t.Errorf("validateConfig BaseDomain=%q expected error, got none", domain)
		}
	}
}

func TestValidateConfigServerURLWithinBaseDomain(t *testing.T) {
	cfg := Config{ServerURL: "https://headscale.example.com", BaseDomain: "example.com"}
	if err := validateConfig(&cfg); err == nil {
		t.Error("expected error: server_url host inside base_domain")
	}
	cfg = Config{ServerURL: "https://example.com", BaseDomain: "example.com"}
	if err := validateConfig(&cfg); err == nil {
		t.Error("expected error: server_url host equals base_domain")
	}
	cfg = Config{ServerURL: "https://headscale.example.net", BaseDomain: "example.com"}
	if err := validateConfig(&cfg); err != nil {
		t.Errorf("unexpected error for unrelated domains: %v", err)
	}
	// empty BaseDomain falls back to the default
	cfg = Config{}
	if err := validateConfig(&cfg); err != nil {
		t.Errorf("unexpected error for empty config: %v", err)
	}
	if cfg.BaseDomain != "headscale.internal" {
		t.Errorf("BaseDomain default not applied: %q", cfg.BaseDomain)
	}
}

func TestValidateUsername(t *testing.T) {
	for _, name := range []string{"alice", "bob.smith", "carol-2", "dave_x", "eve@example.com", "Ab"} {
		if err := validateUsername(name); err != nil {
			t.Errorf("validateUsername(%q) unexpected error: %v", name, err)
		}
	}
	for _, name := range []string{"", "a", "1abc", "-abc", "a b", "a;rm -rf /", "a@b@c", "$(id)", strings.Repeat("a", 64)} {
		if err := validateUsername(name); err == nil {
			t.Errorf("validateUsername(%q) expected error, got none", name)
		}
	}
}

func TestValidateExpiration(t *testing.T) {
	for _, exp := range []string{"30m", "24h", "90d", "1h30m", "1w", "1y"} {
		if err := validateExpiration(exp); err != nil {
			t.Errorf("validateExpiration(%q) unexpected error: %v", exp, err)
		}
	}
	for _, exp := range []string{"", "h", "30", "30 m", "-1h", "1h;id", "never"} {
		if err := validateExpiration(exp); err == nil {
			t.Errorf("validateExpiration(%q) expected error, got none", exp)
		}
	}
}

func TestValidateID(t *testing.T) {
	for _, id := range []string{"1", "42", "9223372036854775807"} {
		if err := validateID(id); err != nil {
			t.Errorf("validateID(%q) unexpected error: %v", id, err)
		}
	}
	for _, id := range []string{"", "0", "00", "-1", "1.5", "1e3", "abc", "1;id", "99999999999999999999"} {
		if err := validateID(id); err == nil {
			t.Errorf("validateID(%q) expected error, got none", id)
		}
	}
}

func TestRenderHeadscaleConfig(t *testing.T) {
	cfg := Config{ServerURL: "https://vpn.example.com", BaseDomain: "hs.internal", MagicDNS: true, DERPEnabled: true}
	data, err := renderHeadscaleConfig(cfg, "100.64.10.2")
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	yaml := string(data)
	for _, want := range []string{
		`server_url: "https://vpn.example.com"`,
		`listen_addr: "100.64.10.2:8080"`,
		`base_domain: "hs.internal"`,
		"magic_dns: true",
		"- https://controlplane.tailscale.com/derpmap/default",
		`metrics_listen_addr: ""`,
		"grpc_allow_insecure: false",
		`unix_socket: "/var/run/headscale/headscale.sock"`,
		`private_key_path: "/configs/spr-headscale/noise_private.key"`,
		`path: "/state/plugins/spr-headscale/data/db.sqlite"`,
		"enabled: false", // embedded DERP server stays off
	} {
		if !strings.Contains(yaml, want) {
			t.Errorf("rendered config missing %q\n---\n%s", want, yaml)
		}
	}
}

func TestRenderHeadscaleConfigMigratesLegacyDERPOff(t *testing.T) {
	cfg := Config{MagicDNS: false, DERPEnabled: false}
	data, err := renderHeadscaleConfig(cfg, "100.64.10.2")
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	yaml := string(data)
	for _, want := range []string{
		`server_url: "http://100.64.10.2:8080"`, // derived from container IP
		"magic_dns: false",
		"- https://controlplane.tailscale.com/derpmap/default",
		"auto_update_enabled: true",
		`base_domain: "headscale.internal"`,
	} {
		if !strings.Contains(yaml, want) {
			t.Errorf("rendered config missing %q\n---\n%s", want, yaml)
		}
	}
}

func TestRenderHeadscaleConfigRejectsBadInput(t *testing.T) {
	if _, err := renderHeadscaleConfig(Config{ServerURL: "http://evil\ninjected: true"}, "100.64.10.2"); err == nil {
		t.Error("expected error for URL with newline")
	}
	if _, err := renderHeadscaleConfig(Config{}, "not-an-ip"); err == nil {
		t.Error("expected error for invalid listen IP")
	}
}

func TestMaskKey(t *testing.T) {
	key := "hskey-auth-abcdef1234567890abcdef1234567890"
	masked := maskKey(key)
	if masked == key {
		t.Error("maskKey returned the full key")
	}
	if !strings.HasPrefix(key, strings.TrimSuffix(masked, "…")) {
		t.Errorf("mask %q is not a prefix of the key", masked)
	}
	if len(masked) > 15 {
		t.Errorf("mask too long: %q", masked)
	}
	if maskKey("short") != "short" {
		t.Error("short keys should pass through")
	}
}

func TestPreAuthKeyMaskingInList(t *testing.T) {
	// simulate `headscale preauthkeys list -o json` output
	raw := `[{"user":{"id":1,"name":"alice"},"id":7,"key":"hskey-auth-abcdef1234567890abcdef1234567890",
	  "reusable":true,"ephemeral":false,"used":false,
	  "expiration":{"seconds":4102444800},"created_at":{"seconds":1751000000}}]`
	keys, err := decodeList[hsPreAuthKey]([]byte(raw))
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	info := preAuthKeyInfo(keys[0])
	out, _ := json.Marshal(info)
	if strings.Contains(string(out), "abcdef1234567890abcdef") {
		t.Errorf("full key leaked in list response: %s", out)
	}
	if info.UserName != "alice" || info.UserID != 1 || !info.Reusable {
		t.Errorf("unexpected key info: %+v", info)
	}
	if info.Expired {
		t.Error("key expiring in 2100 marked expired")
	}
	if info.CreatedAt == "" || info.Expires == "" {
		t.Errorf("timestamps not rendered: %+v", info)
	}
}

func TestDecodeListNull(t *testing.T) {
	keys, err := decodeList[hsPreAuthKey]([]byte("null\n"))
	if err != nil || len(keys) != 0 {
		t.Errorf("decodeList(null) = %v, %v; want empty, nil", keys, err)
	}
	nodes, err := decodeList[hsNode]([]byte(""))
	if err != nil || len(nodes) != 0 {
		t.Errorf("decodeList(empty) = %v, %v; want empty, nil", nodes, err)
	}
}

func TestNodeInfoMapping(t *testing.T) {
	raw := `[{"id":3,"name":"phone","given_name":"phone","online":true,
	  "ip_addresses":["100.64.0.3","fd7a:115c:a1e0::3"],
	  "user":{"id":2,"name":"bob"},
	  "last_seen":{"seconds":1751000000},
	  "expiry":{"seconds":100},
	  "created_at":{"seconds":1750000000}}]`
	nodes, err := decodeList[hsNode]([]byte(raw))
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	info := nodeInfo(nodes[0])
	if info.User != "bob" || info.UserID != 2 || !info.Online || len(info.IPs) != 2 {
		t.Errorf("unexpected node info: %+v", info)
	}
	if !info.Expired {
		t.Error("node with 1970 expiry not marked expired")
	}
	if info.LastSeen == "" || !strings.HasSuffix(info.LastSeen, "Z") {
		t.Errorf("LastSeen not RFC3339: %q", info.LastSeen)
	}
	// zero expiry means "never expires"
	nodes[0].Expiry = &pbTime{}
	if nodeInfo(nodes[0]).Expired {
		t.Error("zero expiry must not be marked expired")
	}
	if nodeInfo(nodes[0]).Expiry != "" {
		t.Error("zero expiry must render empty")
	}
}
