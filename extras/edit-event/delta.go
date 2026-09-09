package main

// The delta store is a shadow git repository. Its history lives under the state
// home and its work tree is the real checkout, so the checkout's own git data is
// never touched and the checkout carries no extra marker. One agent write is one
// commit, whose subject is the prompt that asked for it, which makes `git blame`
// over the shadow repository answer "which prompt wrote this line".

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	gogit "github.com/roshbhatia/go-utils/git"
)

const (
	deltaBytes   = 4 * 1024 * 1024
	subjectRunes = 72
	lockWait     = 2 * time.Second
	lockStale    = time.Minute
)

type deltaMeta struct {
	harness string
	session string
	kind    string
	file    string
	prompt  prompt
}

// recordDelta commits one written file into the shadow repository and returns
// the commit. An empty return means the write left no delta to record.
func recordDelta(tree string, meta deltaMeta) string {
	if tree == "" || !inTree(tree, meta.file) || tooBig(meta.file) {
		return ""
	}
	dir := deltaDir(tree)
	if !ensureStore(dir, tree) {
		return ""
	}

	release, ok := lockStore(dir)
	if !ok {
		return ""
	}
	defer release()

	relative, err := filepath.Rel(resolved(tree), resolved(meta.file))
	if err != nil {
		return ""
	}
	seed(dir, tree, relative)
	if _, err := git(dir, tree, nil, "add", "--all", "--force", "--", relative); err != nil {
		return ""
	}
	message := commitMessage(relative, meta)
	if _, err := git(dir, tree, strings.NewReader(message),
		"commit", "--quiet", "--cleanup=whitespace", "--file=-"); err != nil {
		return ""
	}
	head, err := git(dir, tree, nil, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return head
}

// seed writes the checkout's HEAD content through the object database, never the
// work tree, so a first write diffs against the old file instead of against nothing.
func seed(dir, tree, relative string) {
	if _, err := git(dir, tree, nil, "ls-files", "--error-unmatch", "--", relative); err == nil {
		return
	}
	committed := exec.Command("git", "-C", tree, "cat-file", "blob", "HEAD:"+relative)
	committed.Env = gogit.CleanEnv()
	body, err := committed.Output()
	if err != nil {
		return
	}

	hash := exec.Command("git", "hash-object", "-w", "--stdin")
	hash.Dir = tree
	hash.Env = gogit.ShadowEnv(dir, tree)
	hash.Stdin = bytes.NewReader(body)
	object, err := hash.Output()
	if err != nil {
		return
	}

	mode := "100644"
	if info, err := os.Stat(filepath.Join(tree, relative)); err == nil && info.Mode()&0o111 != 0 {
		mode = "100755"
	}
	entry := strings.Join([]string{mode, strings.TrimSpace(string(object)), relative}, ",")
	if _, err := git(dir, tree, nil, "update-index", "--add", "--cacheinfo", entry); err != nil {
		return
	}
	_, _ = git(dir, tree, strings.NewReader("seed "+relative+"\n"),
		"commit", "--quiet", "--cleanup=whitespace", "--file=-")
}

func commitMessage(relative string, meta deltaMeta) string {
	subject := subjectOf(meta.prompt.Text)
	if subject == "" {
		subject = meta.kind + " " + relative
	}

	var out strings.Builder
	out.WriteString(subject)
	out.WriteString("\n\n")
	if body := strings.TrimSpace(meta.prompt.Text); body != "" && body != subject {
		out.WriteString(body)
		out.WriteString("\n\n")
	}
	for _, trailer := range [][2]string{
		{"Edit-Harness", meta.harness},
		{"Edit-Session", meta.session},
		{"Edit-Kind", meta.kind},
		{"Edit-File", relative},
	} {
		if trailer[1] == "" {
			continue
		}
		out.WriteString(trailer[0])
		out.WriteString(": ")
		out.WriteString(oneLine(trailer[1]))
		out.WriteString("\n")
	}
	return out.String()
}

func subjectOf(body string) string {
	for line := range strings.SplitSeq(body, "\n") {
		line = oneLine(line)
		if line == "" {
			continue
		}
		runes := []rune(line)
		if len(runes) > subjectRunes {
			return string(runes[:subjectRunes-1]) + "…"
		}
		return line
	}
	return ""
}

func oneLine(body string) string {
	return strings.Join(strings.Fields(body), " ")
}

// inTree compares resolved paths. The tree comes from git as a real path, the
// file from the harness payload as typed; on macOS /var is a symlink to
// /private/var, so a literal prefix test skipped every edit under it.
func inTree(tree, file string) bool {
	return strings.HasPrefix(resolved(file), strings.TrimRight(resolved(tree), "/")+"/")
}

func resolved(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	// A deleted file has no target; resolve its directory instead.
	if dir, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		return filepath.Join(dir, filepath.Base(path))
	}
	return path
}

func tooBig(file string) bool {
	info, err := os.Stat(file)
	if err != nil {
		// A delete has no file to size, and it is still worth a delta.
		return !os.IsNotExist(err)
	}
	return info.Size() > deltaBytes
}

func ensureStore(dir, tree string) bool {
	if _, err := os.Stat(filepath.Join(dir, "HEAD")); err == nil {
		return true
	}
	if os.MkdirAll(dir, 0o700) != nil {
		return false
	}
	if _, err := git(dir, tree, nil, "init", "--quiet", "--initial-branch=deltas"); err != nil {
		return false
	}
	for _, setting := range [][2]string{
		{"user.name", "edit-event"},
		{"user.email", "edit-event@gate.invalid"},
		// A hook fires this commit with no terminal, so a signing prompt would hang it.
		{"commit.gpgsign", "false"},
		{"core.bare", "false"},
		{"core.hooksPath", filepath.Join(dir, "hooks")},
	} {
		if _, err := git(dir, tree, nil, "config", setting[0], setting[1]); err != nil {
			return false
		}
	}
	return true
}

func lockStore(dir string) (func(), bool) {
	path := filepath.Join(dir, "edit-event.lock")
	deadline := time.Now().Add(lockWait)
	for {
		handle, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = handle.Close()
			return func() { _ = os.Remove(path) }, true
		}
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > lockStale {
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, false
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func git(dir, tree string, stdin *strings.Reader, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = tree
	cmd.Env = gogit.ShadowEnv(dir, tree)
	if stdin != nil {
		cmd.Stdin = stdin
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}
