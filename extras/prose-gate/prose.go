package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/roshbhatia/go-utils/paths"

	"github.com/roshbhatia/gate/pkg/gate"
)

const (
	// offEnv set to "off" switches every mode to a pass.
	offEnv = "PROSE_GATE"
	// stateEnv overrides where the per-session arming files go.
	stateEnv = "PROSE_GATE_STATE_DIR"
	// styleEnv is the CLI's fallback for --style.
	styleEnv = "PROSE_GATE_STYLE"

	// One style tell is a slip. A rule in block_on is exempt from this threshold.
	defaultMaxTells = 1
	// A teammate reports into the caller's context, so its report is the whole
	// cost of delegating. Anthropic sizes a useful one at 1,000 to 2,000 tokens;
	// 6 KiB is the top of that range.
	defaultMaxReportKiB = 6
)

// config is the step's args. Every rule and every injected text is here, so
// the binary decides nothing about style on its own.
type config struct {
	// style is the vale config. Without it, or without vale on PATH, check
	// passes everything and says so on stderr.
	style string
	// blockOn names the rules that record on one hit. A tool fingerprint
	// leaking into a reply is one such: a second occurrence adds no evidence.
	blockOn []string
	// maxTells is the count of alerts a reply carries before it is recorded.
	maxTells int
	// shape closes the recorded findings: what the model is told to send
	// instead. Empty leaves the findings alone.
	shape string
	// reminder is injected on UserPromptSubmit when due. Empty carries only
	// the recorded findings.
	reminder string
	// session is injected on SessionStart. Empty passes.
	session string
	// maxReportBytes is the teammate report budget.
	maxReportBytes int
	stateDir       string
}

func off() bool { return os.Getenv(offEnv) == "off" }

// configure reads the step's args over the defaults.
func configure(request gate.Request) config {
	return config{
		style:          request.String("style", ""),
		blockOn:        request.Strings("block_on"),
		maxTells:       request.Int("max_tells", defaultMaxTells),
		shape:          request.String("shape", ""),
		reminder:       request.String("reminder", ""),
		session:        request.String("session", ""),
		maxReportBytes: request.Int("max_report_kib", defaultMaxReportKiB) * 1024,
		stateDir:       stateDir(),
	}
}

func stateDir() string {
	if dir := os.Getenv(stateEnv); dir != "" {
		return dir
	}
	return filepath.Join(paths.StateHome(), "gate", "prose-gate")
}

// defaultModes routes an event with no args.mode.
var defaultModes = map[string]string{
	"Stop":             "check",
	"UserPromptSubmit": "remind",
	"SessionStart":     "session",
	"PostToolUse":      "report",
}

// Decide routes on args.mode, or on the event when no mode is given.
func Decide(request gate.Request) gate.Outcome {
	if off() {
		return gate.PassOutcome()
	}
	cfg := configure(request)
	mode := request.String("mode", defaultModes[request.Event.Event])
	switch mode {
	case "check":
		return cfg.check(request.Event)
	case "remind":
		return cfg.remind(request.Event)
	case "session":
		return cfg.sessionStart()
	case "report":
		return cfg.report(request.Event)
	}
	return gate.PassOutcome()
}

func note(text string) gate.Outcome {
	return gate.Outcome{Kind: gate.Context, Message: text}
}

// The reminder used to go in on every prompt. That is the one injection here
// that grows without bound: its size times the turn count, restating a rule the
// Stop gate already enforces deterministically. A rule stated twice is worse
// than a rule stated once, because the model spends tokens reconciling the two
// (OpenAI, GPT-5 prompting guide). So the reminder is armed rather than
// constant: it goes in on the first prompt of a session, and again only after
// the gate has recorded tells on a reply.
func (c config) armPath(session string) string {
	if c.stateDir == "" || session == "" || strings.ContainsAny(session, `/\`) {
		return ""
	}
	return filepath.Join(c.stateDir, session)
}

func writeMarker(path, text string) {
	if path == "" {
		return
	}
	if os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	_ = os.WriteFile(path, []byte(text), 0o644)
}

// spend reports whether the marker exists, and removes it.
func spend(path string) bool {
	if path == "" {
		return false
	}
	if _, err := os.Stat(path); err != nil {
		return false
	}
	_ = os.Remove(path)
	return true
}

// arm records that this session's last reply carried tells, so the next prompt
// carries the reminder again.
func (c config) arm(session string) { writeMarker(c.armPath(session), "armed\n") }

// findingsPath holds what check found, for remind to carry on the next prompt.
func (c config) findingsPath(session string) string {
	if path := c.armPath(session); path != "" {
		return path + ".findings"
	}
	return ""
}

// record keeps the tells of the reply just sent. check writes, remind reads.
func (c config) record(session, text string) { writeMarker(c.findingsPath(session), text) }

// recorded returns the tells check kept, and spends them.
func (c config) recorded(session string) string {
	path := c.findingsPath(session)
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	_ = os.Remove(path)
	return strings.TrimSpace(string(data))
}

// The escape word comes from bigskysoftware/be-terse, which drops its injection
// when a prompt ends in "noterse". Here the injection is only half the gate, so
// the word has to reach the Stop hook as well: a reminder the user opted out of,
// followed by a note about the style they opted out of, is worse than neither.
// remind writes the marker and check clears it, so the escape lasts one turn.
func (c config) escapePath(session string) string {
	if path := c.armPath(session); path != "" {
		return path + ".noterse"
	}
	return ""
}

// noterse reports whether the prompt's last word is the escape word.
func noterse(prompt string) bool {
	fields := strings.Fields(prompt)
	if len(fields) == 0 {
		return false
	}
	return strings.EqualFold(fields[len(fields)-1], "noterse")
}

func (c config) escape(session string) { writeMarker(c.escapePath(session), "noterse\n") }

// release reports whether this reply is exempt, and spends the exemption.
func (c config) release(session string) bool { return spend(c.escapePath(session)) }

// disarm reports whether the reminder is due, and clears the arming if it is.
// An unknown session is always due: injecting the reminder costs less than a
// reply in the wrong shape.
func (c config) disarm(session string) bool {
	path := c.armPath(session)
	if path == "" {
		return true
	}
	if spend(path) {
		return true
	}
	seen := path + ".seen"
	if _, err := os.Stat(seen); err == nil {
		return false
	}
	writeMarker(seen, "seen\n")
	return true
}

// One alert as vale reports it. The rule set lives in the style the step names,
// not here, so a rule change is a style edit rather than a rebuild.
type valeAlert struct {
	Check    string `json:"Check"`
	Message  string `json:"Message"`
	Severity string `json:"Severity"`
	Match    string `json:"Match"`
	Line     int    `json:"Line"`
	Span     []int  `json:"Span"`
}

// at is the alert's position, for deduplication. Two rules that own one span
// are one fault.
func (a valeAlert) at() [3]int {
	from, to := 0, 0
	if len(a.Span) > 0 {
		from = a.Span[0]
	}
	if len(a.Span) > 1 {
		to = a.Span[1]
	}
	return [3]int{a.Line, from, to}
}

// vale parses the reply as markdown, so a fenced block is skipped and a list is
// read as a list. A hand-rolled matcher stripped every bullet before it looked
// for tells, which made a bullet the one place a tell could hide.
//
// Every failure here returns no alerts. A gate that blocks because vale is
// missing costs the user a turn for a fault that is not in their reply.
func (c config) alerts(text string) []valeAlert {
	// Every path out of this function that is not "vale ran and found nothing"
	// says so. A gate that opens quietly is worse than no gate, because it is
	// trusted: one unsupported key on one rule already turned the whole check
	// into a pass with no sign of it.
	if c.style == "" {
		fmt.Fprintln(os.Stderr, "prose-gate: args.style is unset, so nothing was checked")
		return nil
	}
	binary, err := exec.LookPath("vale")
	if err != nil {
		fmt.Fprintln(os.Stderr, "prose-gate: vale is not on PATH, so nothing was checked")
		return nil
	}

	// --no-global is what makes the rule set the step's. Vale merges the user
	// config at ~/.vale.ini into every run, and a hand-written style under
	// ~/.local/share/vale/styles then replaced these rules wholesale: the gate
	// ran 12 foreign rules and reported their messages. Nothing in the output
	// said the rule set had changed.
	cmd := exec.Command(binary, "--config="+c.style, "--no-global", "--output=JSON", "--ext=.md", "--no-exit")
	cmd.Stdin = strings.NewReader(undirect(text))
	// A malformed rule makes vale lint nothing, which read as "no alerts"
	// silently disabled every check: one invalid key on one rule turned the
	// whole gate into a pass. Measured on vale 3.17.1 with an unsupported
	// `tokenIgnores` key.
	//
	// `--no-exit` suppresses only the alert-driven status. A config error still
	// exits 2, so the exit code is the signal and not the body.
	//
	// The reply still goes through, because a broken rule set is the style's
	// fault and not the reader's. The warning is what makes it visible instead
	// of silent.
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "prose-gate: vale could not run, so nothing was checked: %s\n", valeError(err, out))
		return nil
	}

	var byFile map[string][]valeAlert
	if json.Unmarshal(out, &byFile) != nil {
		fmt.Fprintf(os.Stderr, "prose-gate: vale returned no alert set, so nothing was checked: %s\n",
			firstLine(strings.TrimSpace(string(out))))
		return nil
	}
	var all []valeAlert
	for _, list := range byFile {
		all = append(all, list...)
	}
	return all
}

// findings decides whether the reply is recorded. It is separate from the fix
// list, because the record is judged on the raw reply and the fix list is built
// from what the applier could not repair.
func (c config) findings(text string) []valeAlert {
	found := oneAlertPerSpan(c.alerts(text))
	if !c.blocks(found) {
		return nil
	}
	return found
}

func (c config) blocks(found []valeAlert) bool {
	if len(found) > c.maxTells {
		return true
	}
	for _, alert := range found {
		if slices.Contains(c.blockOn, alert.Check) {
			return true
		}
	}
	return false
}

// reason is the whole message the model gets back. It leads with the lines the
// gate already rewrote, because those need no judgement and reading them is
// cheaper than deriving them again. What is left needs a person's decision, so
// it is listed second and the step's shape text comes last.
func (c config) reason(fixes []correction, manual []valeAlert) string {
	var b strings.Builder
	b.WriteString("Your last reply read like agent prose. It was sent as written.\n")

	if len(fixes) > 0 {
		b.WriteString("\nThese lines had a mechanical fix. Write the next reply the second way:\n\n")
		for i, f := range fixes {
			if i == maxCorrections {
				fmt.Fprintf(&b, "  ... and %d more line(s), same rules\n", len(fixes)-maxCorrections)
				break
			}
			fmt.Fprintf(&b, "  line %d: %s\n", f.Line, f.Text)
		}
	}

	if len(manual) > 0 {
		fmt.Fprintf(&b, "\n%d faults have no mechanical rewrite. Fix every one before sending:\n\n", len(manual))
		for _, g := range groupByRule(manual) {
			fmt.Fprintf(&b, "  - %s\n", g)
		}
	}

	if shape := strings.TrimSpace(c.shape); shape != "" {
		b.WriteString("\n" + shape)
	}
	return b.String()
}

// check is the whole Stop decision. It records and never blocks.
func (c config) check(event gate.Envelope) gate.Outcome {
	// The rewrite gets one pass. A gate that fires on its own correction is a
	// loop the user cannot interrupt.
	if event.StopActive || event.LastMessage == "" {
		return gate.PassOutcome()
	}
	if c.release(event.Session) {
		return gate.PassOutcome()
	}

	if len(c.findings(event.LastMessage)) == 0 {
		return gate.PassOutcome()
	}

	// The gate used to block here and hand the corrections back, which cost a
	// rewrite turn per slip. A Stop hook has no passive channel: a context note
	// on Stop continues the turn just as a block does. So the findings are
	// recorded, and remind carries them into the next prompt, where a note is
	// a note. The reply the user already read stays as it was.
	fixes, manual := corrections(c.style, event.LastMessage)
	c.record(event.Session, c.reason(fixes, manual))
	c.arm(event.Session)
	return gate.PassOutcome()
}

// remind carries the recorded findings and the step's reminder into the next
// prompt. Claude Code re-states a built-in output style on every turn and a
// custom one never, so the reminder is that missing per-turn line.
func (c config) remind(event gate.Envelope) gate.Outcome {
	if noterse(event.Prompt) {
		c.escape(event.Session)
		return gate.PassOutcome()
	}
	held := c.recorded(event.Session)
	if !c.disarm(event.Session) && held == "" {
		return gate.PassOutcome()
	}
	text := strings.TrimSpace(c.reminder)
	if held != "" && text != "" {
		text = held + "\n\n" + text
	} else if held != "" {
		text = held
	}
	if text == "" {
		return gate.PassOutcome()
	}
	return note(text)
}

// sessionStart injects the step's session text: the rules a fresh or
// compacted session has no other way to learn.
func (c config) sessionStart() gate.Outcome {
	text := strings.TrimSpace(c.session)
	if text == "" {
		return gate.PassOutcome()
	}
	return note(text)
}

// agentResponse is the PostToolUse tool_response of the Agent tool. The
// teammate's final text arrives as content blocks.
type agentResponse struct {
	Status  string `json:"status"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// A teammate's report is the entire cost of delegating: the caller pays for it
// in the window the delegation was meant to protect. Size is the only thing
// worth gating here, because a report is data and the style rules are not.
//
// This used to block SubagentStop and make the teammate rewrite. A critic whose
// evidence ran long then spent a turn shortening it, and the loop that spawned
// it ran longer. The report now lands as written and the caller gets the note,
// which is where the next delegation is shaped.
func (c config) report(event gate.Envelope) gate.Outcome {
	var response agentResponse
	if len(event.Response) == 0 || json.Unmarshal(event.Response, &response) != nil || response.Status == "async_launched" {
		return gate.PassOutcome()
	}
	size := 0
	for _, block := range response.Content {
		size += len(block.Text)
	}
	if size <= c.maxReportBytes {
		return gate.PassOutcome()
	}
	return note(fmt.Sprintf(`prose-gate: that teammate report is %d KiB and the budget is %d KiB. It landed whole in your context. Next time ask the teammate for the answer in one or two sentences, the evidence as file:line pointers, and what it could not determine.`,
		size/1024, c.maxReportBytes/1024))
}

// cliConfig reads the CLI's flags. lint and fix write for a person, not for a
// hook, so the thresholds are flags where the provider reads args.
func cliConfig(name string, args []string) (config, []string, error) {
	cfg := config{style: os.Getenv(styleEnv), maxTells: defaultMaxTells}
	var rest []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--style", "--max-tells", "--block-on":
			if i+1 >= len(args) {
				return cfg, nil, fmt.Errorf("%s needs a value", args[i])
			}
			value := args[i+1]
			i++
			switch args[i-1] {
			case "--style":
				cfg.style = value
			case "--block-on":
				cfg.blockOn = append(cfg.blockOn, value)
			case "--max-tells":
				n, err := strconv.Atoi(value)
				if err != nil {
					return cfg, nil, errors.New("--max-tells must be an integer")
				}
				cfg.maxTells = n
			}
		default:
			rest = append(rest, args[i])
		}
	}
	if cfg.style == "" {
		return cfg, nil, fmt.Errorf("%s: --style or %s is required", name, styleEnv)
	}
	return cfg, rest, nil
}

func lint(args []string, stdin io.Reader) int {
	cfg, rest, err := cliConfig("prose-gate lint", args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "prose-gate lint: %v\n", err)
		return 2
	}
	if len(rest) > 0 {
		fmt.Fprintf(os.Stderr, "prose-gate lint: unknown argument: %s\n", rest[0])
		return 2
	}
	data, err := io.ReadAll(stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "prose-gate lint: %v\n", err)
		return 2
	}
	// lint reports every alert. A recorded note costs the next prompt bytes, so
	// check stays quiet until there is more than max_tells, and the two counts
	// differ on purpose. The same dedupe `findings` applies, so `lint` reports
	// the number that decides it.
	all := oneAlertPerSpan(cfg.alerts(string(data)))
	for _, a := range all {
		fmt.Printf("  - %s: %q (line %d) [%s]\n", a.Message, a.Match, a.Line, a.Check)
	}
	if !cfg.blocks(all) {
		fmt.Printf("%d alerts; check records above %d, so this passes\n", len(all), cfg.maxTells)
		return 0
	}
	fmt.Printf("%d alerts; check records this for the next prompt\n", len(all))
	return 1
}

// firstLine keeps the warning to one line. Vale's config errors are one line
// each and the first names the rule.
func firstLine(text string) string {
	if at := strings.IndexByte(text, '\n'); at >= 0 {
		return text[:at]
	}
	return text
}

// valeError reads the reason out of vale's own error object. On a config error
// vale writes one JSON object to stderr, whose Text field carries the E-code
// and the explanation over several lines. The first line of that field is the
// one the operator needs.
func valeError(err error, stdout []byte) string {
	body := stdout
	var exit *exec.ExitError
	if errors.As(err, &exit) && len(exit.Stderr) > 0 {
		body = exit.Stderr
	}
	// Text already opens with the code, so the Code field would only repeat it.
	var held struct {
		Text string `json:"Text"`
	}
	if json.Unmarshal(body, &held) == nil && held.Text != "" {
		return firstLine(strings.TrimSpace(held.Text))
	}
	return fmt.Sprintf("%v: %s", err, firstLine(strings.TrimSpace(string(body))))
}

// undirect removes vale's own inline control comments. The gate reads the text
// the model wrote, and vale obeys a directive it finds there: one
// `<!-- vale off -->` at the top of a reply took 4 alerts to 0, and an HTML
// comment renders invisibly, so nothing showed. A gate the governed party can
// switch off is not a gate.
//
// The comment is dropped rather than escaped, because a reply that names a
// directive on purpose does so in a fence, and a fence is not what vale reads
// a directive from.
var valeDirective = regexp.MustCompile(`(?is)<!--\s*vale\b.*?-->`)

func undirect(text string) string {
	return valeDirective.ReplaceAllString(text, "")
}

// oneAlertPerSpan keeps the first alert on each span. Two rules can own one
// slip, and counting both spent two tells on one fault: every em-dash in a
// heading matched a heading rule and a dash rule on the same span, so it
// always reached maxTells and always recorded. Measured over 1805 real replies,
// all 81 heading-dash alerts were such a pair.
//
// The first alert wins because `alerts` returns vale's order, and `fix`
// already breaks a same-span tie by rule name for the same reason.
func oneAlertPerSpan(in []valeAlert) []valeAlert {
	seen := make(map[[3]int]bool, len(in))
	out := make([]valeAlert, 0, len(in))
	for _, one := range in {
		if seen[one.at()] {
			continue
		}
		seen[one.at()] = true
		out = append(out, one)
	}
	return out
}

// groupByRule folds the manual list to one line per rule, with every match on
// it. Seventeen faults over nine rules printed seventeen lines, and only the
// first three survived the trip back to the model, so a rewrite fixed three and
// the next attempt recorded the rest. One line per rule is the whole set in a
// third of the bytes, and it reads as one instruction rather than as a list of
// incidents.
//
// Rule order follows first appearance, so the reply is fixed top down.
func groupByRule(in []valeAlert) []string {
	order := []string{}
	byRule := map[string][]string{}
	message := map[string]string{}
	for _, a := range in {
		if _, seen := byRule[a.Check]; !seen {
			order = append(order, a.Check)
			message[a.Check] = a.Message
		}
		hit := a.Match
		if hit == "" {
			hit = fmt.Sprintf("line %d", a.Line)
		}
		byRule[a.Check] = append(byRule[a.Check], fmt.Sprintf("%q", hit))
	}
	out := make([]string, 0, len(order))
	for _, rule := range order {
		hits := byRule[rule]
		line := message[rule]
		if len(hits) > 1 {
			line = fmt.Sprintf("%s (%d)", line, len(hits))
		}
		out = append(out, line+": "+strings.Join(hits, ", "))
	}
	return out
}
