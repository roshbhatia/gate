package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/roshbhatia/gate/extras/internal/ledger"
	"github.com/roshbhatia/gate/extras/internal/state"
	"github.com/roshbhatia/gate/pkg/gate"
)

func open(t *testing.T, st ledger.State, critics int) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(state.Env, filepath.Join(dir, ".gate"))
	record := ledger.Record{Change: "demo", Base: "abcdef1234567890", Tier: "full", Critics: critics, PassesMax: 2, State: st,
		Passes: []ledger.Pass{{N: 1, Tree: "t", At: time.Now()}}}
	if err := ledger.Save(dir, record); err != nil {
		t.Fatal(err)
	}
	return dir
}

func agent(dir, event, prompt, agentType string) gate.Request {
	return gate.Request{Event: gate.Envelope{Event: event, Tool: "Agent", Cwd: dir,
		Input: map[string]any{"prompt": prompt, "subagent_type": agentType}}}
}

func TestSpawnIsBoundedByTheLedger(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(state.Env, filepath.Join(dir, ".gate"))
	out, _ := Decide(agent(dir, "PreToolUse", "ADVERSARIAL-CRITIC-ROLE: x", "Explore"))
	if out.Kind != gate.Deny || !strings.Contains(out.Message, "review open") {
		t.Fatalf("no-ledger spawn = %+v", out)
	}
	if out, _ := Decide(agent(dir, "PreToolUse", "ordinary task", "general-purpose")); out.Kind != gate.Pass {
		t.Fatalf("ordinary spawn = %+v", out)
	}

	dir = open(t, ledger.Open, 2)
	if out, _ := Decide(agent(dir, "PreToolUse", "ADVERSARIAL-CRITIC-ROLE: x", "general-purpose")); out.Kind != gate.Deny || !strings.Contains(out.Message, "read-only") {
		t.Fatalf("writing critic = %+v", out)
	}
	for i := 1; i <= 2; i++ {
		out, _ := Decide(agent(dir, "PreToolUse", "ADVERSARIAL-CRITIC-ROLE: x", "Explore"))
		if out.Kind != gate.Allow || !strings.Contains(out.Context, "abcdef123456") {
			t.Fatalf("critic %d = %+v", i, out)
		}
	}
	if out, _ := Decide(agent(dir, "PreToolUse", "ADVERSARIAL-CRITIC-ROLE: x", "Explore")); out.Kind != gate.Deny || !strings.Contains(out.Message, "review judge") {
		t.Fatalf("third critic = %+v", out)
	}
	if out, _ := Decide(agent(dir, "PreToolUse", "ADVERSARIAL-MEDIATOR-ROLE: x", "Explore")); out.Kind != gate.Deny {
		t.Fatalf("mediator as wrong type = %+v", out)
	}
	if out, _ := Decide(agent(dir, "PreToolUse", "ADVERSARIAL-MEDIATOR-ROLE: x", "review-mediator")); out.Kind != gate.Pass {
		t.Fatalf("mediator = %+v", out)
	}

	dir = open(t, ledger.Capped, 3)
	if out, _ := Decide(agent(dir, "PreToolUse", "ADVERSARIAL-CRITIC-ROLE: x", "Explore")); out.Kind != gate.Deny || !strings.Contains(out.Message, "CAPPED") {
		t.Fatalf("capped spawn = %+v", out)
	}
}

func TestStartPromptAndFinishNotes(t *testing.T) {
	dir := open(t, ledger.Open, 1)
	out, _ := Decide(gate.Request{Event: gate.Envelope{Event: "SubagentStart", Cwd: dir, Agent: gate.Agent{Type: "Explore"}}})
	if out.Kind != gate.Context || !strings.Contains(out.Message, "git diff abcdef123456") {
		t.Fatalf("start note = %+v", out)
	}
	out, _ = Decide(gate.Request{Event: gate.Envelope{Event: "SubagentStart", Cwd: dir, Agent: gate.Agent{Type: "general-purpose"}}})
	if out.Kind != gate.Pass {
		t.Fatalf("start note for a worker = %+v", out)
	}
	out, _ = Decide(gate.Request{Event: gate.Envelope{Event: "UserPromptSubmit", Cwd: dir}})
	if out.Kind != gate.Context || !strings.Contains(out.Message, "review OPEN") {
		t.Fatalf("prompt note = %+v", out)
	}
	out, _ = Decide(agent(dir, "PostToolUse", "ADVERSARIAL-MEDIATOR-ROLE: x", "review-mediator"))
	if out.Kind != gate.Context || !strings.Contains(out.Message, "review judge") {
		t.Fatalf("finish note = %+v", out)
	}
}
