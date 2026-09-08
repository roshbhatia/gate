package event

import (
	"strings"
	"testing"

	"github.com/roshbhatia/gate/pkg/gate"
)

// The shell, read, edit, and post fixtures are payloads captured from
// cursor-agent 2026.08.31; the rest follow the published hook reference.
func TestNormalizeCursorFixtures(t *testing.T) {
	env, err := Normalize("cursor", "", fixture(t, "cursor-preToolUse-shell.json"))
	if err != nil {
		t.Fatal(err)
	}
	if env.Harness != "cursor" || env.Event != "PreToolUse" || env.Tool != "Bash" || env.Input["command"] != "echo hello-from-shell" {
		t.Fatalf("preToolUse envelope = %+v", env)
	}
	if env.Session != "573b52db-79cf-48ce-a8c8-8321b636db5f" || env.Cwd != "/repo" {
		t.Fatalf("preToolUse identity = session %q cwd %q", env.Session, env.Cwd)
	}
	if CarriesRewrite(env) != true {
		t.Fatal("preToolUse cannot carry a rewrite")
	}
	env, err = Normalize("cursor", "", fixture(t, "cursor-beforeShellExecution.json"))
	if err != nil || env.Event != "PreToolUse" || env.Tool != "Bash" || env.Input["command"] != "echo hello-from-shell" || env.Cwd != "/repo" {
		t.Fatalf("beforeShellExecution envelope = %+v, %v", env, err)
	}
	if CarriesRewrite(env) {
		t.Fatal("beforeShellExecution carries a rewrite")
	}
	env, err = Normalize("cursor", "", fixture(t, "cursor-postToolUse-shell.json"))
	if err != nil || env.Event != "PostToolUse" || env.Tool != "Bash" || !strings.Contains(string(env.Response), "exitCode") {
		t.Fatalf("postToolUse envelope = %+v, %v", env, err)
	}
	env, err = Normalize("cursor", "", fixture(t, "cursor-beforeReadFile.json"))
	if err != nil || env.Event != "PreToolUse" || env.Tool != "Read" || env.Input["file_path"] != "/repo/README.md" {
		t.Fatalf("beforeReadFile envelope = %+v, %v", env, err)
	}
	env, err = Normalize("cursor", "", fixture(t, "cursor-afterFileEdit.json"))
	if err != nil || env.Event != "PostToolUse" || env.Tool != "Edit" || env.Input["file_path"] != "/repo/README.md" {
		t.Fatalf("afterFileEdit envelope = %+v, %v", env, err)
	}
	if edits, ok := env.Input["edits"].([]any); !ok || len(edits) != 1 {
		t.Fatalf("afterFileEdit edits = %#v", env.Input["edits"])
	}
}

func TestNormalizeCursorEvents(t *testing.T) {
	const common = `"conversation_id":"c1","workspace_roots":["/ws"]`
	cases := []struct {
		name    string
		payload string
		event   string
		tool    string
		cwd     string
		check   func(t *testing.T, env gate.Envelope)
	}{
		{"sessionStart", `{"hook_event_name":"sessionStart",` + common + `,"session_id":"c1","composer_mode":"agent"}`, "SessionStart", "", "/ws", nil},
		{"sessionEnd", `{"hook_event_name":"sessionEnd",` + common + `,"reason":"completed"}`, "SessionEnd", "", "/ws", nil},
		{"beforeSubmitPrompt", `{"hook_event_name":"beforeSubmitPrompt",` + common + `,"prompt":"fix it"}`, "UserPromptSubmit", "", "/ws",
			func(t *testing.T, env gate.Envelope) {
				if env.Prompt != "fix it" {
					t.Fatalf("prompt = %q", env.Prompt)
				}
			}},
		{"preToolUse keeps a matching name", `{"hook_event_name":"preToolUse",` + common + `,"tool_name":"Write","tool_input":{"file_path":"a.go"},"cwd":"/here"}`, "PreToolUse", "Write", "/here", nil},
		{"afterShellExecution", `{"hook_event_name":"afterShellExecution",` + common + `,"command":"ls","output":"a\n"}`, "PostToolUse", "Bash", "/ws",
			func(t *testing.T, env gate.Envelope) {
				if env.Input["command"] != "ls" || string(env.Response) != `{"output":"a\n"}` {
					t.Fatalf("envelope = %+v", env)
				}
			}},
		{"beforeMCPExecution", `{"hook_event_name":"beforeMCPExecution",` + common + `,"tool_name":"search","tool_input":"{\"q\":\"x\"}","mcp_server_name":"linear"}`, "PreToolUse", "mcp__linear__search", "/ws",
			func(t *testing.T, env gate.Envelope) {
				if env.Input["q"] != "x" {
					t.Fatalf("stringified tool_input was not decoded: %+v", env.Input)
				}
			}},
		{"afterMCPExecution", `{"hook_event_name":"afterMCPExecution",` + common + `,"tool_name":"search","mcp_server_name":"linear","result_json":"{}"}`, "PostToolUse", "mcp__linear__search", "/ws", nil},
		{"stop", `{"hook_event_name":"stop",` + common + `,"status":"completed","loop_count":1}`, "Stop", "", "/ws",
			func(t *testing.T, env gate.Envelope) {
				if !env.StopActive {
					t.Fatal("loop_count 1 should read as an active stop hook")
				}
			}},
		{"subagentStart", `{"hook_event_name":"subagentStart",` + common + `,"subagent_id":"s1","subagent_type":"explore","task":"look"}`, "SubagentStart", "", "/ws",
			func(t *testing.T, env gate.Envelope) {
				if env.Agent.ID != "s1" || env.Agent.Type != "explore" {
					t.Fatalf("agent = %+v", env.Agent)
				}
			}},
		{"subagentStop", `{"hook_event_name":"subagentStop",` + common + `,"subagent_id":"s1","subagent_type":"explore","status":"completed"}`, "SubagentStop", "", "/ws", nil},
		{"preCompact", `{"hook_event_name":"preCompact",` + common + `,"trigger":"auto"}`, "PreCompact", "", "/ws",
			func(t *testing.T, env gate.Envelope) {
				if env.Source != "auto" {
					t.Fatalf("source = %q", env.Source)
				}
			}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			env, err := Normalize("cursor", "", []byte(test.payload))
			if err != nil {
				t.Fatal(err)
			}
			if env.Event != test.event || env.Tool != test.tool || env.Session != "c1" || env.Cwd != test.cwd {
				t.Fatalf("envelope = %+v", env)
			}
			if test.check != nil {
				test.check(t, env)
			}
		})
	}
}

func TestNormalizeCursorRejectsWhatItCannotPlace(t *testing.T) {
	for _, name := range []string{"afterAgentThought", "afterAgentResponse", "postToolUseFailure", "workspaceOpen", "somethingNew"} {
		_, err := Normalize("cursor", "", []byte(`{"hook_event_name":"`+name+`","conversation_id":"c1"}`))
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
	if _, err := Normalize("cursor", "", []byte(`{"conversation_id":"c1"}`)); err == nil {
		t.Fatal("a payload without an event name was accepted")
	}
	// --event names the chain in gate's vocabulary and skips the mapping.
	env, err := Normalize("cursor", "Stop", []byte(`{"hook_event_name":"afterAgentResponse","conversation_id":"c1","workspace_roots":["/ws"]}`))
	if err != nil || env.Event != "Stop" || env.Cwd != "/ws" {
		t.Fatalf("overridden envelope = %+v, %v", env, err)
	}
}
