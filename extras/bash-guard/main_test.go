package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/roshbhatia/gate/extras/internal/bulkread"
	"github.com/roshbhatia/gate/pkg/gate"
)

func request(command, cwd string) gate.Request {
	return gate.Request{Event: gate.Envelope{Event: "PreToolUse", Tool: "Bash", Cwd: cwd,
		Input: map[string]any{"command": command, "timeout": float64(30)}}}
}

func TestBoundAndAlreadyBounded(t *testing.T) {
	for _, command := range []string{"cat /etc/hosts", "rg TODO .", "find . -name '*.go'", "git log --oneline"} {
		if rewritten, ok := bound(command); !ok || !strings.Contains(rewritten, "head -c 16384") {
			t.Fatalf("%q was not bounded: %q", command, rewritten)
		}
	}
	for _, command := range []string{"cat /etc/hosts | head -20", "rg TODO . --max-count=5", "find . -delete", "git status", "cat"} {
		if rewritten, ok := bound(command); ok {
			t.Fatalf("%q was rewritten as %q", command, rewritten)
		}
	}
}

func TestDecideDeniesBoundsAndRoutes(t *testing.T) {
	dir := t.TempDir()
	rulesPath := filepath.Join(dir, "rules.json")
	if err := os.WriteFile(rulesPath, []byte(`[{"regex":"rm[ ]+-rf","reason":"unsafe"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	rules, err := loadRules(rulesPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadRules(""); err == nil {
		t.Fatal("no rules path was accepted")
	}
	if out := Decide(request("rm -rf target", dir), rules); out.Kind != gate.Deny || out.Message != "unsafe" {
		t.Fatalf("destructive decision = %+v", out)
	}
	out := Decide(request("rg TODO .", dir), rules)
	if out.Kind != gate.Allow || out.UpdatedInput["timeout"] != float64(30) || !strings.Contains(out.Context, "capped") {
		t.Fatalf("bounded decision = %+v", out)
	}
	large := filepath.Join(dir, "large.txt")
	if err := os.WriteFile(large, []byte(strings.Repeat(strings.Repeat("x", 100)+"\n", 200)), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"cat large.txt", "less " + large, "bat -p large.txt"} {
		if out := Decide(request(command, dir), rules); out.Kind != gate.Deny || !strings.Contains(out.Message, bulkread.DefaultReader) {
			t.Fatalf("%q decision = %+v", command, out)
		}
	}
	for _, command := range []string{"head -n 20 large.txt", "cat large.txt other.txt", "cat large.txt | wc -l"} {
		if out := Decide(request(command, dir), rules); out.Kind == gate.Deny {
			t.Fatalf("%q was denied", command)
		}
	}
	custom := request("cat large.txt", dir)
	custom.Args = map[string]any{"reader": "ask -t bulk-read", "trigger_kib": float64(1)}
	if out := Decide(custom, rules); !strings.Contains(out.Message, "ask -t bulk-read --var") {
		t.Fatalf("custom reader decision = %+v", out)
	}
	background := request("cat /etc/hosts", dir)
	background.Event.Input["run_in_background"] = true
	if out := Decide(background, rules); out.Kind != gate.Pass {
		t.Fatalf("background decision = %+v", out)
	}
}
