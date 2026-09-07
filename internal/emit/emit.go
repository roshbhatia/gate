// Package emit encodes a merged decision for one harness.
//
// The decision and the wire shape are separate on purpose: a provider returns a
// gate.Outcome and this package turns it into the bytes the harness reads.
package emit

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/roshbhatia/gate/pkg/gate"
)

// Format names one wire shape.
type Format string

const (
	// Claude is Claude Code's hook JSON on stdout, always exit 0. Codex reads
	// the same shape.
	Claude Format = "claude"
	// ExitCode carries the message on stderr and the verdict in the status:
	// 0 lets the call through, 2 stops it. Gemini reads this.
	ExitCode Format = "exit-code"
	// JSON is the gate.Outcome itself, for an adapter or a test.
	JSON Format = "json"
)

// Parse validates a format name.
func Parse(name string) (Format, error) {
	switch Format(name) {
	case Claude, ExitCode, JSON:
		return Format(name), nil
	}
	return "", fmt.Errorf("unknown format %q; expected claude, exit-code, or json", name)
}

type claudeHook struct {
	HookEventName            string         `json:"hookEventName"`
	PermissionDecision       string         `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string         `json:"permissionDecisionReason,omitempty"`
	AdditionalContext        string         `json:"additionalContext,omitempty"`
	UpdatedInput             map[string]any `json:"updatedInput,omitempty"`
}

type claudeOutput struct {
	HookSpecificOutput claudeHook `json:"hookSpecificOutput"`
}

type claudeBlock struct {
	Decision           string      `json:"decision"`
	Reason             string      `json:"reason"`
	HookSpecificOutput *claudeHook `json:"hookSpecificOutput,omitempty"`
}

// Emit writes the outcome for the event in the given format and returns the
// process exit status.
func Emit(format Format, event string, out gate.Outcome) int {
	return EmitTo(os.Stdout, os.Stderr, format, event, out)
}

// EmitTo is Emit over explicit streams.
func EmitTo(stdout, stderr io.Writer, format Format, event string, out gate.Outcome) int {
	if out.Kind == gate.Pass || out.Kind == "" {
		return 0
	}
	switch format {
	case ExitCode:
		return exitCode(stderr, out)
	case JSON:
		return write(stdout, stderr, out)
	default:
		return claude(stdout, stderr, event, out)
	}
}

// exitCode has one channel, stderr, and it reaches the user rather than the
// model. A deny carries its message there with exit 2. Anything else prints
// the note it has, so a rewrite or a cap is at least visible to the person.
func exitCode(stderr io.Writer, out gate.Outcome) int {
	switch out.Kind {
	case gate.Deny, gate.Block:
		if _, err := fmt.Fprintln(stderr, out.Message); err != nil {
			return 1
		}
		return 2
	default:
		note := out.Context
		if note == "" && out.Kind == gate.Context {
			note = out.Message
		}
		if note != "" {
			if _, err := fmt.Fprintln(stderr, note); err != nil {
				return 1
			}
		}
		return 0
	}
}

func claude(stdout, stderr io.Writer, event string, out gate.Outcome) int {
	switch out.Kind {
	case gate.Block:
		block := claudeBlock{Decision: "block", Reason: out.Message}
		if out.Context != "" {
			block.HookSpecificOutput = &claudeHook{HookEventName: event, AdditionalContext: out.Context}
		}
		return write(stdout, stderr, block)
	case gate.Context:
		note := out.Message
		if out.Context != "" {
			if note != "" {
				note += "\n\n"
			}
			note += out.Context
		}
		return write(stdout, stderr, claudeOutput{claudeHook{HookEventName: event, AdditionalContext: note}})
	default:
		// additionalContext is honored beside permissionDecision, and on an
		// allow it is the only one of the two the model reads.
		return write(stdout, stderr, claudeOutput{claudeHook{
			HookEventName:            event,
			PermissionDecision:       string(out.Kind),
			PermissionDecisionReason: out.Message,
			AdditionalContext:        out.Context,
			UpdatedInput:             out.UpdatedInput,
		}})
	}
}

func write(stdout, stderr io.Writer, v any) int {
	encoded, err := json.Marshal(v)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	if _, err := fmt.Fprintln(stdout, string(encoded)); err != nil {
		return 1
	}
	return 0
}
