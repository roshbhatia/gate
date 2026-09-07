package ledger

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/roshbhatia/gate/extras/internal/state"
)

func repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(state.Env, filepath.Join(dir, ".gate"))
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nfunc A() int { return 1 }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "base")
	run("checkout", "-q", "-b", "work")
	body := "package a\n\nfunc A() int { return 2 }\n\nfunc B() int { return 3 }\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	run("commit", "-q", "-am", "change")
	return dir
}

func TestClassifyUsesSizeAndSensitivePaths(t *testing.T) {
	policy := DefaultPolicy()
	if tier, _, _ := policy.Classify(5, []string{"a.go"}); tier != "trivial" {
		t.Fatalf("small diff tier = %s", tier)
	}
	if tier, _, _ := policy.Classify(50, []string{"a.go"}); tier != "lite" {
		t.Fatalf("medium diff tier = %s", tier)
	}
	if tier, _, _ := policy.Classify(500, []string{"a.go"}); tier != "full" {
		t.Fatalf("large diff tier = %s", tier)
	}
	if tier, rule, _ := policy.Classify(2, []string{".github/workflows/ci.yml"}); tier != "full" || !strings.Contains(rule, "sensitive") {
		t.Fatalf("sensitive diff tier = %s (%s)", tier, rule)
	}
	if !globMatch("modules/nixos/**", "modules/nixos/common/default.nix") || globMatch("hosts/**", "modules/hosts.nix") {
		t.Fatal("glob matching is off")
	}
}

func TestOpenJudgeReopenAndCap(t *testing.T) {
	dir := repo(t)
	// The fixture diff is four lines, which the default policy calls trivial
	// and does not review. Drop trivial so the change earns one critic.
	policy := DefaultPolicy()
	delete(policy.Tiers, "trivial")
	record, err := Begin(dir, "demo", "", policy)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != Open || record.Tier != "lite" || record.Critics != 1 || len(record.Files) != 1 {
		t.Fatalf("opened record = %+v", record)
	}
	if _, err := Begin(dir, "again", "", policy); err == nil {
		t.Fatal("a second open over an OPEN review was accepted")
	}

	findings := []Finding{
		{ID: "f1", Verdict: Accept, Severity: Warn, Scenario: "B is dead", DisproofAttempt: "grep for callers found none",
			Evidence: []Anchor{{Path: "a.go", Line: 5}}},
		{ID: "f2", Verdict: Accept, Severity: Warn, Scenario: "no test", DisproofAttempt: "looked",
			Evidence: []Anchor{{Path: "a.go", Line: 99}}},
		{ID: "f3", Verdict: Accept, Severity: Warn, Scenario: "style", DisproofAttempt: "looked",
			Evidence: []Anchor{{Path: "missing.go", Line: 1}}},
		{ID: "f4", Verdict: Accept, Severity: Nit, Scenario: "rename", DisproofAttempt: "looked",
			Evidence: []Anchor{{Path: "a.go", Line: 1}}},
		{ID: "f5", Verdict: Reject, Scenario: "not real"},
	}
	record, err = Judge(dir, &record, findings)
	if err != nil {
		t.Fatal(err)
	}
	pass := record.Current()
	if record.State != Revise || pass.Actionable != 1 || pass.Dropped != 2 {
		t.Fatalf("judged record = %s, pass %+v", record.State, pass)
	}
	if pass.Findings[1].Verdict != Reject || !strings.Contains(pass.Findings[1].Reason, "out of range") {
		t.Fatalf("out-of-range finding = %+v", pass.Findings[1])
	}
	if pass.Findings[2].Verdict != Reject || !strings.Contains(pass.Findings[2].Reason, "does not exist") {
		t.Fatalf("missing-file finding = %+v", pass.Findings[2])
	}

	if _, err := Reopen(dir, &record); err == nil || !strings.Contains(err.Error(), "has not changed") {
		t.Fatalf("reopen on the same tree = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nfunc A() int { return 2 }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	record, err = Reopen(dir, &record)
	if err != nil || record.State != Open || record.Current().N != 2 {
		t.Fatalf("reopen = %+v, %v", record, err)
	}
	record, err = Judge(dir, &record, []Finding{{ID: "g1", Verdict: Accept, Severity: Warn, Scenario: "s", DisproofAttempt: "d",
		Evidence: []Anchor{{Path: "a.go", Line: 1}}}})
	if err != nil || record.State != Capped {
		t.Fatalf("second judged pass = %s, %v", record.State, err)
	}
	if !strings.Contains(record.Summary(), "CAPPED") || !strings.Contains(record.Markdown(), "Open findings:") {
		t.Fatalf("summary = %s\n%s", record.Summary(), record.Markdown())
	}
}

func TestJudgeHandsBackOnDeferAndBlocking(t *testing.T) {
	dir := repo(t)
	policy := DefaultPolicy()
	delete(policy.Tiers, "trivial")
	record, err := Begin(dir, "demo", "", policy)
	if err != nil {
		t.Fatal(err)
	}
	record, err = Judge(dir, &record, []Finding{{ID: "d1", Verdict: Defer, Scenario: "owner call"}})
	if err != nil || record.State != Handback {
		t.Fatalf("deferred judge = %s, %v", record.State, err)
	}
	if _, err := Judge(dir, &record, nil); err == nil {
		t.Fatal("judging a terminal review was accepted")
	}
	if err := Remove(dir); err != nil {
		t.Fatal(err)
	}
	record, err = Begin(dir, "demo", "", policy)
	if err != nil {
		t.Fatal(err)
	}
	record, err = Judge(dir, &record, nil)
	if err != nil || record.State != Clean {
		t.Fatalf("empty judge = %s, %v", record.State, err)
	}
}

func TestNotRunTier(t *testing.T) {
	dir := repo(t)
	policy := DefaultPolicy()
	record, err := Begin(dir, "demo", "", policy)
	if err != nil || record.State != NotRun || record.Critics != 0 {
		t.Fatalf("not-run record = %+v, %v", record, err)
	}
}
