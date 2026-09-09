# lint-gate

Check token parser formatting after an edit.

## Install

```sh
brew install roshbhatia/tap/gate-provider-lint-gate
nix profile add 'github:roshbhatia/gate#provider-lint-gate'
```

Install the core utility separately, or select its all-provider bundle. Runtime tools still need their own credentials.

Nix includes every configured linter. Homebrew supplies the available formulae; install `deadnix` separately for Nix file checks. Go checks also require `gofmt` from your Go toolchain.

## Demo

![Check token parser formatting after an edit](demo.gif)

[Tape source](demo.tape) · [Task script](demo.sh)

Run `nix develop -c bash extras/lint-gate/demo.sh` to run the task without recording.
Run `nix develop -c python3 hack/extra-demos.py lint-gate` to record it.
