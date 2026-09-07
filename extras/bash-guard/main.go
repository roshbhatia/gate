// gate-provider-bash-guard decides a Bash call: it denies a destructive
// command, denies a whole-file read of a large file and names the cheap
// reader, and bounds a command that prints without a limit.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/roshbhatia/gate/extras/internal/bulkread"
	"github.com/roshbhatia/gate/pkg/gate"
)

// Rule is one deny rule. The rules file is a JSON list of these; the chain
// step names it with args.rules.
type Rule struct {
	Regex  string `json:"regex"`
	Reason string `json:"reason"`
}

type compiled struct {
	pattern *regexp.Regexp
	reason  string
}

const fallbackReason = "blocked by the destructive-command guard"

func loadRules(path string) ([]compiled, error) {
	if path == "" {
		return nil, fmt.Errorf("args.rules is required; refusing to run with no deny rules")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read the deny rules at %s: %w", path, err)
	}
	var rules []Rule
	if err := json.Unmarshal(data, &rules); err != nil {
		return nil, fmt.Errorf("the deny rules at %s do not parse: %w", path, err)
	}
	if len(rules) == 0 {
		return nil, fmt.Errorf("the deny rules at %s are empty", path)
	}
	out := make([]compiled, 0, len(rules))
	for _, rule := range rules {
		pattern, err := regexp.Compile(rule.Regex)
		if err != nil {
			return nil, fmt.Errorf("deny rule %q does not compile: %w", rule.Regex, err)
		}
		out = append(out, compiled{pattern: pattern, reason: rule.Reason})
	}
	return out, nil
}

func denied(command string, rules []compiled) (string, bool) {
	for _, rule := range rules {
		if rule.pattern.MatchString(command) {
			if rule.reason == "" {
				return fallbackReason, true
			}
			return rule.reason, true
		}
	}
	return "", false
}

// budget bounds what one Bash call can print into the window.
const budget = 16 * 1024

// Any of these means the caller composed something, and appending a pipe would
// bound the last stage alone or change precedence. A quote is not here on
// purpose: `rg "a b" .` is still one simple command.
const shellOperators = "|&;<>()$`\\\n"

// Commands that print all of what they are pointed at.
var unboundedCommands = map[string]bool{
	"cat": true, "fd": true, "find": true, "grep": true, "jq": true,
	"printenv": true, "rg": true, "tree": true, "yq": true,
}

var unboundedGitSubcommands = map[string]bool{
	"blame": true, "diff": true, "log": true, "reflog": true, "shortlog": true,
}

var findWriteActions = map[string]bool{
	"-delete": true, "-exec": true, "-execdir": true, "-ok": true, "-okdir": true,
}

// Commands whose whole purpose is to print a file. `head` and `tail` are not
// here: with -n they are the targeted read the redirect asks for.
var wholeFileReaders = map[string]bool{
	"cat": true, "less": true, "more": true, "bat": true,
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func alreadyBounded(args []string) bool {
	for _, arg := range args {
		switch {
		case arg == "-n" || arg == "-m" || arg == "--max-count" || arg == "--limit":
			return true
		case strings.HasPrefix(arg, "--max-count=") || strings.HasPrefix(arg, "--limit="):
			return true
		case strings.HasPrefix(arg, "-") && isAllDigits(arg[1:]):
			return true
		case len(arg) > 2 && arg[0] == '-' && (arg[1] == 'n' || arg[1] == 'm') && isAllDigits(arg[2:]):
			return true
		}
	}
	return false
}

// simple splits one plain command, or reports that the caller composed one.
func simple(command string) (string, []string, bool) {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" || strings.ContainsAny(trimmed, shellOperators) {
		return "", nil, false
	}
	fields := strings.Fields(trimmed)
	return filepath.Base(fields[0]), fields[1:], true
}

func bound(command string) (string, bool) {
	name, args, ok := simple(command)
	if !ok {
		return "", false
	}
	switch {
	case name == "git":
		if len(args) == 0 || !unboundedGitSubcommands[args[0]] {
			return "", false
		}
	case unboundedCommands[name]:
		if len(args) == 0 {
			return "", false
		}
		if name == "find" {
			for _, arg := range args {
				if findWriteActions[arg] {
					return "", false
				}
			}
		}
	default:
		return "", false
	}
	if alreadyBounded(args) {
		return "", false
	}
	// A subshell, so pipefail does not leak into the shell the Bash tool
	// reuses across calls. `cat >/dev/null` drains the remainder so the command
	// keeps its own exit status rather than 141 from SIGPIPE.
	return fmt.Sprintf("( set -o pipefail; %s | { head -c %d; cat >/dev/null; } )", strings.TrimSpace(command), budget), true
}

// wholeFile reports whether the command prints one large file whole. A
// relative path resolves against the harness's cwd.
func wholeFile(command, cwd string, trigger int64) (string, int64, bool) {
	name, args, ok := simple(command)
	if !ok || !wholeFileReaders[name] {
		return "", 0, false
	}
	var paths []string
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			paths = append(paths, arg)
		}
	}
	if len(paths) != 1 {
		return "", 0, false
	}
	path := paths[0]
	if !filepath.IsAbs(path) && cwd != "" {
		path = filepath.Join(cwd, path)
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= trigger {
		return "", 0, false
	}
	return path, info.Size(), true
}

// Decide is the whole decision, with no harness in it.
func Decide(request gate.Request, rules []compiled) gate.Outcome {
	command, _ := request.Event.Input["command"].(string)
	if command == "" {
		return gate.PassOutcome()
	}
	if reason, stop := denied(command, rules); stop {
		return gate.Outcome{Kind: gate.Deny, Message: reason}
	}
	options := bulkread.FromRequest(request)
	if path, size, whole := wholeFile(command, request.Event.Cwd, options.TriggerBytes); whole {
		return bulkread.Redirect(options, path, size)
	}
	// A backgrounded command writes to a log file, not into the window.
	if background, ok := request.Event.Input["run_in_background"].(bool); ok && background {
		return gate.PassOutcome()
	}
	rewritten, ok := bound(command)
	if !ok {
		return gate.PassOutcome()
	}
	updated := make(map[string]any, len(request.Event.Input))
	for key, value := range request.Event.Input {
		updated[key] = value
	}
	updated["command"] = rewritten
	// The note goes in Context, not only Message: on an allow the harness
	// shows Message to the user and nothing to the model, and a model that
	// reads a cut diff without knowing it was cut reports the cut part as
	// missing from the tree.
	note := fmt.Sprintf("bash-guard: this command can print without a limit, so its output is capped at %d KiB. If the end is missing, narrow it with a filter, a path, or a flag such as -n or --max-count.", budget/1024)
	return gate.Outcome{Kind: gate.Allow, Message: note, Context: note, UpdatedInput: updated}
}

func main() {
	gate.Serve(func(request gate.Request) (gate.Outcome, error) {
		rules, err := loadRules(request.String("rules", ""))
		if err != nil {
			return gate.Outcome{}, err
		}
		return Decide(request, rules), nil
	})
}
