package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/roshbhatia/gate/pkg/gate"
	"github.com/roshbhatia/go-utils/paths"
	"github.com/roshbhatia/go-utils/store"
	"github.com/roshbhatia/go-utils/workspace"
)

// SchemaVersion is the `version` field of every log line.
const SchemaVersion = 1

const (
	maxBytes    = 512 * 1024
	keepLines   = 200
	promptBytes = 8 * 1024
)

// record is one line of the edit log. It names the file and never carries its
// contents; the shadow repository holds those.
type record struct {
	Version int    `json:"version"`
	TS      int64  `json:"ts"`
	Harness string `json:"harness"`
	Kind    string `json:"kind"`
	File    string `json:"file"`
	CWD     string `json:"cwd"`
	Session string `json:"session,omitempty"`
	Delta   string `json:"delta,omitempty"`
}

// prompt is the last UserPromptSubmit for a workspace, kept until the next one.
type prompt struct {
	TS      int64  `json:"ts"`
	Harness string `json:"harness"`
	Session string `json:"session"`
	Text    string `json:"text"`
}

// The three files of a workspace share one keyed stem, so a reader that knows
// the manifest entry and the workspace root can name all of them.
func logFile(root string) string    { return paths.Keyed(paths.AgentEdits(), root) + ".jsonl" }
func deltaDir(root string) string   { return paths.Keyed(paths.AgentEdits(), root) + ".delta" }
func promptFile(root string) string { return paths.Keyed(paths.AgentEdits(), root) + ".prompt" }

// Decide records the event and answers pass. An error is what the chain logs
// and moves past; it never turns into a deny, because the edit is already on
// disk and refusing to record it would lose the record and keep the edit.
func Decide(request gate.Request) (gate.Outcome, error) {
	env := request.Event
	dir := env.Cwd
	if dir == "" {
		dir = workingDir()
	}
	switch env.Event {
	case "UserPromptSubmit":
		return gate.PassOutcome(), savePrompt(workspace.Root(dir), env.Harness, env.Session, env.Prompt)
	case "PostToolUse":
		return gate.PassOutcome(), recordEdit(env, dir)
	}
	return gate.PassOutcome(), nil
}

type change struct {
	file string
	kind string
}

// changesOf names the files an edit tool wrote. An edit tool names one file in
// its input; codex's apply_patch names each file in the patch envelope with
// its own verb.
func changesOf(input map[string]any) []change {
	if file := text(input, "file_path", "notebook_path"); file != "" {
		return []change{{file: file}}
	}
	return applyPatchChanges(text(input, "command", "patchText", "patch_text", "input", "patch"))
}

func applyPatchChanges(envelope string) []change {
	if envelope == "" {
		return nil
	}
	verbs := map[string]string{
		"*** Add File: ":    "write",
		"*** Update File: ": "edit",
		"*** Delete File: ": "delete",
	}
	var out []change
	for line := range strings.SplitSeq(envelope, "\n") {
		line = strings.TrimRight(line, "\r")
		for marker, kind := range verbs {
			path, found := strings.CutPrefix(line, marker)
			if !found {
				continue
			}
			if path = strings.TrimSpace(path); path != "" {
				out = append(out, change{file: path, kind: kind})
			}
			break
		}
	}
	return out
}

func recordEdit(env gate.Envelope, dir string) error {
	changes := changesOf(env.Input)
	if len(changes) == 0 {
		return nil
	}
	kind := strings.ToLower(env.Tool)
	if kind == "" {
		kind = "edit"
	}

	tree := workspace.Root(dir)
	log := logFile(tree)
	if err := os.MkdirAll(filepath.Dir(log), 0o700); err != nil {
		return fmt.Errorf("edit log: %w", err)
	}
	trim(log)

	asked := loadPrompt(tree)
	if asked.Harness != env.Harness {
		// Another harness asked for that one. Attributing this write to it would lie.
		asked = prompt{}
	}
	now := time.Now().UnixMilli()
	var failed []error
	for _, c := range changes {
		absolute := absoluteIn(dir, c.file)
		fileKind := kind
		if c.kind != "" {
			fileKind = c.kind
		}
		err := appendLine(log, record{
			Version: SchemaVersion,
			TS:      now,
			Harness: env.Harness,
			Kind:    fileKind,
			File:    absolute,
			CWD:     dir,
			Session: env.Session,
			Delta: recordDelta(tree, deltaMeta{
				harness: env.Harness,
				session: env.Session,
				kind:    fileKind,
				file:    absolute,
				prompt:  asked,
			}),
		})
		if err != nil {
			failed = append(failed, fmt.Errorf("edit log %s: %w", absolute, err))
		}
	}
	return errors.Join(failed...)
}

func savePrompt(tree, harness, session, body string) error {
	body = strings.TrimSpace(body)
	if body == "" || tree == "" {
		return nil
	}
	if len(body) > promptBytes {
		body = body[:promptBytes]
	}
	encoded, err := json.Marshal(prompt{
		TS:      time.Now().UnixMilli(),
		Harness: harness,
		Session: session,
		Text:    body,
	})
	if err != nil {
		return err
	}
	path := promptFile(tree)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("prompt: %w", err)
	}
	if err := store.Write(path, append(encoded, '\n')); err != nil {
		return fmt.Errorf("prompt: %w", err)
	}
	return nil
}

func loadPrompt(tree string) prompt {
	var saved prompt
	body, err := os.ReadFile(promptFile(tree))
	if err != nil {
		return saved
	}
	if json.Unmarshal(body, &saved) != nil {
		return prompt{}
	}
	return saved
}

func appendLine(log string, line record) error {
	encoded, err := json.Marshal(line)
	if err != nil {
		return err
	}
	handle, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = handle.Close() }()
	_, err = handle.Write(append(encoded, '\n'))
	return err
}

// trim keeps the log bounded. The newest lines survive; a reader tails the
// file, so what it lost is history it had already seen.
func trim(log string) {
	info, err := os.Stat(log)
	if err != nil || info.Size() <= maxBytes {
		return
	}
	body, err := os.ReadFile(log)
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	if len(lines) <= keepLines {
		return
	}
	kept := strings.Join(lines[len(lines)-keepLines:], "\n") + "\n"
	_ = store.Write(log, []byte(kept))
}

func absoluteIn(dir, file string) string {
	if filepath.IsAbs(file) {
		return filepath.Clean(file)
	}
	return filepath.Join(dir, file)
}

func workingDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return os.Getenv("PWD")
	}
	return dir
}

// text returns the first of keys whose value is a non-empty string.
func text(input map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := input[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}
