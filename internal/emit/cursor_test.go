package emit

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/roshbhatia/gate/pkg/gate"
)

func emitCursor(t *testing.T, event string, out gate.Outcome) map[string]any {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := EmitTo(&stdout, &stderr, Cursor, event, out); code != 0 || stderr.Len() != 0 {
		t.Fatalf("cursor %s wrote stderr=%q code %d", out.Kind, stderr.String(), code)
	}
	var decoded map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("%s: %v in %q", out.Kind, err, stdout.String())
	}
	return decoded
}

func TestCursorSpeaksItsOwnVocabulary(t *testing.T) {
	cases := []struct {
		name  string
		event string
		out   gate.Outcome
		want  map[string]any
	}{
		{"deny", "PreToolUse", gate.Outcome{Kind: gate.Deny, Message: "stop", Context: "use ask"},
			map[string]any{"continue": true, "permission": "deny", "user_message": "stop", "agent_message": "stop\n\nuse ask"}},
		{"block on a post", "PostToolUse", gate.Outcome{Kind: gate.Block, Message: "lint failed"},
			map[string]any{"continue": true, "permission": "deny", "user_message": "lint failed", "agent_message": "lint failed"}},
		{"context", "PostToolUse", gate.Outcome{Kind: gate.Context, Message: "lint", Context: "note"},
			map[string]any{"continue": true, "agent_message": "lint\n\nnote", "additional_context": "lint\n\nnote"}},
		{"allow", "PreToolUse", gate.Outcome{Kind: gate.Allow, Message: "fine"},
			map[string]any{"continue": true, "permission": "allow"}},
		{"allow with rewrite", "PreToolUse", gate.Outcome{Kind: gate.Allow, Context: "capped", UpdatedInput: map[string]any{"command": "x | head"}},
			map[string]any{"continue": true, "permission": "allow", "agent_message": "capped", "additional_context": "capped", "updated_input": map[string]any{"command": "x | head"}}},
		{"block on a stop", "Stop", gate.Outcome{Kind: gate.Block, Message: "tests still red"},
			map[string]any{"continue": true, "followup_message": "tests still red"}},
		{"deny on a prompt", "UserPromptSubmit", gate.Outcome{Kind: gate.Deny, Message: "no"},
			map[string]any{"continue": false, "user_message": "no", "agent_message": "no"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := emitCursor(t, test.event, test.out)
			want, _ := json.Marshal(test.want)
			have, _ := json.Marshal(got)
			if string(want) != string(have) {
				t.Fatalf("got %s\nwant %s", have, want)
			}
		})
	}
	if _, err := Parse("cursor"); err != nil {
		t.Fatal(err)
	}
}
