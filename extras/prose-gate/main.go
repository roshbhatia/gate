// gate-provider-prose-gate records the style tells of a reply and carries
// them into the next prompt.
//
// The rule set is vale's, named by the chain step's args.style, so this
// binary holds the mechanism and none of the rules: which style, which rule
// blocks on one hit, and what the model is told to do about it all come from
// the step. Every text it injects is an argument too.
//
//   - Stop (`check`): lint the reply, record the tells and the corrected
//     lines. It never sends the reply back: a Stop hook has no passive
//     channel, so a note there is a forced extra turn.
//   - UserPromptSubmit (`remind`): inject args.reminder on the first prompt
//     of a session, and again, with the recorded tells, on the prompt after
//     check found some. A prompt ending in "noterse" skips the reminder and
//     the next check.
//   - SessionStart (`session`): inject args.session, which a fresh or
//     compacted session has just lost.
//   - PostToolUse on Agent (`report`): note a teammate report that returned
//     the material instead of the conclusion, to the caller.
//
// The same binary is the CLI: `prose-gate lint` reads text on stdin and
// prints the findings, `prose-gate fix` rewrites the .md files under the
// given paths in place. With `serve` or no argument it serves gate.decide.
package main

import (
	"fmt"
	"os"

	"github.com/roshbhatia/gate/pkg/gate"
)

const usage = `prose-gate: record a reply that reads like agent prose

Usage:
  prose-gate serve     gate provider. Reads a gate.decide request frame on stdin
                       and routes on args.mode, or on the event when no mode is
                       given: Stop is check, UserPromptSubmit is remind,
                       SessionStart is session, PostToolUse is report.
  prose-gate lint      Reads text on stdin, prints the findings, exits 1 when
                       check would record them.
  prose-gate fix       Rewrites the .md files under the given paths in place,
                       applying every rule that carries an action. --dry-run
                       counts without writing.

lint and fix take --style <vale.ini>, or read PROSE_GATE_STYLE. lint also takes
--max-tells <n> and --block-on <rule>, the same thresholds the step's args
carry. Off with PROSE_GATE=off.
`

func main() {
	args := os.Args[1:]
	if len(args) == 0 || args[0] == "serve" {
		gate.Serve(func(request gate.Request) (gate.Outcome, error) { return Decide(request), nil })
		return
	}
	switch args[0] {
	case "lint":
		os.Exit(lint(args[1:], os.Stdin))
	case "fix":
		os.Exit(fix(args[1:]))
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}
