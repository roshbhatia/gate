# nix-guard

Detect a managed configuration path before editing.

## Install

```sh
brew install roshbhatia/tap/gate-provider-nix-guard
nix profile add 'github:roshbhatia/gate#provider-nix-guard'
```

Install the core utility separately, or select its all-provider bundle. Runtime tools still need their own credentials.

## Demo

![Detect a managed configuration path before editing](demo.gif)

[Tape source](demo.tape) · [Task script](demo.sh)

Run `nix develop -c bash extras/nix-guard/demo.sh` to run the task without recording.
Run `nix develop -c python3 hack/extra-demos.py nix-guard` to record it.
