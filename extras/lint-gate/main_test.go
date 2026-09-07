package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/roshbhatia/gate/pkg/gate"
)

func write(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestGofmtReportsByOutput(t *testing.T) {
	one := byExtension[".go"][0]
	if report := check(one, write(t, "clean.go", "package sample\n\nfunc Exported() {}\n")); report != "" {
		t.Fatalf("clean file report = %q", report)
	}
	messy := write(t, "messy.go", "package sample\nfunc  Exported()  {}\n")
	if report := check(one, messy); !strings.Contains(report, "messy.go") {
		t.Fatalf("messy file report = %q", report)
	}
	if out := Inspect(messy); out.Kind != gate.Context || !strings.Contains(out.Message, "messy.go") {
		t.Fatalf("messy outcome = %+v", out)
	}
	if out := Inspect(""); out.Kind != gate.Pass {
		t.Fatalf("empty path outcome = %+v", out)
	}
	if report := check(checker{binary: "missing-linter-for-test"}, messy); report != "" {
		t.Fatalf("missing checker report = %q", report)
	}
	if clipped := clip(strings.Repeat("x", widest+1)); !strings.HasSuffix(clipped, "[the rest is cut]") {
		t.Fatal("long report was not clipped")
	}
}
