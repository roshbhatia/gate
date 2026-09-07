package chain

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/roshbhatia/go-utils/provider"

	"github.com/roshbhatia/gate/internal/config"
	"github.com/roshbhatia/gate/pkg/gate"
)

type fakeRegistry map[string]bool

func (r fakeRegistry) Lookup(name string) (provider.LoadedManifest, bool) {
	if !r[name] {
		return provider.LoadedManifest{}, false
	}
	return provider.LoadedManifest{Manifest: provider.Manifest{Name: name}}, true
}

type scripted map[string]func(gate.Request) (gate.Outcome, error)

func (s scripted) invoke(_ context.Context, manifest provider.Manifest, request provider.Request, _ time.Duration) (provider.Result, error) {
	var in gate.Request
	if err := json.Unmarshal(request.Input, &in); err != nil {
		return provider.Result{}, err
	}
	outcome, err := s[manifest.Name](in)
	if err != nil {
		return provider.Result{}, err
	}
	output, _ := json.Marshal(outcome)
	return provider.Result{Version: provider.Version, Kind: provider.FrameResult, RequestID: request.RequestID, Status: provider.ResultOK, Output: output}, nil
}

func cfg(steps ...config.Step) config.Config {
	c := config.Default()
	c.Chains["PreToolUse"] = steps
	return c
}

func TestDenyWinsAndLaterProvidersStillRun(t *testing.T) {
	ran := []string{}
	s := scripted{
		"a": func(gate.Request) (gate.Outcome, error) {
			ran = append(ran, "a")
			return gate.Outcome{Kind: gate.Deny, Message: "no"}, nil
		},
		"b": func(gate.Request) (gate.Outcome, error) {
			ran = append(ran, "b")
			return gate.Outcome{Kind: gate.Context, Message: "note"}, nil
		},
	}
	result := Run(context.Background(), cfg(config.Step{Provider: "a"}, config.Step{Provider: "b"}),
		fakeRegistry{"a": true, "b": true}, s.invoke,
		gate.Envelope{Event: "PreToolUse", Tool: "Bash"})
	if result.Outcome.Kind != gate.Deny || result.Outcome.Message != "no" || result.Outcome.Context != "note" {
		t.Fatalf("merged = %+v", result.Outcome)
	}
	if len(ran) != 2 || len(result.Decisions) != 2 {
		t.Fatalf("ran %v, decisions %+v", ran, result.Decisions)
	}
}

func TestRewriteChainsIntoTheNextProvider(t *testing.T) {
	s := scripted{
		"bound": func(r gate.Request) (gate.Outcome, error) {
			return gate.Outcome{Kind: gate.Allow, Context: "capped",
				UpdatedInput: map[string]any{"command": r.Event.Input["command"].(string) + " | head"}}, nil
		},
		"seen": func(r gate.Request) (gate.Outcome, error) {
			if r.Event.Input["command"] != "rg x | head" {
				t.Fatalf("second provider saw %v", r.Event.Input)
			}
			return gate.Outcome{Kind: gate.Context, Message: "seen"}, nil
		},
	}
	result := Run(context.Background(), cfg(config.Step{Provider: "bound", Match: "^Bash$"}, config.Step{Provider: "seen"}, config.Step{Provider: "skipped", Match: "^Read$"}),
		fakeRegistry{"bound": true, "seen": true, "skipped": true}, s.invoke,
		gate.Envelope{Event: "PreToolUse", Tool: "Bash", Input: map[string]any{"command": "rg x"}})
	if result.Outcome.Kind != gate.Allow || result.Outcome.UpdatedInput["command"] != "rg x | head" {
		t.Fatalf("merged = %+v", result.Outcome)
	}
	if result.Outcome.Context != "capped\n\nseen" {
		t.Fatalf("context = %q", result.Outcome.Context)
	}
	if len(result.Decisions) != 2 {
		t.Fatalf("a non-matching step ran: %+v", result.Decisions)
	}
}

func TestTimeoutPrecedence(t *testing.T) {
	c := config.Default()
	manifest := provider.Manifest{Defaults: provider.Defaults{Timeout: provider.Duration(5 * time.Minute)}}
	if got := timeoutFor(c, config.Step{Timeout: time.Second}, manifest); got != time.Second {
		t.Fatalf("step timeout lost: %v", got)
	}
	if got := timeoutFor(c, config.Step{}, manifest); got != 5*time.Minute {
		t.Fatalf("manifest timeout lost: %v", got)
	}
	if got := timeoutFor(c, config.Step{}, provider.Manifest{}); got != c.Defaults.Timeout {
		t.Fatalf("config default lost: %v", got)
	}
}

func TestErrorsAndMissingManifestsReadAsPassAndAreLogged(t *testing.T) {
	s := scripted{"broken": func(gate.Request) (gate.Outcome, error) { return gate.Outcome{}, errors.New("timed out") }}
	result := Run(context.Background(), cfg(config.Step{Provider: "broken"}, config.Step{Provider: "absent"}),
		fakeRegistry{"broken": true}, s.invoke, gate.Envelope{Event: "PreToolUse", Tool: "Bash"})
	if result.Outcome.Kind != gate.Pass {
		t.Fatalf("merged = %+v", result.Outcome)
	}
	if len(result.Decisions) != 2 || result.Decisions[0].Error != "timed out" || result.Decisions[1].Error == "" {
		t.Fatalf("decisions = %+v", result.Decisions)
	}
}
