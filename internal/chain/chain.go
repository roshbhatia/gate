// Package chain runs the configured providers for one event and merges what
// they decided.
package chain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/roshbhatia/go-utils/provider"

	"github.com/roshbhatia/gate/internal/config"
	"github.com/roshbhatia/gate/internal/log"
	"github.com/roshbhatia/gate/pkg/gate"
)

// Invoker runs one provider action. The default shells out through go-utils;
// tests substitute a fake.
type Invoker func(ctx context.Context, manifest provider.Manifest, request provider.Request, timeout time.Duration) (provider.Result, error)

// Registry finds a manifest by provider name.
type Registry interface {
	Lookup(name string) (provider.LoadedManifest, bool)
}

// Result is the merged decision plus the per-provider trail.
type Result struct {
	Outcome   gate.Outcome
	Decisions []log.Decision
}

// Run executes the chain for the envelope's event and merges the outcomes.
//
// Merge order is fixed. The first deny or block wins and later providers still
// run, so the log is complete. An allow that rewrites the input feeds the
// rewritten input to the next provider. Every context note is kept and
// concatenated onto whatever the final decision is. Anything else is a pass.
func Run(ctx context.Context, cfg config.Config, registry Registry, invoke Invoker, env gate.Envelope) Result {
	var (
		result   Result
		deny     *gate.Outcome
		block    *gate.Outcome
		rewrite  *gate.Outcome
		contexts []string
	)
	for _, step := range cfg.Steps(env.Event) {
		if !matches(step.Match, env.Tool) {
			continue
		}
		started := time.Now()
		decision := log.Decision{Provider: step.Provider, Kind: gate.Pass}
		outcome, err := runStep(ctx, cfg, registry, invoke, step, env)
		decision.Ms = time.Since(started).Milliseconds()
		if err != nil {
			decision.Error = err.Error()
			result.Decisions = append(result.Decisions, decision)
			continue
		}
		decision.Kind = outcome.Kind
		decision.Message = log.Clip(firstNonEmpty(outcome.Message, outcome.Context))
		result.Decisions = append(result.Decisions, decision)
		if outcome.Context != "" {
			contexts = append(contexts, outcome.Context)
		}
		switch outcome.Kind {
		case gate.Deny:
			if deny == nil {
				copied := outcome
				deny = &copied
			}
		case gate.Block:
			if block == nil {
				copied := outcome
				block = &copied
			}
		case gate.Allow:
			if outcome.UpdatedInput != nil {
				copied := outcome
				rewrite = &copied
				env.Input = outcome.UpdatedInput
			}
		case gate.Context:
			if outcome.Message != "" {
				contexts = append(contexts, outcome.Message)
			}
		}
	}
	note := strings.Join(contexts, "\n\n")
	switch {
	case deny != nil:
		result.Outcome = gate.Outcome{Kind: gate.Deny, Message: deny.Message, Context: note}
	case block != nil:
		result.Outcome = gate.Outcome{Kind: gate.Block, Message: block.Message, Context: note}
	case rewrite != nil:
		result.Outcome = gate.Outcome{Kind: gate.Allow, Message: rewrite.Message, Context: note, UpdatedInput: env.Input}
	case note != "":
		result.Outcome = gate.Outcome{Kind: gate.Context, Message: note}
	default:
		result.Outcome = gate.PassOutcome()
	}
	return result
}

func runStep(ctx context.Context, cfg config.Config, registry Registry, invoke Invoker, step config.Step, env gate.Envelope) (gate.Outcome, error) {
	loaded, ok := registry.Lookup(step.Provider)
	if !ok {
		return gate.Outcome{}, fmt.Errorf("provider %q has no manifest", step.Provider)
	}
	input, err := json.Marshal(gate.Request{Event: env, Args: step.Args})
	if err != nil {
		return gate.Outcome{}, fmt.Errorf("encode request: %w", err)
	}
	request := provider.Request{
		Version:    provider.Version,
		Kind:       provider.FrameRequest,
		RequestID:  requestID(env, step.Provider),
		Capability: gate.Action,
		Input:      input,
	}
	result, err := invoke(ctx, loaded.Manifest, request, timeoutFor(cfg, step, loaded.Manifest))
	if err != nil {
		return gate.Outcome{}, err
	}
	switch result.Status {
	case provider.ResultOK:
	case provider.ResultDeclined:
		return gate.PassOutcome(), nil
	default:
		return gate.Outcome{}, fmt.Errorf("provider %q failed: %s", step.Provider, result.Message)
	}
	var outcome gate.Outcome
	if len(result.Output) > 0 {
		if err := json.Unmarshal(result.Output, &outcome); err != nil {
			return gate.Outcome{}, fmt.Errorf("decode outcome from %q: %w", step.Provider, err)
		}
	}
	if outcome.Kind == "" {
		outcome.Kind = gate.Pass
	}
	return outcome, nil
}

// DefaultInvoker runs the provider process through go-utils.
func DefaultInvoker(ctx context.Context, manifest provider.Manifest, request provider.Request, timeout time.Duration) (provider.Result, error) {
	invocation, err := provider.Invoke(ctx, manifest, request, provider.InvokeOptions{
		Timeout: timeout,
		Limits:  provider.Limits{MaxOutputBytes: 1024 * 1024},
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return provider.Result{}, fmt.Errorf("timed out after %s", timeout)
		}
		return provider.Result{}, err
	}
	return invocation.Result, nil
}

// timeoutFor resolves the bound for one step: the step's own, then the
// manifest's default, then the config default. loop-gate runs the owner's
// STOP command and declares minutes; bash-guard declares seconds.
func timeoutFor(cfg config.Config, step config.Step, manifest provider.Manifest) time.Duration {
	if step.Timeout > 0 {
		return step.Timeout
	}
	if declared := manifest.Defaults.Timeout.Duration(); declared > 0 {
		return declared
	}
	return cfg.Defaults.Timeout
}

func matches(pattern, tool string) bool {
	if pattern == "" {
		return true
	}
	matched, err := regexp.MatchString(pattern, tool)
	return err == nil && matched
}

func requestID(env gate.Envelope, providerName string) string {
	return fmt.Sprintf("%s-%s-%d", providerName, env.Event, time.Now().UnixNano())
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
