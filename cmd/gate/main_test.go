package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/roshbhatia/go-utils/completion"
)

func TestCompletionSpecCoversEveryCommandAndShell(t *testing.T) {
	spec := completionSpec(command())
	names := map[string]bool{}
	for _, sub := range spec.Subcommands {
		names[sub.Name] = true
	}
	for _, want := range []string{"hook", "config", "provider", "log", "completion"} {
		if !names[want] {
			t.Fatalf("spec lacks %q: %v", want, names)
		}
	}
	for _, shell := range []string{"bash", "fish", "nu", "zsh"} {
		if _, err := completion.Generate(shell, spec); err != nil {
			t.Fatalf("%s completion: %v", shell, err)
		}
	}
	if !strings.Contains(completion.Markdown(spec), "gate hook") {
		t.Fatal("markdown reference lacks the hook command")
	}
}

func TestConfigValidateRejectsAMissingProvider(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "config.yaml")
	t.Setenv("GATE_CONFIG", config)
	body := "version: gate.config/v1\nproviders: { directory: " + filepath.Join(dir, "providers") + " }\nchains:\n  PreToolUse:\n    - { provider: ghost }\n"
	if err := os.WriteFile(config, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	root := command()
	root.SetArgs([]string{"config", "validate"})
	root.SetOut(new(bytes.Buffer))
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("validate error = %v", err)
	}
}

func TestConfigShowIsJSON(t *testing.T) {
	t.Setenv("GATE_CONFIG", filepath.Join(t.TempDir(), "absent.yaml"))
	var out bytes.Buffer
	root := command()
	root.SetArgs([]string{"config", "show"})
	root.SetOut(&out)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil || decoded["version"] != "gate.config/v1" {
		t.Fatalf("show output = %s, %v", out.String(), err)
	}
}

func TestLogFieldsResolveDeclaredVariables(t *testing.T) {
	t.Setenv("GATE_TEST_BOUND", " s1 ")
	t.Setenv("GATE_TEST_UNSET", "")
	if got := logFields(nil); got != nil {
		t.Errorf("no declaration = %v, want nil", got)
	}
	if got := logFields(map[string]string{"scope": "GATE_TEST_UNSET"}); got != nil {
		t.Errorf("an unset variable = %v, want nil", got)
	}
	got := logFields(map[string]string{"session": "GATE_TEST_BOUND", "scope": "GATE_TEST_UNSET"})
	if len(got) != 1 || got["session"] != "s1" {
		t.Errorf("declared fields = %v, want only a trimmed session", got)
	}
}
