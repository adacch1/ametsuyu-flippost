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
