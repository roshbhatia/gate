// Package ledger is the review record a gate keeps for one change: the tier
// the diff earned, the pass being run, and what the judge let through.
//
// The record replaces a round loop the model used to run from prose. Every
// bound that was a sentence is a field here: the tier decides how many critics
// run, the judge drops a finding it cannot anchor, the pass count is capped,
// and a second pass needs a changed tree.
package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	gitutil "github.com/roshbhatia/go-utils/git"

	"github.com/roshbhatia/gate/extras/internal/state"
)

// State is where a review stands.
type State string

const (
	// Open is a pass in progress: critics may spawn, the judge has not ruled.
	Open State = "OPEN"
	// Revise is a judged pass with actionable findings the author owes fixes for.
	Revise State = "REVISE"
	// Clean is a judged pass with nothing actionable left. Model evidence, not approval.
	Clean State = "CLEAN"
	// Handback is a pass the owner must read: a DEFER or a blocking finding.
	Handback State = "HANDBACK"
	// Capped is the last allowed pass, judged, with actionable findings still open.
	Capped State = "CAPPED"
	// Halted is the owner stopping the review.
	Halted State = "HALTED"
	// NotRun is a change whose tier needs no critic.
	NotRun State = "NOT_RUN"
)

// Terminal reports whether the state ends the review.
func (s State) Terminal() bool {
	switch s {
	case Clean, Handback, Capped, Halted, NotRun:
		return true
	}
	return false
}

// Verdict is the judge's ruling on one finding.
type Verdict string

const (
	Accept  Verdict = "ACCEPT"
	Reject  Verdict = "REJECT"
	Reframe Verdict = "REFRAME"
	Defer   Verdict = "DEFER"
)

// Severity is how much a finding weighs.
type Severity string

const (
	Blocking Severity = "blocking"
	Warn     Severity = "warn"
	Nit      Severity = "nit"
)

// Anchor is where a finding lives.
type Anchor struct {
	Path string `json:"path"`
	Line int    `json:"line"`
}

// Finding is one judged critic objection.
type Finding struct {
	ID              string   `json:"id"`
	Lens            string   `json:"lens,omitempty"`
	Verdict         Verdict  `json:"verdict"`
	Severity        Severity `json:"severity,omitempty"`
	Scenario        string   `json:"scenario,omitempty"`
	DisproofAttempt string   `json:"disproof_attempt,omitempty"`
	Evidence        []Anchor `json:"evidence,omitempty"`
	// Reason is filled by the judge when it downgrades a finding.
	Reason string `json:"reason,omitempty"`
}

// Pass is one critic-and-judge cycle.
type Pass struct {
	N          int       `json:"n"`
	Tree       string    `json:"tree"`
	Critics    int       `json:"critics"`
	Judged     bool      `json:"judged"`
	Actionable int       `json:"actionable"`
	Deferred   int       `json:"deferred"`
	Blocking   int       `json:"blocking"`
	Dropped    int       `json:"dropped"`
	Findings   []Finding `json:"findings,omitempty"`
	At         time.Time `json:"at"`
}

// Record is the ledger file.
type Record struct {
	Change    string    `json:"change"`
	Base      string    `json:"base"`
	Tier      string    `json:"tier"`
	TierRule  string    `json:"tier_rule"`
	Lines     int       `json:"lines"`
	Files     []string  `json:"files"`
	Critics   int       `json:"critics"`
	PassesMax int       `json:"passes_max"`
	State     State     `json:"state"`
	Passes    []Pass    `json:"passes"`
	OpenedAt  time.Time `json:"opened_at"`
}

// Tier is one row of the policy.
type Tier struct {
	MaxLines int `json:"max_lines" yaml:"max_lines"`
	MaxFiles int `json:"max_files" yaml:"max_files"`
	Critics  int `json:"critics" yaml:"critics"`
}

// Policy is the owner's review policy, decided once in config.
type Policy struct {
	Tiers          map[string]Tier `json:"tiers" yaml:"tiers"`
	Sensitive      []string        `json:"sensitive" yaml:"sensitive"`
	ReadonlyAgents []string        `json:"readonly_agents" yaml:"readonly_agents"`
	PassesMax      int             `json:"passes_max" yaml:"passes_max"`
}

// DefaultPolicy is Cloudflare's tiering with neutral sensitive paths. An
// installation adds its own permission surfaces to Sensitive.
func DefaultPolicy() Policy {
	return Policy{
		Tiers: map[string]Tier{
			"trivial": {MaxLines: 10, MaxFiles: 20, Critics: 0},
			"lite":    {MaxLines: 100, MaxFiles: 20, Critics: 1},
			"full":    {Critics: 3},
		},
		Sensitive:      []string{"**/secrets/**", "**/.github/workflows/**", "**/auth/**", "**/iam/**", "**/permission*"},
		ReadonlyAgents: []string{"Explore", "review-mediator"},
		PassesMax:      2,
	}
}

// Validate checks a policy.
func (p Policy) Validate() error {
	var problems []error
	if p.PassesMax < 1 {
		problems = append(problems, errors.New("passes_max must be at least 1"))
	}
	if _, ok := p.Tiers["full"]; !ok {
		problems = append(problems, errors.New("tiers must define full"))
	}
	for name, tier := range p.Tiers {
		if tier.Critics < 0 {
			problems = append(problems, fmt.Errorf("tier %s: critics must not be negative", name))
		}
	}
	return errors.Join(problems...)
}

// Path is the ledger file for a working directory.
func Path(cwd string) string { return filepath.Join(state.Dir(cwd), "review.json") }

// PassFile is where the judge's verdicts for a pass are written.
func PassFile(cwd string, n int) string {
	return filepath.Join(state.Dir(cwd), "review", fmt.Sprintf("pass-%d.json", n))
}

// Load reads the ledger. ok is false when no review is open.
func Load(cwd string) (Record, bool, error) {
	data, err := os.ReadFile(Path(cwd))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Record{}, false, nil
		}
		return Record{}, false, err
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		return Record{}, false, fmt.Errorf("decode %s: %w", Path(cwd), err)
	}
	return record, true, nil
}

// Save writes the ledger.
func Save(cwd string, record Record) error {
	path := Path(cwd)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Remove closes the ledger.
func Remove(cwd string) error {
	if err := os.Remove(Path(cwd)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// git runs one command with inherited repository state removed, so a GIT_DIR
// from the harness cannot retarget a command that already names its tree.
func git(cwd string, args ...string) (string, error) {
	out, err := gitutil.Output(cwd, args...)
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(out), nil
}

// DefaultBase is the merge base with the default branch, or the first commit
// when there is no default branch to merge with.
func DefaultBase(cwd string) (string, error) {
	for _, ref := range []string{"origin/HEAD", "origin/main", "main", "origin/master", "master"} {
		if base, err := git(cwd, "merge-base", ref, "HEAD"); err == nil && base != "" {
			return base, nil
		}
	}
	return git(cwd, "rev-list", "--max-parents=0", "HEAD")
}

// Tree fingerprints what is under review: the committed tree plus the
// working-tree status, so an uncommitted fix still counts as a change.
func Tree(cwd string) (string, error) {
	tree, err := git(cwd, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return "", err
	}
	status, err := git(cwd, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return "", err
	}
	// The ledger itself lives under the state directory, often inside the
	// repository. Its own writes must not read as a changed tree.
	var kept []string
	for _, line := range strings.Split(status, "\n") {
		if len(line) > 3 && inState(cwd, strings.TrimSpace(line[3:])) {
			continue
		}
		kept = append(kept, line)
	}
	diff, _ := git(cwd, "diff", "HEAD")
	sum := sha256.Sum256([]byte(tree + "\n" + strings.Join(kept, "\n") + "\n" + diff))
	return hex.EncodeToString(sum[:8]), nil
}

// inState reports whether a repository-relative path is under the state
// directory.
func inState(cwd, path string) bool {
	rel, err := filepath.Rel(cwd, state.Dir(cwd))
	if err != nil || strings.HasPrefix(rel, "..") {
		return false
	}
	return path == rel || strings.HasPrefix(path, rel+"/")
}

// Diff measures the change since base: changed lines and touched files,
// including the working tree.
func Diff(cwd, base string) (int, []string, error) {
	numstat, err := git(cwd, "diff", "--numstat", base)
	if err != nil {
		return 0, nil, err
	}
	lines := 0
	files := map[string]bool{}
	for _, row := range strings.Split(numstat, "\n") {
		fields := strings.Fields(row)
		if len(fields) < 3 {
			continue
		}
		added, _ := strconv.Atoi(fields[0])
		removed, _ := strconv.Atoi(fields[1])
		lines += added + removed
		files[fields[len(fields)-1]] = true
	}
	untracked, _ := git(cwd, "ls-files", "--others", "--exclude-standard")
	for _, path := range strings.Split(untracked, "\n") {
		if path = strings.TrimSpace(path); path != "" && !inState(cwd, path) {
			files[path] = true
			if data, err := os.ReadFile(filepath.Join(cwd, path)); err == nil {
				lines += strings.Count(string(data), "\n")
			}
		}
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return lines, names, nil
}

// Classify picks the tier for a diff and says which rule decided it.
func (p Policy) Classify(lines int, files []string) (string, string, Tier) {
	for _, path := range files {
		for _, pattern := range p.Sensitive {
			if globMatch(pattern, path) {
				return "full", fmt.Sprintf("%s matches sensitive pattern %s", path, pattern), p.Tiers["full"]
			}
		}
	}
	// Smallest tier whose bounds hold, in ascending order of max_lines.
	names := make([]string, 0, len(p.Tiers))
	for name := range p.Tiers {
		if name != "full" {
			names = append(names, name)
		}
	}
	sort.Slice(names, func(i, j int) bool { return p.Tiers[names[i]].MaxLines < p.Tiers[names[j]].MaxLines })
	for _, name := range names {
		tier := p.Tiers[name]
		if lines <= tier.MaxLines && (tier.MaxFiles == 0 || len(files) <= tier.MaxFiles) {
			return name, fmt.Sprintf("%d lines and %d files within %s (max %d lines, %d files)", lines, len(files), name, tier.MaxLines, tier.MaxFiles), tier
		}
	}
	return "full", fmt.Sprintf("%d lines and %d files exceed every smaller tier", lines, len(files)), p.Tiers["full"]
}

// globMatch supports `**` across separators, which filepath.Match does not.
func globMatch(pattern, path string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(path, "/"))
}

func matchSegments(pattern, path []string) bool {
	for len(pattern) > 0 {
		if pattern[0] == "**" {
			if len(pattern) == 1 {
				return true
			}
			for i := 0; i <= len(path); i++ {
				if matchSegments(pattern[1:], path[i:]) {
					return true
				}
			}
			return false
		}
		if len(path) == 0 {
			return false
		}
		if ok, _ := filepath.Match(pattern[0], path[0]); !ok {
			return false
		}
		pattern, path = pattern[1:], path[1:]
	}
	return len(path) == 0
}

// Begin starts a review: measures the diff, picks the tier, records pass 1.
func Begin(cwd, change, base string, policy Policy) (Record, error) {
	if err := policy.Validate(); err != nil {
		return Record{}, err
	}
	if existing, ok, err := Load(cwd); err != nil {
		return Record{}, err
	} else if ok && !existing.State.Terminal() {
		return Record{}, fmt.Errorf("a review of %s is already %s; judge it, halt it, or close it first", existing.Change, existing.State)
	}
	if base == "" {
		var err error
		if base, err = DefaultBase(cwd); err != nil {
			return Record{}, err
		}
	}
	lines, files, err := Diff(cwd, base)
	if err != nil {
		return Record{}, err
	}
	tree, err := Tree(cwd)
	if err != nil {
		return Record{}, err
	}
	tier, rule, row := policy.Classify(lines, files)
	record := Record{
		Change: change, Base: base, Tier: tier, TierRule: rule, Lines: lines, Files: files,
		Critics: row.Critics, PassesMax: policy.PassesMax, State: Open, OpenedAt: time.Now().UTC(),
		Passes: []Pass{{N: 1, Tree: tree, At: time.Now().UTC()}},
	}
	if row.Critics == 0 {
		record.State = NotRun
	}
	return record, Save(cwd, record)
}

// Current is the pass in progress.
func (r *Record) Current() *Pass {
	if len(r.Passes) == 0 {
		return nil
	}
	return &r.Passes[len(r.Passes)-1]
}

// Judge reads the mediator's verdicts, validates each anchor, and moves the
// review to its next state. A finding the judge cannot anchor is downgraded to
// REJECT rather than argued about: DoorDash's rule, a comment with no action
// point is dropped.
func Judge(cwd string, record *Record, findings []Finding) (Record, error) {
	pass := record.Current()
	if pass == nil || record.State != Open {
		return *record, fmt.Errorf("review is %s; there is no open pass to judge", record.State)
	}
	changed := map[string]bool{}
	for _, file := range record.Files {
		changed[file] = true
	}
	pass.Findings = nil
	pass.Actionable, pass.Deferred, pass.Blocking, pass.Dropped = 0, 0, 0, 0
	for _, finding := range findings {
		if finding.ID == "" {
			return *record, errors.New("every finding needs an id")
		}
		switch finding.Verdict {
		case Accept, Reframe:
			if reason := unanchored(cwd, changed, finding); reason != "" {
				finding.Reason = reason
				finding.Verdict = Reject
				pass.Dropped++
				break
			}
			if finding.Severity != Nit {
				pass.Actionable++
			}
			if finding.Severity == Blocking {
				pass.Blocking++
			}
		case Defer:
			pass.Deferred++
		case Reject:
		default:
			return *record, fmt.Errorf("finding %s: verdict %q is not ACCEPT, REJECT, REFRAME, or DEFER", finding.ID, finding.Verdict)
		}
		pass.Findings = append(pass.Findings, finding)
	}
	pass.Judged = true
	switch {
	case pass.Deferred > 0 || pass.Blocking > 0:
		record.State = Handback
	case pass.Actionable == 0:
		record.State = Clean
	case pass.N >= record.PassesMax:
		record.State = Capped
	default:
		record.State = Revise
	}
	return *record, Save(cwd, *record)
}

// unanchored explains why an ACCEPT or REFRAME cannot stand, or returns "".
func unanchored(cwd string, changed map[string]bool, finding Finding) string {
	if strings.TrimSpace(finding.Scenario) == "" {
		return "no failing scenario"
	}
	if strings.TrimSpace(finding.DisproofAttempt) == "" {
		return "no disproof attempt recorded"
	}
	if len(finding.Evidence) == 0 {
		return "no file:line evidence"
	}
	for _, anchor := range finding.Evidence {
		full := anchor.Path
		if !filepath.IsAbs(full) {
			full = filepath.Join(cwd, anchor.Path)
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return fmt.Sprintf("evidence %s does not exist", anchor.Path)
		}
		if anchor.Line < 1 || anchor.Line > strings.Count(string(data), "\n")+1 {
			return fmt.Sprintf("evidence %s:%d is out of range", anchor.Path, anchor.Line)
		}
		if !changed[anchor.Path] {
			return fmt.Sprintf("evidence %s is not in the change", anchor.Path)
		}
	}
	return ""
}

// Reopen starts the next pass. It needs a judged pass in REVISE, a changed
// tree, and room under the cap.
func Reopen(cwd string, record *Record) (Record, error) {
	pass := record.Current()
	if pass == nil || record.State != Revise {
		return *record, fmt.Errorf("review is %s; only a REVISE pass reopens", record.State)
	}
	if pass.N >= record.PassesMax {
		record.State = Capped
		_ = Save(cwd, *record)
		return *record, fmt.Errorf("pass %d was the last allowed; the review is CAPPED", pass.N)
	}
	tree, err := Tree(cwd)
	if err != nil {
		return *record, err
	}
	if tree == pass.Tree {
		return *record, errors.New("the tree has not changed since the last pass; fix the accepted findings first")
	}
	record.Passes = append(record.Passes, Pass{N: pass.N + 1, Tree: tree, At: time.Now().UTC()})
	record.State = Open
	return *record, Save(cwd, *record)
}

// Halt is the owner stopping the review.
func Halt(cwd string, record *Record) (Record, error) {
	record.State = Halted
	return *record, Save(cwd, *record)
}

// Summary is the one-line status.
func (r Record) Summary() string {
	trend := make([]string, 0, len(r.Passes))
	for _, pass := range r.Passes {
		if pass.Judged {
			trend = append(trend, strconv.Itoa(pass.Actionable))
		} else {
			trend = append(trend, "?")
		}
	}
	pass := len(r.Passes)
	return fmt.Sprintf("review %s: %s, tier %s (%s), pass %d/%d, actionable per pass [%s]",
		r.State, r.Change, r.Tier, r.TierRule, pass, r.PassesMax, strings.Join(trend, ", "))
}

// Markdown is the block that pastes into review.md.
func (r Record) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "State: %s\nTier: %s (%s)\nPasses run: %d of %d\n", r.State, r.Tier, r.TierRule, len(r.Passes), r.PassesMax)
	trend := []string{}
	for _, pass := range r.Passes {
		if pass.Judged {
			trend = append(trend, strconv.Itoa(pass.Actionable))
		}
	}
	fmt.Fprintf(&b, "Actionable-finding trend: %s\n", strings.Join(trend, ", "))
	open := []string{}
	for _, pass := range r.Passes {
		for _, finding := range pass.Findings {
			if finding.Verdict == Accept || finding.Verdict == Reframe || finding.Verdict == Defer {
				open = append(open, fmt.Sprintf("- %s (%s, %s): %s", finding.ID, finding.Verdict, finding.Severity, finding.Scenario))
			}
		}
	}
	if r.State == Clean || r.State == NotRun {
		b.WriteString("Open findings: none\n")
	} else if len(open) > 0 {
		b.WriteString("Open findings:\n" + strings.Join(open, "\n") + "\n")
	}
	return b.String()
}
