// Package config loads ~/.config/gate/config.yaml: the chains, one per hook
// event, and the defaults every step inherits.
package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	shared "github.com/roshbhatia/go-utils/config"
	"github.com/roshbhatia/go-utils/paths"

	"github.com/roshbhatia/gate/internal/event"
)

const (
	// Version is the config document version.
	Version = "gate.config/v1"
	// Name is the config surface; go-utils resolves it to
	// $XDG_CONFIG_HOME/gate/config.yaml, or $GATE_CONFIG.
	Name = "gate"
	// EnvPrefix scopes environment overrides such as GATE_LOG.
	EnvPrefix = "GATE"
)

var providerName = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)

// Step is one provider in a chain.
type Step struct {
	// Provider names a manifest in the providers directory.
	Provider string `json:"provider" yaml:"provider" jsonschema:"pattern=^[a-z][a-z0-9._-]*$"`
	// Match is a regular expression over the tool name. Empty matches every
	// event of the chain's kind.
	Match string `json:"match,omitempty" yaml:"match,omitempty"`
	// Args are handed to the provider unchanged.
	Args map[string]any `json:"args,omitempty" yaml:"args,omitempty"`
	// Timeout bounds this step; zero inherits the default.
	Timeout time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty" jsonschema:"type=string"`
}

// Defaults are inherited by every step.
type Defaults struct {
	// Timeout bounds one provider call. A provider that overruns is logged and
	// read as a pass, so a slow linter cannot hang a hook.
	Timeout time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty" jsonschema:"type=string"`
	// OnRewriteUnsupported says what an allow-with-rewrite becomes on a wire
	// that cannot carry updatedInput: "deny" makes it visible, "pass" keeps
	// the harness's original call.
	OnRewriteUnsupported string `json:"on_rewrite_unsupported,omitempty" yaml:"on_rewrite_unsupported,omitempty" jsonschema:"enum=deny,enum=pass"`
}

// Providers says where manifests are discovered.
type Providers struct {
	Directory string `json:"directory,omitempty" yaml:"directory,omitempty"`
}

// Config is the whole document.
type Config struct {
	Version string `json:"version" yaml:"version" jsonschema:"enum=gate.config/v1"`
	Log     string `json:"log,omitempty" yaml:"log,omitempty"`
	// LogFields names an extra record field and the environment variable it
	// reads. The value is a variable name, not a value, so gate carries an
	// identity it does not have to understand.
	LogFields map[string]string `json:"log_fields,omitempty" yaml:"log_fields,omitempty"`
	Providers Providers         `json:"providers,omitempty" yaml:"providers,omitempty"`
	Defaults  Defaults          `json:"defaults,omitempty" yaml:"defaults,omitempty"`
	Chains    map[string][]Step `json:"chains" yaml:"chains"`
}

// Default is the config before the file and the environment are applied.
func Default() Config {
	return Config{
		Version: Version,
		Log:     filepath.Join(paths.StateHome(), "gate", "decisions.jsonl"),
		Providers: Providers{
			Directory: filepath.Join(paths.ConfigHome(), "gate", "providers"),
		},
		Defaults: Defaults{Timeout: 2 * time.Second, OnRewriteUnsupported: "deny"},
		Chains:   map[string][]Step{},
	}
}

// Options is the go-utils config surface.
func Options() shared.Options { return shared.Options{Name: Name, EnvPrefix: EnvPrefix} }

// Load reads the file and the environment over Default and validates.
func Load() (Config, error) {
	cfg, err := shared.Load(Default(), Options())
	if err != nil {
		return cfg, err
	}
	return cfg, cfg.Validate()
}

// Path is where Load reads from.
func Path() (string, error) { return shared.Path(Options()) }

// Schema is the JSON Schema of Config.
func Schema() ([]byte, error) { return shared.Schema[Config]("Gate configuration") }

// Validate checks the document without touching the host.
func (c Config) Validate() error {
	var problems []error
	if c.Version != Version {
		problems = append(problems, fmt.Errorf("version must be %q", Version))
	}
	if strings.TrimSpace(c.Log) == "" {
		problems = append(problems, errors.New("log path is required"))
	}
	if strings.TrimSpace(c.Providers.Directory) == "" {
		problems = append(problems, errors.New("providers.directory is required"))
	}
	if c.Defaults.Timeout <= 0 {
		problems = append(problems, errors.New("defaults.timeout must be positive"))
	}
	switch c.Defaults.OnRewriteUnsupported {
	case "deny", "pass":
	default:
		problems = append(problems, errors.New("defaults.on_rewrite_unsupported must be deny or pass"))
	}
	for name, steps := range c.Chains {
		if !slices.Contains(event.Known, name) {
			problems = append(problems, fmt.Errorf("chain %q is not a known hook event", name))
		}
		for index, step := range steps {
			where := fmt.Sprintf("chains.%s[%d]", name, index)
			if !providerName.MatchString(step.Provider) {
				problems = append(problems, fmt.Errorf("%s.provider %q is not a valid provider name", where, step.Provider))
			}
			if step.Match != "" {
				if _, err := regexp.Compile(step.Match); err != nil {
					problems = append(problems, fmt.Errorf("%s.match: %w", where, err))
				}
			}
			if step.Timeout < 0 {
				problems = append(problems, fmt.Errorf("%s.timeout must not be negative", where))
			}
		}
	}
	return errors.Join(problems...)
}

// Steps returns the chain for one event.
func (c Config) Steps(eventName string) []Step { return c.Chains[eventName] }

// StepTimeout resolves a step's bound.
func (c Config) StepTimeout(step Step) time.Duration {
	if step.Timeout > 0 {
		return step.Timeout
	}
	return c.Defaults.Timeout
}
