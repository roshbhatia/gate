// gate-provider-notes is the store of review notes an agent and its owner leave
// on a working tree, and the provider that puts the owner's open notes in front
// of the model.
//
// The same binary is the CLI: `note add`, `answer`, `list`, `clear`, and `path`
// write and read the record. With `serve` it serves the gate.decide action: on
// UserPromptSubmit it answers with every note still waiting for an answer as
// context, and with a pass when there is none.
package main

import (
	"fmt"
	"os"

	"github.com/roshbhatia/gate/pkg/gate"
)

// Decide answers the open notes as context. The record is keyed by the
// absolute path of each annotated file, not by the event's cwd, so work that
// spans several repositories reads back as one list from any of them.
func Decide(gate.Request) (gate.Outcome, error) {
	notes, err := openNotes()
	if err != nil {
		return gate.Outcome{}, err
	}
	text := renderOpen(notes)
	if text == "" {
		return gate.PassOutcome(), nil
	}
	return gate.Outcome{Kind: gate.Context, Message: text}, nil
}

// Run dispatches one CLI invocation and returns its exit code.
func Run(args []string) int {
	if len(args) == 0 {
		fmt.Print(usageText)
		return 0
	}
	var err error
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Print(usageText)
		return 0
	case "add":
		err = cmdAdd(args[1:])
	case "answer":
		err = cmdAnswer(args[1:])
	case "list":
		err = cmdList(args[1:])
	case "clear":
		err = cmdClear(args[1:])
	case "path":
		err = cmdPath(args[1:])
	default:
		err = die("unknown subcommand '%s'", args[0])
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "note: %s\n", err)
		return 1
	}
	return 0
}

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "serve" {
		gate.Serve(Decide)
		return
	}
	os.Exit(Run(args))
}
