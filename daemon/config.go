package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
)

// loopbackRe mirrors config.schema.json bind_host pattern: loopback only.
var loopbackRe = regexp.MustCompile(`^(127\.0\.0\.1|::1|localhost)$`)

type Config struct {
	BindHost string            `json:"bind_host"`
	BindPort int               `json:"bind_port"`
	Tokens   map[string]string `json:"tokens"`
	Ingress  struct {
		Mode string `json:"mode"`
	} `json:"ingress"`
	Thermal struct {
		WarnC      float64 `json:"warn_c"`
		GateC      float64 `json:"gate_c"`
		FailClosed bool    `json:"fail_closed"`
	} `json:"thermal"`
	SMS struct {
		Enabled       bool   `json:"enabled"`
		RedactDefault bool   `json:"redact_default"`
		Forward       bool   `json:"forward"`
		Path          string `json:"path"`
	} `json:"sms"`
	RateLimits struct {
		DefaultPerMin int `json:"default_per_min"`
		SMSPerMin     int `json:"sms_per_min"`
		RadioPerMin   int `json:"radio_per_min"`
	} `json:"rate_limits"`
	CPU struct {
		Mode string `json:"mode"` // auto | performance | balanced | eco | off
	} `json:"cpu"`
	Hotspot struct {
		SSIDWhitelist []string `json:"ssid_whitelist"` // auto-toggle: off when seen, on when absent
	} `json:"hotspot"`
	Dashboard struct {
		// OpenReads: serve read-status GETs WITHOUT a token (tailnet convenience).
		// Reads only — radio-control and sms always keep their tokens. Off by default.
		OpenReads bool `json:"open_reads"`
	} `json:"dashboard"`
}

// CPUMode returns the configured CPU policy mode, defaulting to "auto".
func (c *Config) CPUMode() string {
	if c.CPU.Mode == "" {
		return "auto"
	}
	return c.CPU.Mode
}

// persistThermalLimits rewrites only thermal.warn_c/gate_c in the config file,
// preserving every other key (including ones not in the Config struct, e.g.
// hotspot). Marshaling the struct would drop those, so we edit the raw JSON.
func persistThermalLimits(path string, warnC, gateC float64) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	th, _ := m["thermal"].(map[string]any)
	if th == nil {
		th = map[string]any{}
		m["thermal"] = th
	}
	th["warn_c"] = warnC
	th["gate_c"] = gateC
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeConfigAtomic(path, out)
}

// writeConfigAtomic writes via a temp file + rename so a crash mid-write can
// never leave a truncated config.json that fails to parse at next boot (which
// would strand the headless device). The temp file is created 0600 in the same
// dir so the rename stays on one filesystem.
func writeConfigAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// persistOpenReads rewrites only dashboard.open_reads, preserving other keys.
func persistOpenReads(path string, open bool) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	dash, _ := m["dashboard"].(map[string]any)
	if dash == nil {
		dash = map[string]any{}
		m["dashboard"] = dash
	}
	dash["open_reads"] = open
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeConfigAtomic(path, out)
}

// persistHotspotWhitelist rewrites only hotspot.ssid_whitelist, preserving
// every other key (enable_on_boot lives in the same object).
func persistHotspotWhitelist(path string, ssids []string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	hs, _ := m["hotspot"].(map[string]any)
	if hs == nil {
		hs = map[string]any{}
		m["hotspot"] = hs
	}
	hs["ssid_whitelist"] = ssids
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeConfigAtomic(path, out)
}

func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("config parse: %w", err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Validate enforces the load-bearing safety invariants. A public bind, a
// missing read-status token, or a disabled fail-closed thermal gate is fatal:
// the daemon refuses to start rather than run in an unsafe posture.
func (c *Config) Validate() error {
	if c.Ingress.Mode == "" {
		c.Ingress.Mode = "loopback"
	}
	if c.Ingress.Mode == "loopback" && !loopbackRe.MatchString(c.BindHost) {
		return fmt.Errorf("refusing non-loopback bind_host %q in loopback mode", c.BindHost)
	}
	// Even in tailscale mode the local listener stays on loopback; ingress is a
	// userspace proxy, never a public bind.
	if !loopbackRe.MatchString(c.BindHost) {
		return fmt.Errorf("refusing public bind_host %q", c.BindHost)
	}
	if c.BindPort < 1024 || c.BindPort > 65535 {
		return fmt.Errorf("bind_port %d out of range", c.BindPort)
	}
	if c.Tokens["read-status"] == "" {
		return fmt.Errorf("missing required token: read-status")
	}
	if !c.Thermal.FailClosed {
		return fmt.Errorf("thermal.fail_closed must be true")
	}
	if c.SMS.Forward {
		return fmt.Errorf("sms.forward must be false (no default forwarding)")
	}
	return nil
}
