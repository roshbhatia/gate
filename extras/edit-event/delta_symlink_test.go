package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A payload path through a symlinked directory still counts as inside the tree.
func TestInTreeResolvesSymlinks(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.MkdirAll(filepath.Join(real, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	tree, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(link, "sub", "a.txt")
	if err := os.WriteFile(file, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !inTree(tree, file) {
		t.Fatalf("inTree(%q, %q) = false, want true", tree, file)
	}
	if inTree(tree, filepath.Join(base, "elsewhere", "b.txt")) {
		t.Fatal("a path outside the tree must not match")
	}
	deleted := filepath.Join(link, "sub", "gone.txt")
	if !inTree(tree, deleted) {
		t.Fatalf("a deleted file under a symlinked dir must still match: %q", deleted)
	}
}
