# review-gate

Require a review ledger before the critic starts.

## Install

```sh
brew install roshbhatia/tap/gate-provider-review-gate
nix profile add 'github:roshbhatia/gate#provider-review-gate'
```

Install the core utility separately, or select its all-provider bundle. Runtime tools still need their own credentials.

## Demo

![Require a review ledger before the critic starts](demo.gif)

[Tape source](demo.tape) · [Task script](demo.sh)

Run `nix develop -c bash extras/review-gate/demo.sh` to run the task without recording.
Run `nix develop -c python3 hack/extra-demos.py review-gate` to record it.
