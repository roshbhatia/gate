package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/roshbhatia/gate/pkg/gate"
)

func testConfig(t *testing.T) config {
	t.Helper()
	return config{
		stateDir:       t.TempDir(),
		maxTells:       defaultMaxTells,
		maxReportBytes: defaultMaxReportKiB * 1024,
		reminder:       "The house style is active.",
		shape:          "Send it again in the house shape.",
	}
}

func prompt(session, text string) gate.Envelope {
	return gate.Envelope{Event: "UserPromptSubmit", Session: session, Prompt: text}
}

func TestReminderArmsOnce(t *testing.T) {
	cfg := testConfig(t)
	const session = "session-1"
	if outcome := cfg.remind(prompt(session, "")); outcome.Kind != gate.Context || outcome.Message != cfg.reminder {
		t.Fatalf("first reminder = %+v", outcome)
	}
	if outcome := cfg.remind(prompt(session, "")); outcome.Kind != gate.Pass {
		t.Fatalf("second reminder = %+v", outcome)
	}
	cfg.arm(session)
	if outcome := cfg.remind(prompt(session, "")); outcome.Kind != gate.Context {
		t.Fatalf("armed reminder = %+v", outcome)
	}
}

func TestReminderWithNoTextPassesUntilFindingsAreHeld(t *testing.T) {
	cfg := testConfig(t)
	cfg.reminder = ""
	const session = "session-4"
	if outcome := cfg.remind(prompt(session, "")); outcome.Kind != gate.Pass {
		t.Fatalf("empty reminder = %+v", outcome)
	}
	cfg.record(session, "held")
	cfg.arm(session)
	if outcome := cfg.remind(prompt(session, "")); outcome.Kind != gate.Context || outcome.Message != "held" {
		t.Fatalf("held findings with no reminder = %+v", outcome)
	}
}

func TestNoterseExemptsOneTurn(t *testing.T) {
	cfg := testConfig(t)
	const session = "session-2"
	if outcome := cfg.remind(prompt(session, "explain this NOTERSE")); outcome.Kind != gate.Pass {
		t.Fatalf("escape reminder = %+v", outcome)
	}
	if !cfg.release(session) || cfg.release(session) {
		t.Fatal("escape did not last exactly one turn")
	}
	if cfg.armPath("../escape") != "" {
		t.Fatal("session path accepted a separator")
	}
}

func TestApplyLocateAndSplice(t *testing.T) {
	replaced, ok := apply(" — ", valeAlertAction{Name: "replace", Params: []string{", "}})
	if !ok || replaced != ", " {
		t.Fatalf("replacement = %q, %v", replaced, ok)
	}
	line := []rune("path and target")
	alert := fileAlert{valeAlert: valeAlert{Match: "target"}, Span: []int{99, 120}}
	start, end, ok := locate(line, alert)
	if !ok || string(line[start-1:end]) != "target" {
		t.Fatalf("located %d:%d, %v", start, end, ok)
	}
	spliced, _ := splice([]rune("schema.yaml —  name"), 13, 14, ": ")
	if string(spliced) != "schema.yaml: name" {
		t.Fatalf("spliced line = %q", string(spliced))
	}
}

func TestMarkdownSkipsHiddenDirectories(t *testing.T) {
	root := t.TempDir()
	visible := filepath.Join(root, "README.md")
	hidden := filepath.Join(root, ".cache", "ignored.md")
	if err := os.MkdirAll(filepath.Dir(hidden), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{visible, hidden} {
		if err := os.WriteFile(path, []byte("text\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	files, err := markdown([]string{root})
	if err != nil {
		t.Fatalf("markdown: %v", err)
	}
	if len(files) != 1 || files[0] != visible {
		t.Fatalf("markdown files = %v", files)
	}
}

// The rule set is the caller's. This runs only where a style is named, so the
// Nix check phase, which has no vale, skips it.
func TestFindingsUsesConfiguredStyle(t *testing.T) {
	cfg := testConfig(t)
	cfg.style = os.Getenv(styleEnv)
	if cfg.style == "" {
		t.Skipf("%s is unset", styleEnv)
	}
	got := cfg.findings("This seamlessly leverages one pivotal unlock.\n")
	if len(got) == 0 {
		t.Fatal("configured style reported no findings")
	}
	for _, finding := range got {
		if strings.TrimSpace(finding.Check) == "" {
			t.Fatalf("finding has no rule: %+v", finding)
		}
	}
}

func TestBlocksNamedRuleOnFirstAlert(t *testing.T) {
	cfg := testConfig(t)
	cfg.blockOn = []string{"Style.Fingerprint"}
	if !cfg.blocks([]valeAlert{{Check: "Style.Fingerprint"}}) {
		t.Fatal("a block_on rule did not record on its first alert")
	}
	if cfg.blocks([]valeAlert{{Check: "Style.Marketing"}}) {
		t.Fatal("ordinary style alert recorded before the threshold")
	}
	if !cfg.blocks([]valeAlert{{Check: "Style.Marketing"}, {Check: "Style.Filler"}}) {
		t.Fatal("two ordinary style alerts did not record")
	}
	cfg.maxTells = 3
	if cfg.blocks([]valeAlert{{Check: "Style.Marketing"}, {Check: "Style.Filler"}}) {
		t.Fatal("max_tells was not read")
	}
}

func TestReasonEndsWithTheShape(t *testing.T) {
	cfg := testConfig(t)
	text := cfg.reason(nil, []valeAlert{{Check: "Style.Filler", Message: "Drop the opener", Match: "Basically"}})
	if !strings.Contains(text, `Drop the opener: "Basically"`) || !strings.HasSuffix(text, cfg.shape) {
		t.Fatalf("reason = %q", text)
	}
	cfg.shape = ""
	if text := cfg.reason(nil, nil); strings.TrimSpace(text) != "Your last reply read like agent prose. It was sent as written." {
		t.Fatalf("reason with no shape = %q", text)
	}
}

func TestRemindCarriesRecordedFindingsOnce(t *testing.T) {
	cfg := testConfig(t)
	const session = "session-3"
	if outcome := cfg.remind(prompt(session, "")); outcome.Kind != gate.Context {
		t.Fatalf("first reminder = %+v", outcome)
	}
	cfg.record(session, "Your last reply read like agent prose.")
	cfg.arm(session)
	outcome := cfg.remind(prompt(session, ""))
	if outcome.Kind != gate.Context || !strings.Contains(outcome.Message, "Your last reply read like agent prose.") ||
		!strings.HasSuffix(outcome.Message, cfg.reminder) {
		t.Fatalf("reminder after findings = %+v", outcome)
	}
	if outcome := cfg.remind(prompt(session, "")); outcome.Kind != gate.Pass {
		t.Fatalf("findings were carried twice: %+v", outcome)
	}
}

func agentReport(t *testing.T, cfg config, status string, size int) gate.Outcome {
	t.Helper()
	response, err := json.Marshal(map[string]any{
		"status":  status,
		"content": []map[string]string{{"type": "text", "text": strings.Repeat("x", size)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return cfg.report(gate.Envelope{Event: "PostToolUse", Tool: "Agent", Response: response})
}

func TestReportNotesOnlyOversizedCompletedReports(t *testing.T) {
	cfg := testConfig(t)
	if outcome := agentReport(t, cfg, "completed", cfg.maxReportBytes+1); outcome.Kind != gate.Context ||
		!strings.Contains(outcome.Message, "file:line") {
		t.Fatalf("oversized report = %+v", outcome)
	}
	if outcome := agentReport(t, cfg, "completed", cfg.maxReportBytes); outcome.Kind != gate.Pass {
		t.Fatalf("report at the budget = %+v", outcome)
	}
	if outcome := agentReport(t, cfg, "async_launched", cfg.maxReportBytes+1); outcome.Kind != gate.Pass {
		t.Fatalf("background launch = %+v", outcome)
	}
	if outcome := cfg.report(gate.Envelope{Event: "PostToolUse", Tool: "Agent"}); outcome.Kind != gate.Pass {
		t.Fatalf("report with no response = %+v", outcome)
	}
}

func TestDecideRoutesOnTheEventAndReadsArgs(t *testing.T) {
	t.Setenv(stateEnv, t.TempDir())
	start := gate.Request{Event: gate.Envelope{Event: "SessionStart"}, Args: map[string]any{"session": "Spend context on purpose."}}
	if outcome := Decide(start); outcome.Kind != gate.Context || outcome.Message != "Spend context on purpose." {
		t.Fatalf("session start = %+v", outcome)
	}
	if outcome := Decide(gate.Request{Event: gate.Envelope{Event: "SessionStart"}}); outcome.Kind != gate.Pass {
		t.Fatalf("session start with no text = %+v", outcome)
	}
	explicit := gate.Request{Event: gate.Envelope{Event: "Stop"}, Args: map[string]any{"mode": "session", "session": "again"}}
	if outcome := Decide(explicit); outcome.Kind != gate.Context || outcome.Message != "again" {
		t.Fatalf("explicit mode = %+v", outcome)
	}
	report := gate.Request{
		Event: gate.Envelope{Event: "PostToolUse", Tool: "Agent", Response: json.RawMessage(`{"status":"completed","content":[{"type":"text","text":"xxxxxxxxxxxxxxxxxxxxxxxx"}]}`)},
		Args:  map[string]any{"max_report_kib": float64(0)},
	}
	if outcome := Decide(report); outcome.Kind != gate.Context {
		t.Fatalf("report over a zero budget = %+v", outcome)
	}
	// A Stop with no style records nothing: the check cannot run, so the next
	// prompt carries no findings.
	stop := gate.Request{Event: gate.Envelope{Event: "Stop", Session: "s", LastMessage: "Basically, this leverages synergy."}}
	if outcome := Decide(stop); outcome.Kind != gate.Pass {
		t.Fatalf("stop with no style = %+v", outcome)
	}
	if entries, _ := os.ReadDir(os.Getenv(stateEnv)); len(entries) != 0 {
		t.Fatalf("stop with no style wrote state: %v", entries)
	}
	t.Setenv(offEnv, "off")
	if outcome := Decide(start); outcome.Kind != gate.Pass {
		t.Fatalf("switched off = %+v", outcome)
	}
}

func TestCLIConfigReadsFlags(t *testing.T) {
	t.Setenv(styleEnv, "")
	if _, _, err := cliConfig("lint", nil); err == nil {
		t.Fatal("no style was accepted")
	}
	cfg, rest, err := cliConfig("lint", []string{"--style", "/s/vale.ini", "--block-on", "A.B", "--max-tells", "2", "docs"})
	if err != nil || cfg.style != "/s/vale.ini" || cfg.maxTells != 2 || len(cfg.blockOn) != 1 || len(rest) != 1 {
		t.Fatalf("cliConfig = %+v, %v, %v", cfg, rest, err)
	}
	if _, _, err := cliConfig("lint", []string{"--style", "/s", "--max-tells", "many"}); err == nil {
		t.Fatal("a non-integer --max-tells was accepted")
	}
}
