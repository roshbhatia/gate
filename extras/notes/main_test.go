package main

import (
	"strings"
	"testing"

	"github.com/roshbhatia/gate/pkg/gate"
)

func submit() gate.Request {
	return gate.Request{Event: gate.Envelope{Event: "UserPromptSubmit", Cwd: "/elsewhere"}}
}

func TestDecidePassesWithNoStore(t *testing.T) {
	newRepo(t)
	out, err := Decide(submit())
	if err != nil || out.Kind != gate.Pass {
		t.Fatalf("decision with no store = %+v, %v", out, err)
	}
}

func TestDecideCarriesTheOpenNotesAsContext(t *testing.T) {
	root := newRepo(t)
	mustAdd(t, "--file", "src/app.ts", "--line", "2", "--summary", "why this here", "--origin", "user")
	mustAdd(t, "--file", "src/app.ts", "--line", "3", "--summary", "an agent's own note")
	id, _ := notes(t)[0]["id"].(string)

	out, err := Decide(submit())
	if err != nil || out.Kind != gate.Context {
		t.Fatalf("decision with an open note = %+v, %v", out, err)
	}
	for _, want := range []string{"1 note(s)", "[" + id + "]", root + "/src/app.ts:2", "why this here", "note answer --id"} {
		if !strings.Contains(out.Message, want) {
			t.Fatalf("context lacks %q:\n%s", want, out.Message)
		}
	}
	if strings.Contains(out.Message, "an agent's own note") {
		t.Fatal("an agent's note, which is never open, reached the context")
	}
	if strings.ContainsAny(out.Message, "\x1b\r") {
		t.Fatalf("the context carries a control byte: %q", out.Message)
	}
}

func TestDecidePassesOnceEveryNoteIsAnswered(t *testing.T) {
	newRepo(t)
	mustAdd(t, "--file", "src/app.ts", "--line", "2", "--summary", "why this here", "--origin", "user")
	id, _ := notes(t)[0]["id"].(string)
	if code, _ := run(t, "answer", "--id", id, "--summary", "because"); code != 0 {
		t.Fatal("answer exited non-zero")
	}
	out, err := Decide(submit())
	if err != nil || out.Kind != gate.Pass {
		t.Fatalf("decision after the answer = %+v, %v", out, err)
	}
}

func TestDecideReportsAStoreItCannotRead(t *testing.T) {
	newRepo(t)
	mustAdd(t, "--file", "src/app.ts", "--line", "1", "--summary", "seed")
	if err := writeStore(t, "not json"); err != nil {
		t.Fatal(err)
	}
	if _, err := Decide(submit()); err == nil {
		t.Fatal("a corrupt store read as a pass")
	}
}

func TestDecideFollowsTheLineThroughAnEdit(t *testing.T) {
	root := newRepo(t)
	mustAdd(t, "--file", "src/app.ts", "--line", "2", "--summary", "about two", "--origin", "user")
	rewrite(t, root, "src/app.ts", "zero\nhalf\none\ntwo\nthree\n")
	out, err := Decide(submit())
	if err != nil || !strings.Contains(out.Message, "src/app.ts:4 ") {
		t.Fatalf("the context did not re-anchor the note: %+v, %v", out, err)
	}
}
