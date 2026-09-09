package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/roshbhatia/gate/pkg/gate"
)

// newRepo isolates the state home and returns a git checkout with one commit.
// The path is symlink-resolved, because git answers `--show-toplevel`
// physically and the workspace key is the root's exact bytes.
func newRepo(t *testing.T) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", filepath.Join(base, "home"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(base, "state"))
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	work := filepath.Join(base, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, work, "init", "--quiet", "--initial-branch=main")
	write(t, filepath.Join(work, "a.go"), "old\n")
	run(t, work, "add", "a.go")
	run(t, work, "commit", "--quiet", "-m", "base")
	return work
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{
		"-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false",
	}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func edit(harness, tool, cwd string, input map[string]any) gate.Request {
	return gate.Request{Event: gate.Envelope{
		Version: gate.Version, Harness: harness, Event: "PostToolUse", Tool: tool,
		Input: input, Cwd: cwd, Session: "s1",
	}}
}

func submit(harness, cwd, text string) gate.Request {
	return gate.Request{Event: gate.Envelope{
		Version: gate.Version, Harness: harness, Event: "UserPromptSubmit",
		Cwd: cwd, Session: "s1", Prompt: text,
	}}
}

func decide(t *testing.T, request gate.Request) gate.Outcome {
	t.Helper()
	out, err := Decide(request)
	if err != nil {
		t.Fatalf("Decide(%s %s) = %v", request.Event.Event, request.Event.Tool, err)
	}
	if out.Kind != gate.Pass {
		t.Fatalf("decision = %q, want pass", out.Kind)
	}
	return out
}

func readLog(t *testing.T, work string) []record {
	t.Helper()
	body, err := os.ReadFile(logFile(work))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	var lines []record
	for line := range strings.SplitSeq(strings.TrimRight(string(body), "\n"), "\n") {
		if line == "" {
			continue
		}
		var parsed record
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			t.Fatalf("line is not one JSON object: %q: %v", line, err)
		}
		lines = append(lines, parsed)
	}
	return lines
}

func TestRecordNamesTheFileAndTheToolPerHarness(t *testing.T) {
	patch := strings.Join([]string{
		"*** Begin Patch",
		"*** Add File: created.go",
		"+package main",
		"*** Update File: changed.go",
		"@@",
		"-old",
		"+new",
		"*** Delete File: gone.go",
		"*** End Patch",
	}, "\n")
	for _, tc := range []struct {
		name    string
		harness string
		tool    string
		input   map[string]any
		want    map[string]string // base name -> kind
	}{
		{"claude Edit", "claude", "Edit", map[string]any{"file_path": "a.go", "old_string": "x"}, map[string]string{"a.go": "edit"}},
		{"claude Write", "claude", "Write", map[string]any{"file_path": "b.go", "content": "x"}, map[string]string{"b.go": "write"}},
		{"claude MultiEdit", "claude", "MultiEdit", map[string]any{"file_path": "c.go"}, map[string]string{"c.go": "multiedit"}},
		{"claude NotebookEdit", "claude", "NotebookEdit", map[string]any{"notebook_path": "n.ipynb"}, map[string]string{"n.ipynb": "notebookedit"}},
		// gate's cursor normalizer already lifts afterFileEdit's top-level
		// file_path into Input, so cursor reads exactly like claude here.
		{"cursor Edit", "cursor", "Edit", map[string]any{"file_path": "d.ts", "edits": []any{}}, map[string]string{"d.ts": "edit"}},
		{"codex apply_patch", "codex", "apply_patch", map[string]any{"command": patch},
			map[string]string{"created.go": "write", "changed.go": "edit", "gone.go": "delete"}},
		{"codex shell without an envelope", "codex", "shell", map[string]any{"command": "sed -i s/a/b/ target.go"}, nil},
		{"edit tool without a path", "claude", "Edit", map[string]any{}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			work := newRepo(t)
			decide(t, edit(tc.harness, tc.tool, work, tc.input))

			if tc.want == nil {
				if _, err := os.Stat(logFile(work)); !os.IsNotExist(err) {
					t.Fatalf("nothing was written, so the log should not exist: %v", err)
				}
				return
			}
			got := map[string]string{}
			for _, line := range readLog(t, work) {
				if !filepath.IsAbs(line.File) || !strings.HasPrefix(line.File, work+"/") {
					t.Errorf("file = %q, want an absolute path under %s", line.File, work)
				}
				if line.Harness != tc.harness || line.Session != "s1" || line.CWD != work {
					t.Errorf("line = %+v, want harness %s, session s1, cwd %s", line, tc.harness, work)
				}
				if line.Version != SchemaVersion || line.TS <= 0 {
					t.Errorf("line = %+v, want version %d and a positive ts", line, SchemaVersion)
				}
				got[filepath.Base(line.File)] = line.Kind
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("recorded %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLineCarriesNoFileContents(t *testing.T) {
	work := newRepo(t)
	write(t, filepath.Join(work, "secret.txt"), "BODY-SENTINEL")
	decide(t, edit("claude", "Write", work, map[string]any{"file_path": "secret.txt", "content": "BODY-SENTINEL"}))

	body, err := os.ReadFile(logFile(work))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "BODY-SENTINEL") {
		t.Fatal("the log holds the file's contents; it must hold only its path")
	}
}

func TestOtherEventsAndAnEmptyPromptPass(t *testing.T) {
	work := newRepo(t)
	decide(t, gate.Request{Event: gate.Envelope{Event: "Stop", Harness: "claude", Cwd: work}})
	decide(t, submit("claude", work, "   "))
	if _, err := os.Stat(promptFile(work)); !os.IsNotExist(err) {
		t.Fatalf("a blank prompt was saved: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(logFile(work))); !os.IsNotExist(err) {
		t.Fatalf("a Stop created the edits directory: %v", err)
	}
}

func TestUnwritableLogIsAnErrorAndStillAPass(t *testing.T) {
	work := newRepo(t)
	edits := filepath.Dir(logFile(work))
	if err := os.MkdirAll(filepath.Dir(edits), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, edits, "not a directory")

	out, err := Decide(edit("claude", "Edit", work, map[string]any{"file_path": "a.go"}))
	if err == nil {
		t.Fatal("a log that cannot be written read as fine")
	}
	if out.Kind != gate.Pass {
		t.Fatalf("decision = %q; a lost record must never turn into a deny", out.Kind)
	}
}

func TestTrimKeepsTheNewestLines(t *testing.T) {
	work := newRepo(t)
	log := logFile(work)
	if err := os.MkdirAll(filepath.Dir(log), 0o700); err != nil {
		t.Fatal(err)
	}
	filler := strings.Repeat("x", 200)
	var body strings.Builder
	for i := 0; body.Len() <= maxBytes; i++ {
		body.WriteString(`{"version":1,"file":"` + filler + `","n":` + string(rune('0'+i%10)) + "}\n")
	}
	write(t, log, body.String())

	decide(t, edit("claude", "Edit", work, map[string]any{"file_path": "a.go"}))

	lines := readLog(t, work)
	if len(lines) != keepLines+1 {
		t.Fatalf("kept %d lines, want %d", len(lines), keepLines+1)
	}
	if !strings.HasSuffix(lines[len(lines)-1].File, "/a.go") {
		t.Fatalf("the newest line is %+v, want this write", lines[len(lines)-1])
	}
}

func TestConcurrentWritesProduceIntactLines(t *testing.T) {
	work := newRepo(t)
	const writers = 16
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := "f" + string(rune('a'+i)) + ".go"
			// An absolute path outside the tree skips the shadow commit, so
			// this exercises the append alone.
			if _, err := Decide(edit("claude", "Edit", work, map[string]any{"file_path": filepath.Join(t.TempDir(), name)})); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := len(readLog(t, work)); got != writers {
		t.Fatalf("read %d lines, want %d", got, writers)
	}
}
