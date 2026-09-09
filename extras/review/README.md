# review

Open a review ledger for the token parser change.

## Install

```sh
brew install roshbhatia/tap/gate-review
nix profile add 'github:roshbhatia/gate#review'
```

Install the core utility separately, or select its all-provider bundle. Runtime tools still need their own credentials.

## Demo

![Open a review ledger for the token parser change](demo.gif)

[Tape source](demo.tape) · [Task script](demo.sh)

Run `nix develop -c bash extras/review/demo.sh` to run the task without recording.
Run `nix develop -c python3 hack/extra-demos.py review` to record it.
