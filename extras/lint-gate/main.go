// gate-provider-lint-gate runs the edited file's own checker after an edit
// and hands the failures back as context.
//
// A checker only speaks when it exits non-zero, so the gate keeps the 100%
// precision that SWE-agent (Yang et al., NeurIPS 2024) holds its guardrails
// to. Feeding static analysis back each round cut security findings from over
// 40% to 13% in arXiv 2508.14419.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/roshbhatia/gate/pkg/gate"
)

const (
	patience = 10 * time.Second
	widest   = 4 * 1024
)

type checker struct {
	binary string
	args   []string
	// byOutput marks a checker that reports on stdout and still exits 0.
	byOutput bool
}

var byExtension = map[string][]checker{
	".nix":  {{binary: "statix", args: []string{"check"}}, {binary: "deadnix", args: []string{"--fail"}}},
	".lua":  {{binary: "stylua", args: []string{"--check"}}},
	".sh":   {{binary: "shellcheck", args: []string{"--shell=bash"}}},
	".bash": {{binary: "shellcheck", args: []string{"--shell=bash"}}},
	".go":   {{binary: "gofmt", args: []string{"-l"}, byOutput: true}},
	".py":   {{binary: "ruff", args: []string{"check"}}},
	".toml": {{binary: "taplo", args: []string{"lint"}}},
}

// A checker writes for a terminal, and the escape codes only cost the model tokens.
var colour = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// Inspect runs every checker registered for the path's extension.
func Inspect(path string) gate.Outcome {
	if path == "" {
		return gate.PassOutcome()
	}
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return gate.PassOutcome()
	}
	var found []string
	for _, one := range byExtension[strings.ToLower(filepath.Ext(path))] {
		if report := check(one, path); report != "" {
			found = append(found, report)
		}
	}
	if len(found) == 0 {
		return gate.PassOutcome()
	}
	return gate.Outcome{
		Kind: gate.Context,
		Message: fmt.Sprintf("lint-gate: %s does not pass its own checker. Fix this before you move on.\n\n%s",
			filepath.Base(path), clip(strings.Join(found, "\n\n"))),
	}
}

func check(one checker, path string) string {
	binary, err := exec.LookPath(one.binary)
	if err != nil {
		return ""
	}
	ctx, stop := context.WithTimeout(context.Background(), patience)
	defer stop()
	run := exec.CommandContext(ctx, binary, append(append([]string{}, one.args...), path)...)
	run.Dir = filepath.Dir(path)
	out, err := run.CombinedOutput()
	if ctx.Err() != nil {
		return ""
	}
	said := strings.TrimSpace(colour.ReplaceAllString(string(out), ""))
	if one.byOutput {
		if said == "" {
			return ""
		}
		return one.binary + " reports " + said
	}
	if err == nil || said == "" {
		return ""
	}
	return "$ " + one.binary + " " + strings.Join(one.args, " ") + " " + filepath.Base(path) + "\n" + said
}

func clip(text string) string {
	if len(text) <= widest {
		return text
	}
	return text[:widest] + "\n[the rest is cut]"
}

func main() {
	gate.Serve(func(request gate.Request) (gate.Outcome, error) {
		path, _ := request.Event.Input["file_path"].(string)
		if path != "" && !filepath.IsAbs(path) && request.Event.Cwd != "" {
			path = filepath.Join(request.Event.Cwd, path)
		}
		return Inspect(path), nil
	})
}
