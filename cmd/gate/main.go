// gate dispatches one harness hook event through a configured chain of gate
// providers and tells the harness what they decided.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/roshbhatia/go-utils/completion"
	shared "github.com/roshbhatia/go-utils/config"
	"github.com/roshbhatia/go-utils/provider"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/roshbhatia/gate/internal/chain"
	"github.com/roshbhatia/gate/internal/config"
	"github.com/roshbhatia/gate/internal/emit"
	"github.com/roshbhatia/gate/internal/event"
	gatelog "github.com/roshbhatia/gate/internal/log"
	"github.com/roshbhatia/gate/pkg/gate"
)

const about = `gate runs the hook chain a harness declared and answers in the harness's shape.

A harness calls one command per hook event:

  gate hook --harness claude --event PreToolUse

gate reads the event on stdin, runs every provider configured for that event
whose match covers the tool, merges the decisions, and prints the result. The
first deny or block wins, a rewrite feeds the next provider, and every note
reaches the model. Each call is one line in the decisions log.

Providers are executables with a provider/v1 manifest in the providers
directory. They read a gate.decide request on stdin and answer with a decision.`

func main() {
	if err := command().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func command() *cobra.Command {
	root := &cobra.Command{
		Use:           "gate",
		Short:         "Run hook decisions through a configured provider chain",
		Long:          about,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
	}
	root.AddCommand(hookCommand(), configCommand(), providerCommand(), logCommand(), completionCommand(), generateCommand())
	return root
}

type registry map[string]provider.LoadedManifest

func (r registry) Lookup(name string) (provider.LoadedManifest, bool) {
	loaded, ok := r[name]
	return loaded, ok
}

func loadRegistry(cfg config.Config) (registry, error) {
	loaded, err := provider.Discover(cfg.Providers.Directory)
	if err != nil {
		return nil, err
	}
	reg := make(registry, len(loaded))
	for _, one := range loaded {
		reg[one.Manifest.Name] = one
	}
	return reg, nil
}

func hookCommand() *cobra.Command {
	var harness, eventName, format string
	cmd := &cobra.Command{
		Use:   "hook",
		Short: "Decide one hook event read from stdin",
		Long: `Reads the harness's hook payload on stdin, runs the chain configured for the
event, and prints the merged decision in the harness's format. Exit status is
0, or 2 for a deny under --format exit-code. A missing or invalid config is
exit 1 with the reason on stderr: a hook that cannot read its policy must not
read as a pass.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			wire, err := emit.Parse(format)
			if err != nil {
				return err
			}
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("gate config: %w", err)
			}
			reg, err := loadRegistry(cfg)
			if err != nil {
				return fmt.Errorf("gate providers: %w", err)
			}
			raw, err := io.ReadAll(cmd.InOrStdin())
			if err != nil {
				return fmt.Errorf("read hook payload: %w", err)
			}
			env, err := event.Normalize(harness, eventName, raw)
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			started := time.Now()
			result := chain.Run(ctx, cfg, reg, chain.DefaultInvoker, env)
			outcome := result.Outcome
			if wire == emit.ExitCode && outcome.Kind == gate.Allow && outcome.UpdatedInput != nil {
				// This wire cannot carry a rewrite. Saying so beats pretending.
				if cfg.Defaults.OnRewriteUnsupported == "deny" {
					outcome = gate.Outcome{Kind: gate.Deny, Message: rewriteDenied(outcome), Context: outcome.Context}
				} else {
					outcome = gate.Outcome{Kind: gate.Context, Message: outcome.Context}
				}
			}
			record := gatelog.Record{
				Ts: started.UTC(), Harness: env.Harness, Event: env.Event, Session: env.Session,
				Agent: env.Agent.Type, Cwd: env.Cwd, Tool: env.Tool, Final: outcome.Kind,
				Ms: time.Since(started).Milliseconds(), Decisions: result.Decisions,
			}
			if err := gatelog.Append(cfg.Log, record); err != nil {
				fmt.Fprintf(os.Stderr, "gate: %v\n", err)
			}
			os.Exit(emit.EmitTo(cmd.OutOrStdout(), os.Stderr, wire, env.Event, outcome))
			return nil
		},
	}
	cmd.Flags().StringVar(&harness, "harness", "claude", "which harness wrote the payload: claude, codex, gemini, or json")
	cmd.Flags().StringVar(&eventName, "event", "", "the hook event; read from the payload when omitted")
	cmd.Flags().StringVar(&format, "format", "claude", "the wire shape to answer in: claude, exit-code, or json")
	_ = cmd.RegisterFlagCompletionFunc("harness", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return []string{"claude", "codex", "gemini", "json"}, cobra.ShellCompDirectiveNoFileComp
	})
	_ = cmd.RegisterFlagCompletionFunc("event", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return event.Known, cobra.ShellCompDirectiveNoFileComp
	})
	_ = cmd.RegisterFlagCompletionFunc("format", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return []string{"claude", "exit-code", "json"}, cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}

func rewriteDenied(outcome gate.Outcome) string {
	var b strings.Builder
	b.WriteString("gate: a provider rewrote this call and this harness cannot carry a rewritten input, so the call is denied instead.")
	if command, ok := outcome.UpdatedInput["command"].(string); ok {
		b.WriteString(" Run this form yourself: ")
		b.WriteString(command)
	}
	return b.String()
}

func configCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Inspect the gate configuration"}
	cmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print where the configuration is read from",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := config.Path()
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), path)
			return err
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "validate",
		Short: "Load the configuration and every referenced provider manifest",
		Long: `Fails when the file does not parse, names an unknown event, carries a match
that does not compile, or refers to a provider with no manifest. Run it at
build time: a hook that finds a broken config at runtime exits 1 and the
harness proceeds without a decision.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			reg, err := loadRegistry(cfg)
			if err != nil {
				return err
			}
			var problems []error
			for eventName, steps := range cfg.Chains {
				for _, step := range steps {
					loaded, ok := reg[step.Provider]
					if !ok {
						problems = append(problems, fmt.Errorf("chains.%s: provider %q has no manifest in %s", eventName, step.Provider, cfg.Providers.Directory))
						continue
					}
					if _, ok := loaded.Manifest.Actions[gate.Action]; !ok {
						problems = append(problems, fmt.Errorf("provider %q does not implement %s", step.Provider, gate.Action))
					}
				}
			}
			if err := errors.Join(problems...); err != nil {
				return err
			}
			path, _ := config.Path()
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s: %d chains, %d providers\n", path, len(cfg.Chains), len(reg))
			return err
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Print the effective configuration as JSON",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(cfg)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "schema",
		Short: "Print the configuration JSON Schema",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			schema, err := config.Schema()
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(schema)
			return err
		},
	})
	return cmd
}

func providerCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "provider", Short: "Inspect gate providers"}
	var asJSON bool
	list := &cobra.Command{
		Use:   "list",
		Short: "List the providers in the providers directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			loaded, err := provider.Discover(cfg.Providers.Directory)
			if err != nil {
				return err
			}
			if asJSON {
				manifests := make([]provider.Manifest, 0, len(loaded))
				for _, one := range loaded {
					manifests = append(manifests, one.Manifest)
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(manifests)
			}
			for _, one := range loaded {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\tcommand: %s\n", one.Manifest.Name, one.Manifest.Description, strings.Join(one.Manifest.Command, " "))
			}
			return nil
		},
	}
	list.Flags().BoolVar(&asJSON, "json", false, "print the manifests as JSON")
	validate := &cobra.Command{
		Use:   "validate [name]",
		Short: "Check that a provider's manifest and host dependencies are in place",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			loaded, err := provider.Discover(cfg.Providers.Directory)
			if err != nil {
				return err
			}
			failed := false
			for _, one := range loaded {
				if len(args) == 1 && one.Manifest.Name != args[0] {
					continue
				}
				report := (provider.Validator{}).Validate(one.Manifest, filepath.Dir(one.Path))
				for _, check := range report.Checks {
					fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", one.Manifest.Name, check.Kind, check.Target, check.Status)
				}
				if _, ok := one.Manifest.Actions[gate.Action]; !ok {
					fmt.Fprintf(cmd.OutOrStdout(), "%s\taction\t%s\tfailed\n", one.Manifest.Name, gate.Action)
					failed = true
				}
				failed = failed || !report.OK()
			}
			if failed {
				return errors.New("a provider failed validation")
			}
			return nil
		},
	}
	cmd.AddCommand(list, validate)
	return cmd
}

func logCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "log", Short: "Read the decisions log"}
	var count int
	tail := &cobra.Command{
		Use:   "tail",
		Short: "Print the last decisions, one JSON line each",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			file, err := os.Open(cfg.Log)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return nil
				}
				return err
			}
			defer file.Close()
			var lines []string
			scanner := bufio.NewScanner(file)
			scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
			for scanner.Scan() {
				lines = append(lines, scanner.Text())
				if len(lines) > count {
					lines = lines[1:]
				}
			}
			if err := scanner.Err(); err != nil {
				return err
			}
			for _, line := range lines {
				fmt.Fprintln(cmd.OutOrStdout(), line)
			}
			return nil
		},
	}
	tail.Flags().IntVarP(&count, "lines", "n", 20, "how many records to print")
	cmd.AddCommand(tail)
	return cmd
}

func completionCommand() *cobra.Command {
	return &cobra.Command{
		Use:       "completion <shell>",
		Short:     "Print shell completion for bash, fish, nu, or zsh",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "fish", "nu", "zsh"},
		RunE: func(cmd *cobra.Command, args []string) error {
			script, err := completion.Generate(args[0], completionSpec(command()))
			if err != nil {
				return err
			}
			_, err = io.WriteString(cmd.OutOrStdout(), script)
			return err
		},
	}
}

// completionSpec maps the cobra tree onto the go-utils command model, which
// renders the shell completions and the README's command reference.
func completionSpec(cmd *cobra.Command) completion.Command {
	spec := completion.Command{
		Name:            cmd.Name(),
		Description:     cmd.Short,
		Synopsis:        cmd.Short,
		LongDescription: cmd.Long,
	}
	cmd.LocalNonPersistentFlags().VisitAll(func(f *pflag.Flag) {
		spec.Flags = append(spec.Flags, completion.Flag{
			Name:        f.Name,
			Short:       f.Shorthand,
			Description: f.Usage,
			Value:       f.Value.Type() != "bool",
		})
	})
	children := cmd.Commands()
	sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
	for _, child := range children {
		if child.Hidden || child.Name() == "help" {
			continue
		}
		spec.Subcommands = append(spec.Subcommands, completionSpec(child))
	}
	return spec
}

func generateCommand() *cobra.Command {
	var root string
	var check bool
	cmd := &cobra.Command{
		Use:    "generate",
		Short:  "Generate the committed schemas and README sections",
		Args:   cobra.NoArgs,
		Hidden: true,
		RunE: func(*cobra.Command, []string) error {
			configSchema, err := config.Schema()
			if err != nil {
				return err
			}
			eventSchema, err := shared.Schema[gate.Envelope]("Gate hook event")
			if err != nil {
				return err
			}
			outcomeSchema, err := shared.Schema[gate.Outcome]("Gate decision")
			if err != nil {
				return err
			}
			providerSchema, err := provider.Schema()
			if err != nil {
				return err
			}
			files := map[string][]byte{
				filepath.Join(root, "schema", "config.schema.json"):   configSchema,
				filepath.Join(root, "schema", "event.schema.json"):    eventSchema,
				filepath.Join(root, "schema", "outcome.schema.json"):  outcomeSchema,
				filepath.Join(root, "schema", "provider.schema.json"): providerSchema,
			}
			for path, content := range files {
				if err := generated(path, content, check); err != nil {
					return err
				}
			}
			return generateReadme(root, check)
		},
	}
	cmd.Flags().StringVar(&root, "root", ".", "repository root")
	cmd.Flags().BoolVar(&check, "check", false, "fail when a generated file differs")
	return cmd
}

func generateReadme(root string, check bool) error {
	path := filepath.Join(root, "README.md")
	document, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	updated, err := completion.ReplaceSection(string(document), "commands", completion.Markdown(completionSpec(command())))
	if err != nil {
		return err
	}
	providers, err := providerReference(root)
	if err != nil {
		return err
	}
	updated, err = completion.ReplaceSection(updated, "providers", providers)
	if err != nil {
		return err
	}
	return generated(path, []byte(updated), check)
}

func providerReference(root string) (string, error) {
	manifests, err := filepath.Glob(filepath.Join(root, "extras", "*", "provider.yaml"))
	if err != nil {
		return "", err
	}
	sort.Strings(manifests)
	var b strings.Builder
	b.WriteString("\n| Provider | Does | Events |\n| --- | --- | --- |\n")
	for _, path := range manifests {
		file, err := os.Open(path)
		if err != nil {
			return "", err
		}
		manifest, err := provider.Decode(file, ".yaml")
		_ = file.Close()
		if err != nil {
			return "", fmt.Errorf("%s: %w", path, err)
		}
		events := manifest.Actions[gate.Action].Description
		fmt.Fprintf(&b, "| `%s` | %s | %s |\n", manifest.Name, manifest.Description, events)
	}
	b.WriteString("\n")
	return b.String(), nil
}

func generated(path string, content []byte, check bool) error {
	current, err := os.ReadFile(path)
	if check {
		if err != nil || string(current) != string(content) {
			return fmt.Errorf("%s is stale; run hack/generate.sh", path)
		}
		return nil
	}
	if err == nil && string(current) == string(content) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}
