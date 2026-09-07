package event

import (
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestNormalizeClaudePayloads(t *testing.T) {
	env, err := Normalize("claude", "", fixture(t, "PreToolUse-read.json"))
	if err != nil {
		t.Fatal(err)
	}
	if env.Event != "PreToolUse" || env.Tool != "Read" || env.Input["file_path"] != "/repo/big.go" || env.Cwd != "/repo" {
		t.Fatalf("read envelope = %+v", env)
	}
	env, err = Normalize("codex", "PreToolUse", fixture(t, "PreToolUse-agent-critic.json"))
	if err != nil {
		t.Fatal(err)
	}
	if env.Harness != "codex" || env.Input["subagent_type"] != "Explore" {
		t.Fatalf("agent envelope = %+v", env)
	}
	env, err = Normalize("claude", "", fixture(t, "PostToolUse-agent-report.json"))
	if err != nil || len(env.Response) == 0 {
		t.Fatalf("post envelope = %+v, %v", env, err)
	}
	env, err = Normalize("claude", "", fixture(t, "Stop.json"))
	if err != nil || env.LastMessage != "Done." || env.StopActive {
		t.Fatalf("stop envelope = %+v, %v", env, err)
	}
	env, err = Normalize("claude", "", fixture(t, "SubagentStart.json"))
	if err != nil || env.Agent.Type != "Explore" || env.Agent.ID != "a1" {
		t.Fatalf("subagent envelope = %+v, %v", env, err)
	}
}

func TestNormalizeNeedsAnEventName(t *testing.T) {
	if _, err := Normalize("claude", "", []byte(`{"cwd":"/x"}`)); err == nil {
		t.Fatal("a payload without an event name was accepted")
	}
	env, err := Normalize("json", "Stop", []byte(`{"harness":"opencode","tool":"bash"}`))
	if err != nil || env.Event != "Stop" || env.Harness != "opencode" || env.Version == "" {
		t.Fatalf("json envelope = %+v, %v", env, err)
	}
}
