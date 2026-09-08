package emit

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/roshbhatia/gate/pkg/gate"
)

func emit(t *testing.T, format Format, out gate.Outcome) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := EmitTo(&stdout, &stderr, format, "PreToolUse", out)
	return stdout.String(), stderr.String(), code
}

func TestPassIsSilentEverywhere(t *testing.T) {
	for _, format := range []Format{Claude, ExitCode, JSON, Cursor} {
		stdout, stderr, code := emit(t, format, gate.PassOutcome())
		if stdout != "" || stderr != "" || code != 0 {
			t.Fatalf("%s pass wrote stdout=%q stderr=%q code %d", format, stdout, stderr, code)
		}
	}
}

func TestClaudeCarriesContextBesideEveryDecision(t *testing.T) {
	cases := []struct {
		out  gate.Outcome
		want []string
	}{
		{gate.Outcome{Kind: gate.Deny, Message: "stop", Context: "use ask"},
			[]string{`"permissionDecision":"deny"`, `"permissionDecisionReason":"stop"`, `"additionalContext":"use ask"`}},
		{gate.Outcome{Kind: gate.Allow, Context: "capped", UpdatedInput: map[string]any{"command": "x"}},
			[]string{`"permissionDecision":"allow"`, `"additionalContext":"capped"`, `"updatedInput"`}},
		{gate.Outcome{Kind: gate.Block, Message: "retry", Context: "note"},
			[]string{`"decision":"block"`, `"reason":"retry"`, `"additionalContext":"note"`}},
		{gate.Outcome{Kind: gate.Context, Message: "lint"}, []string{`"additionalContext":"lint"`}},
	}
	for _, test := range cases {
		stdout, stderr, code := emit(t, Claude, test.out)
		if code != 0 || stderr != "" {
			t.Fatalf("%s: stderr=%q code %d", test.out.Kind, stderr, code)
		}
		for _, want := range test.want {
			if !strings.Contains(stdout, want) {
				t.Fatalf("%s output %q lacks %q", test.out.Kind, stdout, want)
			}
		}
	}
}

func TestExitCodeAndJSON(t *testing.T) {
	deny := gate.Outcome{Kind: gate.Deny, Message: "stop"}
	stdout, stderr, code := emit(t, ExitCode, deny)
	if stdout != "" || strings.TrimSpace(stderr) != "stop" || code != 2 {
		t.Fatalf("exit-code deny wrote stdout=%q stderr=%q code %d", stdout, stderr, code)
	}
	allow := gate.Outcome{Kind: gate.Allow, Context: "capped", UpdatedInput: map[string]any{"command": "x"}}
	if _, stderr, code := emit(t, ExitCode, allow); strings.TrimSpace(stderr) != "capped" || code != 0 {
		t.Fatalf("exit-code allow wrote stderr=%q code %d", stderr, code)
	}
	stdout, _, _ = emit(t, JSON, allow)
	var decoded gate.Outcome
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Kind != gate.Allow || decoded.Context != "capped" || decoded.UpdatedInput["command"] != "x" {
		t.Fatalf("json allow = %+v", decoded)
	}
	if _, err := Parse("yaml"); err == nil {
		t.Fatal("Parse accepted yaml")
	}
}
