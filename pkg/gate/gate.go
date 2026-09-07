// Package gate is the contract between the gate dispatcher and a gate provider.
//
// A harness fires a hook, `gate hook` normalizes the event into an Envelope,
// and each provider in the configured chain receives a Request and answers
// with an Outcome. Providers are ordinary executables speaking the go-utils
// provider/v1 frames on stdio; Serve is the whole provider side.
package gate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/roshbhatia/go-utils/provider"
)

const (
	// Version names the envelope shape a provider receives.
	Version = "gate.event/v1"
	// Action is the one capability a gate provider implements.
	Action = "gate.decide"
)

// Agent identifies the subagent a hook fired inside of, when it did.
type Agent struct {
	ID   string `json:"id,omitempty"`
	Type string `json:"type,omitempty"`
}

// Envelope is one harness hook event, with the harness's field names removed.
// Raw carries the original payload for a provider that needs a field the
// envelope does not name.
type Envelope struct {
	Version     string          `json:"version" jsonschema:"enum=gate.event/v1"`
	Harness     string          `json:"harness"`
	Event       string          `json:"event"`
	Tool        string          `json:"tool,omitempty"`
	Input       map[string]any  `json:"input,omitempty"`
	Response    json.RawMessage `json:"response,omitempty"`
	Session     string          `json:"session,omitempty"`
	Agent       Agent           `json:"agent,omitempty"`
	Cwd         string          `json:"cwd,omitempty"`
	StopActive  bool            `json:"stopActive,omitempty"`
	LastMessage string          `json:"lastMessage,omitempty"`
	Prompt      string          `json:"prompt,omitempty"`
	Source      string          `json:"source,omitempty"`
	Raw         json.RawMessage `json:"raw,omitempty"`
}

// Kind is what a provider decided.
type Kind string

const (
	// Pass says nothing and changes nothing.
	Pass Kind = "pass"
	// Allow lets the call run, optionally with a rewritten input.
	Allow Kind = "allow"
	// Deny stops the call. Message reaches the model.
	Deny Kind = "deny"
	// Block sends a Stop or PostToolUse turn back. Message reaches the model.
	Block Kind = "block"
	// Context adds a note for the model and changes nothing else.
	Context Kind = "context"
)

// Outcome is one provider's decision, with no harness vocabulary in it.
//
// Message is the decision's reason. On a deny or block the harness shows it
// to the model; on an allow Claude Code shows it to the user only. Context is
// the note the model reads whatever the decision was, so a provider that
// rewrote the input says so here.
type Outcome struct {
	Kind         Kind           `json:"decision" jsonschema:"enum=pass,enum=allow,enum=deny,enum=block,enum=context"`
	Message      string         `json:"message,omitempty"`
	Context      string         `json:"context,omitempty"`
	UpdatedInput map[string]any `json:"updatedInput,omitempty"`
}

// PassOutcome is the silent decision.
func PassOutcome() Outcome { return Outcome{Kind: Pass} }

// Request is the input of one gate.decide call: the event, and the arguments
// the chain step carried for this provider.
type Request struct {
	Event Envelope       `json:"event"`
	Args  map[string]any `json:"args,omitempty"`
}

// Decider is a provider's decision function.
type Decider func(Request) (Outcome, error)

// Serve reads one provider/v1 request frame from stdin, decides, and writes the
// result frame. It exits 2 on a protocol error and 0 otherwise, so a broken
// provider is visible in the dispatcher's log rather than read as a pass.
func Serve(decide Decider) {
	if err := Run(decide, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

// Run is Serve over explicit streams.
func Run(decide Decider, stdin io.Reader, stdout io.Writer) error {
	var request provider.Request
	decoder := json.NewDecoder(stdin)
	if err := decoder.Decode(&request); err != nil {
		return fmt.Errorf("decode request frame: %w", err)
	}
	if request.Version != provider.Version || request.Kind != provider.FrameRequest {
		return errors.New("unsupported request frame")
	}
	if request.Capability != Action {
		return fmt.Errorf("unsupported capability %q", request.Capability)
	}
	var in Request
	if len(request.Input) > 0 {
		if err := json.Unmarshal(request.Input, &in); err != nil {
			return fmt.Errorf("decode gate request: %w", err)
		}
	}
	if in.Event.Version != "" && in.Event.Version != Version {
		return fmt.Errorf("unsupported event version %q", in.Event.Version)
	}
	result := provider.Result{
		Version:   provider.Version,
		Kind:      provider.FrameResult,
		RequestID: request.RequestID,
		Status:    provider.ResultOK,
	}
	outcome, err := decide(in)
	if err != nil {
		result.Status = provider.ResultError
		result.Message = err.Error()
	} else {
		if outcome.Kind == "" {
			outcome.Kind = Pass
		}
		encoded, err := json.Marshal(outcome)
		if err != nil {
			return fmt.Errorf("encode outcome: %w", err)
		}
		result.Output = encoded
	}
	return json.NewEncoder(stdout).Encode(result)
}

// String returns a value argument, or the fallback when absent or not a string.
func (r Request) String(key, fallback string) string {
	if value, ok := r.Args[key].(string); ok && value != "" {
		return value
	}
	return fallback
}

// Int returns an integer argument. YAML and JSON both decode numbers as
// float64, so that is what is accepted.
func (r Request) Int(key string, fallback int) int {
	switch value := r.Args[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	}
	return fallback
}

// Strings returns a list argument, or nil.
func (r Request) Strings(key string) []string {
	raw, ok := r.Args[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}
