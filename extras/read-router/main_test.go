package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/roshbhatia/gate/extras/internal/bulkread"
	"github.com/roshbhatia/gate/pkg/gate"
)

func TestDecideRoutesOnlyUnboundedLargeTextReads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat(strings.Repeat("x", 100)+"\n", 200)), 0o600); err != nil {
		t.Fatal(err)
	}
	req := func(input map[string]any) gate.Request {
		return gate.Request{Event: gate.Envelope{Event: "PreToolUse", Tool: "Read", Cwd: dir, Input: input}}
	}
	out := Decide(req(map[string]any{"file_path": path}))
	if out.Kind != gate.Deny {
		t.Fatalf("large read decision = %+v", out)
	}
	for _, want := range []string{"offset and limit", bulkread.DefaultReader, path, "about 200 lines"} {
		if !strings.Contains(out.Message, want) {
			t.Fatalf("deny lacks %q: %s", want, out.Message)
		}
	}
	if out := Decide(req(map[string]any{"file_path": "large.txt"})); out.Kind != gate.Deny {
		t.Fatalf("relative large read decision = %+v", out)
	}
	if out := Decide(req(map[string]any{"file_path": path, "limit": float64(10)})); out.Kind != gate.Pass {
		t.Fatalf("ranged read decision = %+v", out)
	}
	if err := os.Rename(path, path+".png"); err != nil {
		t.Fatal(err)
	}
	if out := Decide(req(map[string]any{"file_path": path + ".png"})); out.Kind != gate.Pass {
		t.Fatalf("opaque read decision = %+v", out)
	}
	small := filepath.Join(dir, "small.txt")
	if err := os.WriteFile(small, []byte("hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := Decide(req(map[string]any{"file_path": small})); out.Kind != gate.Pass {
		t.Fatalf("small read decision = %+v", out)
	}
}
