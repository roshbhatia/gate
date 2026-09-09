// gate-provider-edit-event records every agent write as an ordered delta, with
// the prompt that asked for it.
//
//   - UserPromptSubmit: save the prompt, keyed by the workspace the harness is
//     working in, so the next write can name what asked for it.
//   - PostToolUse on an edit tool: append one JSON line per written file to the
//     workspace's edit log, and commit the file into a shadow git repository
//     whose commit subject is the saved prompt.
//
// It answers pass on every event; the record is a side effect. An editor that
// wants the log or the shadow repository computes the same path from the
// `agentEdits` entry of the paths manifest and go-utils' paths.Keyed, and reads
// them directly; there is no query verb here.
package main

import (
	"fmt"
	"os"

	"github.com/roshbhatia/gate/pkg/gate"
)

const usage = `edit-event: record every agent write as an ordered delta, with the prompt that asked for it

Usage:
  gate-provider-edit-event serve   gate provider. Reads a gate.decide request frame on
                                   stdin. UserPromptSubmit saves the prompt; PostToolUse
                                   on an edit tool appends the edit log and commits the
                                   file into the workspace's shadow repository.

The log is <agentEdits>/<workspace>-<key>.jsonl, the shadow repository is
<agentEdits>/<workspace>-<key>.delta, and the saved prompt is
<agentEdits>/<workspace>-<key>.prompt, where agentEdits is the paths manifest's
entry and the key is paths.Keyed's digest of the workspace root.
`

func main() {
	args := os.Args[1:]
	if len(args) == 0 || args[0] == "serve" {
		gate.Serve(Decide)
		return
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}
