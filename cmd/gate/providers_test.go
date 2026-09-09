package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/roshbhatia/gate/internal/config"
)

func TestSeparateProviderPackagesAndUserOverride(t *testing.T) {
	root := t.TempDir()
	user := filepath.Join(root, "config")
	share := filepath.Join(root, "prefix", "share")
	system := filepath.Join(share, "gate", "providers")
	for _, dir := range []string{user, system} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(dir, name, command string) {
		t.Helper()
		text := "version: provider/v1\nname: " + name + "\ndescription: Review notes\ncommand: [" + command + "]\nactions:\n  gate.decide:\n    description: Review request\n"
		if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(system, "notes", "packaged-notes")
	write(system, "lint", "packaged-lint")
	write(user, "notes", "user-notes")
	t.Setenv("XDG_DATA_DIRS", share)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	cfg := config.Config{}
	cfg.Providers.Directory = user
	loaded, err := discoverProviders(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 || loaded[0].Manifest.Name != "lint" || loaded[1].Manifest.Command[0] != "user-notes" {
		t.Fatalf("discovered providers: %+v", loaded)
	}
	if err := os.WriteFile(filepath.Join(user, "broken.yaml"), []byte("invalid: ["), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := discoverProviders(cfg); err == nil {
		t.Fatal("invalid user manifest was ignored")
	}
}
