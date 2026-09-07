// gate-provider-review-gate enforces the review ledger at the hooks:
//
//   - PreToolUse on Agent: a critic or mediator spawns only inside an OPEN
//     pass, only as a read-only agent type, and only up to the tier's count.
//   - SubagentStart: a critic is told which revision is under review, so it
//     never reads an empty `git diff HEAD` and reports a fixed defect as open.
//   - UserPromptSubmit: one line of status while a review is open.
//   - PostToolUse on Agent: after the mediator returns, a reminder to judge.
package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/roshbhatia/gate/extras/internal/ledger"
	"github.com/roshbhatia/gate/pkg/gate"
)

const (
	criticMark   = "ADVERSARIAL-CRITIC-ROLE"
	mediatorMark = "ADVERSARIAL-MEDIATOR-ROLE"
	mediatorType = "review-mediator"
)

func readonly(request gate.Request, pol ledger.Policy) []string {
	if configured := request.Strings("readonly_agents"); len(configured) > 0 {
		return configured
	}
	return pol.ReadonlyAgents
}

// Decide routes on the event.
func Decide(request gate.Request) (gate.Outcome, error) {
	record, ok, err := ledger.Load(request.Event.Cwd)
	if err != nil {
		return gate.Outcome{}, err
	}
	switch request.Event.Event {
	case "PreToolUse":
		return spawn(request, record, ok), nil
	case "SubagentStart":
		return start(request, record, ok), nil
	case "UserPromptSubmit":
		if !ok {
			return gate.PassOutcome(), nil
		}
		return gate.Outcome{Kind: gate.Context, Message: record.Summary()}, nil
	case "PostToolUse":
		return finished(request, record, ok), nil
	}
	return gate.PassOutcome(), nil
}

func spawn(request gate.Request, record ledger.Record, ok bool) gate.Outcome {
	prompt, _ := request.Event.Input["prompt"].(string)
	agentType, _ := request.Event.Input["subagent_type"].(string)
	isCritic := strings.Contains(prompt, criticMark)
	isMediator := strings.Contains(prompt, mediatorMark)
	if !isCritic && !isMediator {
		return gate.PassOutcome()
	}
	if !ok {
		return gate.Outcome{Kind: gate.Deny, Message: "review-gate: no review is open in this repository. Run `review open --change <dir>` first; it measures the diff and decides how many critics the change earns."}
	}
	if record.State != ledger.Open {
		return gate.Outcome{Kind: gate.Deny, Message: fmt.Sprintf("review-gate: the review is %s, not OPEN, so no critic or mediator spawns. %s. A REVISE state reopens with `review reopen` once the tree has changed; anything else is the owner's to read.", record.State, record.Summary())}
	}
	if isMediator {
		if agentType != mediatorType {
			return gate.Outcome{Kind: gate.Deny, Message: fmt.Sprintf("review-gate: the mediator must be the %q agent type, not %q.", mediatorType, agentType)}
		}
		return gate.PassOutcome()
	}
	allowed := readonly(request, ledger.DefaultPolicy())
	if !slices.Contains(allowed, agentType) {
		return gate.Outcome{Kind: gate.Deny, Message: fmt.Sprintf("review-gate: a critic must be a read-only agent type (%s), not %q. A critic that can write injects the defect it wants to prove.", strings.Join(allowed, ", "), agentType)}
	}
	pass := record.Current()
	if pass.Judged {
		return gate.Outcome{Kind: gate.Deny, Message: "review-gate: this pass is already judged. Fix what survived, then `review reopen` if the cap allows."}
	}
	if pass.Critics >= record.Critics {
		return gate.Outcome{Kind: gate.Deny, Message: fmt.Sprintf("review-gate: pass %d already spawned its %d critic(s) for tier %s. Spawn the mediator, write its verdicts to `%s`, and run `review judge`.", pass.N, record.Critics, record.Tier, ledger.PassFile(request.Event.Cwd, pass.N))}
	}
	pass.Critics++
	if err := ledger.Save(request.Event.Cwd, record); err != nil {
		return gate.Outcome{Kind: gate.Deny, Message: "review-gate: " + err.Error()}
	}
	return gate.Outcome{Kind: gate.Allow, Context: fmt.Sprintf("review-gate: critic %d of %d for pass %d. The work is `%s..HEAD` plus the working tree.", pass.Critics, record.Critics, pass.N, short(record.Base))}
}

func start(request gate.Request, record ledger.Record, ok bool) gate.Outcome {
	if !ok || record.State != ledger.Open {
		return gate.PassOutcome()
	}
	allowed := readonly(request, ledger.DefaultPolicy())
	if !slices.Contains(allowed, request.Event.Agent.Type) {
		return gate.PassOutcome()
	}
	return gate.Outcome{Kind: gate.Context, Message: fmt.Sprintf(
		"The work under review is `%s..HEAD` plus the working tree. Read `git diff %s` and the files themselves for current state. `git diff HEAD` is empty after a commit; do not read that as unfixed. Every objection needs a concrete failing scenario and a file:line in the change.",
		short(record.Base), short(record.Base))}
}

func finished(request gate.Request, record ledger.Record, ok bool) gate.Outcome {
	if !ok || record.State != ledger.Open {
		return gate.PassOutcome()
	}
	prompt, _ := request.Event.Input["prompt"].(string)
	if !strings.Contains(prompt, mediatorMark) {
		return gate.PassOutcome()
	}
	pass := record.Current()
	return gate.Outcome{Kind: gate.Context, Message: fmt.Sprintf("review-gate: the mediator returned. Write its verdicts as JSON to `%s` and run `review judge`; the ledger drops any ACCEPT it cannot anchor.", ledger.PassFile(request.Event.Cwd, pass.N))}
}

func short(rev string) string {
	if len(rev) > 12 {
		return rev[:12]
	}
	return rev
}

func main() {
	gate.Serve(Decide)
}
