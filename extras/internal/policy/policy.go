// Package policy loads the owner's review policy from
// $XDG_CONFIG_HOME/gate/review.yaml, or $GATE_REVIEW_CONFIG.
package policy

import (
	"os"
	"path/filepath"

	shared "github.com/roshbhatia/go-utils/config"
	"github.com/roshbhatia/go-utils/paths"

	"github.com/roshbhatia/gate/extras/internal/ledger"
)

// Env names the file override.
const Env = "GATE_REVIEW_CONFIG"

// Path is where the policy is read from.
func Path() string {
	if override := os.Getenv(Env); override != "" {
		return override
	}
	return filepath.Join(paths.ConfigHome(), "gate", "review.yaml")
}

// Load reads the policy over the defaults.
func Load() (ledger.Policy, error) {
	pol, err := shared.Load(ledger.DefaultPolicy(), shared.Options{Name: "gate", Path: Path()})
	if err != nil {
		return pol, err
	}
	return pol, pol.Validate()
}

// Schema is the JSON Schema of the policy file.
func Schema() ([]byte, error) { return shared.Schema[ledger.Policy]("Gate review policy") }
