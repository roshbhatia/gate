package main

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/roshbhatia/gate/internal/config"
	"github.com/roshbhatia/go-utils/provider"
)

func discoverProviders(cfg config.Config) ([]provider.LoadedManifest, error) {
	directories := []string{cfg.Providers.Directory}
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		data = filepath.Join(home, ".local", "share")
	}
	directories = append(directories, filepath.Join(data, "gate", "providers"))
	if executable, err := os.Executable(); err == nil {
		directories = append(directories, filepath.Join(filepath.Dir(executable), "..", "share", "gate", "providers"))
	}
	systemData := os.Getenv("XDG_DATA_DIRS")
	if systemData == "" {
		systemData = "/usr/local/share:/usr/share"
	}
	for _, directory := range filepath.SplitList(systemData) {
		if filepath.IsAbs(directory) {
			directories = append(directories, filepath.Join(directory, "gate", "providers"))
		}
	}
	return discoverDirectories(directories)
}

func discoverDirectories(directories []string) ([]provider.LoadedManifest, error) {
	selected := make(map[string]provider.LoadedManifest)
	visited := make(map[string]bool)
	for _, directory := range directories {
		directory = filepath.Clean(directory)
		if visited[directory] {
			continue
		}
		visited[directory] = true
		manifests, err := provider.Discover(directory)
		if err != nil {
			return nil, err
		}
		for _, manifest := range manifests {
			if _, exists := selected[manifest.Manifest.Name]; !exists {
				selected[manifest.Manifest.Name] = manifest
			}
		}
	}
	loaded := make([]provider.LoadedManifest, 0, len(selected))
	for _, manifest := range selected {
		loaded = append(loaded, manifest)
	}
	sort.Slice(loaded, func(i, j int) bool { return loaded[i].Manifest.Name < loaded[j].Manifest.Name })
	return loaded, nil
}
