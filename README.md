# gate

`gate` runs the hook chain a coding-agent harness declared and answers in the
harness's own shape. It is the one enforced layer: skills advise, scripts hide
plumbing, and a gate provider can say no.

```
harness ──hook event──▶ gate hook ──▶ provider ─▶ provider ─▶ provider
                          │              deny        allow      context
                          ◀── merged decision, one JSON line in the log
```

A harness calls one command per event. gate normalizes the payload, runs every
provider configured for that event whose match covers the tool, and merges the
decisions with fixed precedence: the first deny or block wins, a rewritten
input feeds the next provider, and every note reaches the model. Each call is
one line in `~/.local/state/gate/decisions.jsonl`, keyed by session so
[traces](https://github.com/roshbhatia/traces) can show it beside the turn.
A bound harness also records `orc_session` and `orc_scope`, so a decision
joins an orc checkpoint without reading orc's bind table.

Providers are executables with a
[provider/v1](https://github.com/roshbhatia/go-utils/tree/main/provider)
manifest, the same contract `ask`, `changes`, `traces`, and `orc` use. A gate
provider implements one action, `gate.decide`: it reads the event and the
step's arguments, and answers `pass`, `allow` (optionally with a rewritten
input), `deny`, `block`, or `context`. `pkg/gate.Serve` is the whole provider
side in Go.

## Install

```bash
# Core only. Bring your own providers.
nix profile install github:roshbhatia/gate#gate

# Core plus every provider in extras/.
nix profile install github:roshbhatia/gate#full
```

## Configure

`~/.config/gate/config.yaml`, or `$GATE_CONFIG`. Chains are keyed by hook
event; a step's `match` is a regular expression over the tool name.

```yaml
version: gate.config/v1
defaults: { timeout: 2s, on_rewrite_unsupported: deny }
chains:
  PreToolUse:
    - { provider: bash-guard,  match: "^Bash$", args: { rules: /etc/gate/deny-rules.json } }
    - { provider: nix-guard,   match: "^(Edit|Write|NotebookEdit)$" }
    - { provider: read-router, match: "^Read$" }
    - { provider: review-gate, match: "^(Agent|Task)$" }
  PostToolUse:
    - { provider: lint-gate,   match: "^(Edit|Write|MultiEdit)$" }
    - { provider: review-gate, match: "^(Agent|Task)$" }
  UserPromptSubmit:
    - { provider: review-gate }
  SubagentStart:
    - { provider: review-gate }
  Stop:
    - { provider: loop-gate }
```

Then one hook entry per event in the harness. Claude Code and Codex read hook
JSON; Gemini reads an exit code:

```json
{ "PreToolUse": [{ "matcher": "", "hooks": [{ "type": "command", "command": "gate hook --harness claude --event PreToolUse" }] }] }
```

`gate config validate` loads the file and every referenced manifest. Run it at
build time: a hook that finds a broken config at runtime exits 1, and a hook
that cannot read its policy must not read as a pass.

### What the model can and cannot see

Claude Code shows a PreToolUse `permissionDecisionReason` to the model only on
a `deny`; on an `allow` it goes to the user. A provider that rewrites the input
therefore puts its note in `context`, which gate renders as
`additionalContext` beside the decision. `bash-guard`'s output bound does
this. A Stop hook has no passive channel at all: `additionalContext` there
continues the turn, so a provider that wants to leave a note about the reply
just sent waits for the next `UserPromptSubmit`.

## Providers
<!-- BEGIN GENERATED:providers -->

| Provider | Does | Events |
| --- | --- | --- |
| `bash-guard` | Deny destructive commands, route whole-file reads of large files, bound unbounded output | PreToolUse on Bash |
| `lint-gate` | Run the edited file's own checker and hand the failures back | PostToolUse on Edit, Write, MultiEdit |
| `loop-gate` | Hold a Stop until the armed command passes, with a cap and a stall bound | Stop |
| `nix-guard` | Deny an edit that resolves into the Nix store | PreToolUse on Edit, Write, NotebookEdit |
| `read-router` | Deny an unbounded Read of a large file and name the ranged read and the cheap reader | PreToolUse on Read |
| `review-gate` | Bound an adversarial review by its ledger, and tell each critic which revision to read | PreToolUse and PostToolUse on Agent, SubagentStart, UserPromptSubmit |

<!-- END GENERATED:providers -->

### The review ledger

`review` and `review-gate` replace a prose-driven adversarial review loop with
a record and a bound. The design follows what Spotify, Cloudflare, Anthropic,
Dropbox, and DoorDash publish: the thing that loops is a deterministic check
(`loop-gate`); a model review is one pass per revision, judged by a second
model that tries to disprove each finding; a finding with no file and line in
the change is dropped, not argued about.

```
review open --change openspec/changes/foo     # measure the diff, pick the tier, pass 1
  critics spawn (the gate counts them, denies a writer, tells each which revision to read)
  the mediator writes .gate/review/pass-1.json
review judge                                  # anchors checked, nits dropped, state set
  REVISE: the author fixes what survived
review reopen                                 # pass 2, only if the tree changed and the cap allows
review status --md                            # the block review.md carries
```

Providers keep that ledger and `loop-gate`'s armed command in `.gate` beside the
work, so add `.gate/` to the repository's gitignore. `GATE_STATE_DIR` moves the
whole directory. When `ORC_SESSION_ID` names a bound orc session, the state
lands in `.gate/orc/<session>` instead: orc binds several sessions to one
checkout, and an open review belongs to the session that opened it.

The tier comes from the diff, once, in `~/.config/gate/review.yaml`:

```yaml
tiers:
  trivial: { max_lines: 10,  max_files: 20, critics: 0 }
  lite:    { max_lines: 100, max_files: 20, critics: 1 }
  full:    { critics: 3 }
sensitive: ["**/secrets/**", "**/.github/workflows/**"]   # any match is full
readonly_agents: [Explore, review-mediator]
passes_max: 2
```

States: `OPEN`, `REVISE`, `CLEAN`, `HANDBACK` (a DEFER or a blocking finding:
the owner reads it), `CAPPED`, `HALTED`, `NOT_RUN`. `CLEAN` is model evidence,
not approval.

## Commands
<!-- BEGIN GENERATED:commands -->

### `gate`

Run hook decisions through a configured provider chain

gate runs the hook chain a harness declared and answers in the harness's shape.

A harness calls one command per hook event:

  gate hook --harness claude --event PreToolUse

gate reads the event on stdin, runs every provider configured for that event
whose match covers the tool, merges the decisions, and prints the result. The
first deny or block wins, a rewrite feeds the next provider, and every note
reaches the model. Each call is one line in the decisions log.

Providers are executables with a provider/v1 manifest in the providers
directory. They read a gate.decide request on stdin and answer with a decision.

### `gate completion`

Print shell completion for bash, fish, nu, or zsh

### `gate config`

Inspect the gate configuration

### `gate config path`

Print where the configuration is read from

### `gate config schema`

Print the configuration JSON Schema

### `gate config show`

Print the effective configuration as JSON

### `gate config validate`

Load the configuration and every referenced provider manifest

Fails when the file does not parse, names an unknown event, carries a match
that does not compile, or refers to a provider with no manifest. Run it at
build time: a hook that finds a broken config at runtime exits 1 and the
harness proceeds without a decision.

### `gate hook`

Decide one hook event read from stdin

Reads the harness's hook payload on stdin, runs the chain configured for the
event, and prints the merged decision in the harness's format. Exit status is
0, or 2 for a deny under --format exit-code. A missing or invalid config is
exit 1 with the reason on stderr: a hook that cannot read its policy must not
read as a pass.

| Option | Description |
| --- | --- |
| `--event` `<value>` | the hook event; read from the payload when omitted |
| `--format` `<value>` | the wire shape to answer in: claude, exit-code, or json |
| `--harness` `<value>` | which harness wrote the payload: claude, codex, gemini, or json |

### `gate log`

Read the decisions log

### `gate log tail`

Print the last decisions, one JSON line each

| Option | Description |
| --- | --- |
| `--lines`, `-n` `<value>` | how many records to print |

### `gate provider`

Inspect gate providers

### `gate provider list`

List the providers in the providers directory

| Option | Description |
| --- | --- |
| `--json` | print the manifests as JSON |

### `gate provider validate`

Check that a provider's manifest and host dependencies are in place

<!-- END GENERATED:commands -->

## Write a provider

```go
package main

import "github.com/roshbhatia/gate/pkg/gate"

func main() {
	gate.Serve(func(request gate.Request) (gate.Outcome, error) {
		if request.Event.Tool != "Bash" {
			return gate.PassOutcome(), nil
		}
		return gate.Outcome{Kind: gate.Context, Message: "noted"}, nil
	})
}
```

Install its manifest under `~/.config/gate/providers/<name>.yaml`:

```yaml
version: provider/v1
name: noted
description: Leave a note on every Bash call
command: [gate-provider-noted]
actions:
  gate.decide:
    description: PreToolUse on Bash
defaults:
  timeout: 1s
```

A provider in another language reads one provider/v1 request frame on stdin
(`input` is `{event, args}`, `schema/event.schema.json`) and writes one result
frame whose `output` is a decision (`schema/outcome.schema.json`).

## Develop

```bash
nix develop
go test -race ./...
./hack/generate.sh          # schemas and README sections; --check in CI
nix flake check
```
