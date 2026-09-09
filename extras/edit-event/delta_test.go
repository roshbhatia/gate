package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gogit "github.com/roshbhatia/go-utils/git"
)

func shadow(t *testing.T, work string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = work
	cmd.Env = gogit.ShadowEnv(deltaDir(work), work)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("shadow git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestShadowCommitSubjectIsThePrompt(t *testing.T) {
	long := strings.Repeat("word ", 40)
	for _, tc := range []struct {
		name        string
		asked       string // harness that submitted the prompt; "" submits none
		prompt      string
		wantSubject string
		wantBody    string
	}{
		{"first line of the prompt", "claude", "Fix the off-by-one\n\nIt drops the last row.", "Fix the off-by-one", "It drops the last row."},
		{"one line prompt has no body", "claude", "  rename   the flag  ", "rename the flag", ""},
		{"long line is clipped", "claude", long, string([]rune(strings.TrimSpace(long))[:subjectRunes-1]) + "…", strings.TrimSpace(long)},
		{"no prompt names the write", "", "", "edit a.go", ""},
		{"another harness's prompt is not borrowed", "codex", "codex asked for this", "edit a.go", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			work := newRepo(t)
			if tc.asked != "" {
				decide(t, submit(tc.asked, work, tc.prompt))
			}
			write(t, filepath.Join(work, "a.go"), "new\n")
			decide(t, edit("claude", "Edit", work, map[string]any{"file_path": "a.go"}))

			lines := readLog(t, work)
			if len(lines) != 1 || lines[0].Delta == "" {
				t.Fatalf("log = %+v, want one line naming its delta", lines)
			}
			if got := shadow(t, work, "log", "-1", "--format=%s"); got != tc.wantSubject {
				t.Fatalf("subject = %q, want %q", got, tc.wantSubject)
			}
			if got := shadow(t, work, "rev-parse", "HEAD"); got != lines[0].Delta {
				t.Fatalf("log names delta %s, shadow HEAD is %s", lines[0].Delta, got)
			}
			message := shadow(t, work, "log", "-1", "--format=%B")
			if tc.wantBody != "" && !strings.Contains(message, "\n\n"+tc.wantBody+"\n") {
				t.Fatalf("message lacks the prompt body:\n%s", message)
			}
			for _, trailer := range []string{"Edit-Harness: claude", "Edit-Session: s1", "Edit-Kind: edit", "Edit-File: a.go"} {
				if !strings.Contains(message, trailer) {
					t.Fatalf("message lacks %q:\n%s", trailer, message)
				}
			}
			if tc.asked == "codex" && strings.Contains(message, "codex asked") {
				t.Fatalf("a prompt from another harness was attributed to this write:\n%s", message)
			}
		})
	}
}

func TestFirstDeltaDiffsAgainstTheCheckoutHead(t *testing.T) {
	work := newRepo(t)
	write(t, filepath.Join(work, "a.go"), "new\n")
	decide(t, edit("claude", "Edit", work, map[string]any{"file_path": "a.go"}))

	if got := shadow(t, work, "rev-list", "--count", "HEAD"); got != "2" {
		t.Fatalf("shadow history has %s commits, want the seed and the delta", got)
	}
	patch := shadow(t, work, "show", "--format=", "HEAD")
	if !strings.Contains(patch, "-old") || !strings.Contains(patch, "+new") {
		t.Fatalf("the delta does not read old -> new:\n%s", patch)
	}
	if got := shadow(t, work, "log", "--format=%s", "HEAD~1"); got != "seed a.go" {
		t.Fatalf("parent subject = %q, want the seed", got)
	}
	// The checkout's own history is untouched.
	if got := run(t, work, "rev-list", "--count", "HEAD"); got != "1" {
		t.Fatalf("checkout history has %s commits, want 1", got)
	}
	if _, err := os.Stat(filepath.Join(work, ".git", "refs", "heads", "deltas")); !os.IsNotExist(err) {
		t.Fatal("the deltas branch landed in the checkout's own repository")
	}
}

func TestEveryWriteIsOneCommit(t *testing.T) {
	work := newRepo(t)
	decide(t, submit("claude", work, "first"))
	write(t, filepath.Join(work, "a.go"), "one\n")
	decide(t, edit("claude", "Edit", work, map[string]any{"file_path": "a.go"}))
	decide(t, submit("claude", work, "second"))
	write(t, filepath.Join(work, "b.go"), "two\n")
	decide(t, edit("claude", "Write", work, map[string]any{"file_path": "b.go"}))

	got := shadow(t, work, "log", "--format=%s")
	want := "second\nfirst\nseed a.go"
	if got != want {
		t.Fatalf("subjects:\n%s\nwant:\n%s", got, want)
	}
	blame := shadow(t, work, "blame", "--porcelain", "-L", "1,1", "--", "b.go")
	if !strings.HasPrefix(blame, shadow(t, work, "rev-parse", "HEAD")) {
		t.Fatalf("blame does not name the delta that wrote the line:\n%s", blame)
	}
}

func TestAWriteOutsideTheTreeOrTooBigLeavesNoDelta(t *testing.T) {
	work := newRepo(t)
	outside := filepath.Join(filepath.Dir(work), "outside.go")
	write(t, outside, "x\n")
	decide(t, edit("claude", "Edit", work, map[string]any{"file_path": outside}))

	big := filepath.Join(work, "big.bin")
	write(t, big, strings.Repeat("x", deltaBytes+1))
	decide(t, edit("claude", "Write", work, map[string]any{"file_path": "big.bin"}))

	for _, line := range readLog(t, work) {
		if line.Delta != "" {
			t.Fatalf("line %+v names a delta", line)
		}
	}
	if _, err := os.Stat(deltaDir(work)); !os.IsNotExist(err) {
		t.Fatalf("a shadow repository was created for nothing: %v", err)
	}
}

func TestDeleteIsADelta(t *testing.T) {
	work := newRepo(t)
	if err := os.Remove(filepath.Join(work, "a.go")); err != nil {
		t.Fatal(err)
	}
	decide(t, edit("codex", "apply_patch", work, map[string]any{"command": "*** Begin Patch\n*** Delete File: a.go\n*** End Patch"}))

	lines := readLog(t, work)
	if len(lines) != 1 || lines[0].Kind != "delete" || lines[0].Delta == "" {
		t.Fatalf("log = %+v, want one delete with a delta", lines)
	}
	if got := shadow(t, work, "show", "--format=", "--name-status", "HEAD"); !strings.HasPrefix(got, "D\ta.go") {
		t.Fatalf("the delta is not a deletion:\n%s", got)
	}
}
