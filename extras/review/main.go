// review is the ledger of one adversarial review: it opens a pass, judges the
// mediator's verdicts, reopens on a changed tree, and reports the state that
// the review-gate provider enforces.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/roshbhatia/gate/extras/internal/ledger"
	"github.com/roshbhatia/gate/extras/internal/policy"
)

func main() {
	if err := command().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func cwd() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return dir
}

func command() *cobra.Command {
	root := &cobra.Command{
		Use:           "review",
		Short:         "Keep the ledger of one adversarial review",
		SilenceUsage:  true,
		SilenceErrors: true,
		Long: `review records one adversarial review as data: the tier the diff earned, the
pass being run, and what the judge let through. The review-gate provider reads
the same file, so a critic cannot spawn past what the ledger allows.

The loop is short by construction. One pass: critics, then a mediator that
tries to disprove each finding, then ` + "`review judge`" + `. The author fixes what
survived. ` + "`review reopen`" + ` runs one more pass only when the tree changed and
the cap allows. There is no third pass; the owner reads the open list.`,
	}
	var change, base string
	open := &cobra.Command{
		Use:   "open",
		Short: "Start a review: measure the diff, pick the tier, record pass 1",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			pol, err := policy.Load()
			if err != nil {
				return err
			}
			if change == "" {
				change = filepath.Base(cwd())
			}
			record, err := ledger.Begin(cwd(), change, base, pol)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), record.Summary())
			return err
		},
	}
	open.Flags().StringVar(&change, "change", "", "what is under review, such as an openspec change directory")
	open.Flags().StringVar(&base, "base", "", "the revision the change is measured against; the merge base with the default branch when omitted")

	judge := &cobra.Command{
		Use:   "judge [file]",
		Short: "Record the mediator's verdicts and move the review to its next state",
		Long: `Reads a JSON list of findings, each {id, lens, verdict, severity, scenario,
disproof_attempt, evidence: [{path, line}]}. An ACCEPT or REFRAME with no
scenario, no disproof attempt, or an anchor that does not resolve to a line in
a changed file is downgraded to REJECT and counted as dropped. Nits do not
count as actionable. A DEFER or a blocking finding hands the review back to the
owner. The default file is the current pass's file under the state directory.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			record, ok, err := ledger.Load(cwd())
			if err != nil {
				return err
			}
			if !ok {
				return errors.New("no review is open; run `review open` first")
			}
			path := ledger.PassFile(cwd(), len(record.Passes))
			if len(args) == 1 {
				path = args[0]
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			var findings []ledger.Finding
			if err := json.Unmarshal(data, &findings); err != nil {
				return fmt.Errorf("decode %s: %w", path, err)
			}
			record, err = ledger.Judge(cwd(), &record, findings)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), record.Summary())
			return err
		},
	}

	reopen := &cobra.Command{
		Use:   "reopen",
		Short: "Start the next pass, when the tree changed and the cap allows",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			record, ok, err := ledger.Load(cwd())
			if err != nil {
				return err
			}
			if !ok {
				return errors.New("no review is open")
			}
			record, err = ledger.Reopen(cwd(), &record)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), record.Summary())
			return err
		},
	}

	var asMarkdown, asJSON bool
	status := &cobra.Command{
		Use:   "status",
		Short: "Print where the review stands",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			record, ok, err := ledger.Load(cwd())
			if err != nil {
				return err
			}
			if !ok {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "no review is open")
				return err
			}
			switch {
			case asJSON:
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(record)
			case asMarkdown:
				_, err = fmt.Fprint(cmd.OutOrStdout(), record.Markdown())
			default:
				_, err = fmt.Fprintln(cmd.OutOrStdout(), record.Summary())
			}
			return err
		},
	}
	status.Flags().BoolVar(&asMarkdown, "md", false, "print the block that pastes into review.md")
	status.Flags().BoolVar(&asJSON, "json", false, "print the whole ledger as JSON")

	halt := &cobra.Command{
		Use:   "halt",
		Short: "Stop the review as the owner; open findings stay recorded",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			record, ok, err := ledger.Load(cwd())
			if err != nil {
				return err
			}
			if !ok {
				return errors.New("no review is open")
			}
			record, err = ledger.Halt(cwd(), &record)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), record.Summary())
			return err
		},
	}

	closeCmd := &cobra.Command{
		Use:   "close",
		Short: "Remove the ledger",
		Args:  cobra.NoArgs,
		RunE:  func(*cobra.Command, []string) error { return ledger.Remove(cwd()) },
	}

	policyCmd := &cobra.Command{
		Use:   "policy",
		Short: "Print the effective review policy",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			pol, err := policy.Load()
			if err != nil {
				return err
			}
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(pol)
		},
	}
	root.AddCommand(open, judge, reopen, status, halt, closeCmd, policyCmd)
	return root
}
