package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/roshbhatia/gate/extras/internal/state"
	"github.com/roshbhatia/gate/pkg/gate"
)

func armed(t *testing.T, until string, max, stall int) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(state.Env, dir)
	if err := write(stateFile(""), loop{Until: until, Max: max, Stall: stall}); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "loop-gate.json")
}

func stop(active bool) gate.Request {
	return gate.Request{Event: gate.Envelope{Event: "Stop", StopActive: active}}
}

func TestArmParsesAndRefuses(t *testing.T) {
	t.Setenv(state.Env, t.TempDir())
	if code := arm([]string{"--until", "true", "--max", "3"}); code != 0 {
		t.Fatalf("arm exited %d", code)
	}
	s, ok := read(stateFile(""))
	if !ok || s.Until != "true" || s.Max != 3 || s.Stall != 2 {
		t.Fatalf("armed state = %+v, %v", s, ok)
	}
	for _, args := range [][]string{{}, {"--max", "3"}, {"--until", "true", "--max", "many"}, {"--until"}} {
		if code := arm(args); code == 0 {
			t.Errorf("arm accepted %v", args)
		}
	}
}

func TestDecideBlocksUntilThePassThenClears(t *testing.T) {
	path := armed(t, "false", 4, 3)
	out := Decide(stop(false))
	if out.Kind != gate.Block || !strings.Contains(out.Message, "iteration 1/4") {
		t.Fatalf("first failing decision = %+v", out)
	}
	if out := Decide(stop(true)); out.Kind != gate.Pass {
		t.Fatalf("stop-active decision = %+v", out)
	}
	if s, _ := read(path); s.Iter != 2 {
		t.Fatalf("iterations = %d", s.Iter)
	}
	if err := write(path, loop{Until: "true", Max: 4, Stall: 2, Iter: 2}); err != nil {
		t.Fatal(err)
	}
	if out := Decide(stop(false)); out.Kind != gate.Context || !strings.Contains(out.Message, "CLEAN") {
		t.Fatalf("passing decision = %+v", out)
	}
	if _, ok := read(path); ok {
		t.Fatal("state survived the pass")
	}
	if out := Decide(stop(false)); out.Kind != gate.Pass {
		t.Fatalf("disarmed decision = %+v", out)
	}
}

func TestDecideReportsStallAndCap(t *testing.T) {
	armed(t, "echo same; false", 9, 2)
	Decide(stop(false))
	Decide(stop(false))
	if out := Decide(stop(false)); out.Kind != gate.Context || !strings.Contains(out.Message, "STALLED") {
		t.Fatalf("stalled decision = %+v", out)
	}
	armed(t, "date +%N; false", 2, 9)
	Decide(stop(false))
	if out := Decide(stop(false)); out.Kind != gate.Context || !strings.Contains(out.Message, "CAPPED") {
		t.Fatalf("capped decision = %+v", out)
	}
}
