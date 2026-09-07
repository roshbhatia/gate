package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultValidates(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadReadsChainsAndRejectsUnknownEvents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	t.Setenv("GATE_CONFIG", path)
	body := `version: gate.config/v1
defaults: { timeout: 3s }
chains:
  PreToolUse:
    - { provider: bash-guard, match: "^Bash$", args: { rules: /tmp/rules.json } }
    - { provider: read-router, match: "^Read$", timeout: 500ms }
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	steps := cfg.Steps("PreToolUse")
	if len(steps) != 2 || steps[0].Args["rules"] != "/tmp/rules.json" {
		t.Fatalf("steps = %+v", steps)
	}
	if cfg.StepTimeout(steps[0]) != 3*time.Second || cfg.StepTimeout(steps[1]) != 500*time.Millisecond {
		t.Fatalf("timeouts = %v, %v", cfg.StepTimeout(steps[0]), cfg.StepTimeout(steps[1]))
	}

	bad := "version: gate.config/v1\nchains:\n  OnCoffee:\n    - { provider: x, match: '(' }\n"
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Load()
	if err == nil || !strings.Contains(err.Error(), "OnCoffee") || !strings.Contains(err.Error(), "match") {
		t.Fatalf("bad config error = %v", err)
	}
}

func TestSchemaMentionsChains(t *testing.T) {
	schema, err := Schema()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(schema), `"chains"`) || !strings.Contains(string(schema), "gate.config/v1") {
		t.Fatalf("schema = %s", schema)
	}
}
