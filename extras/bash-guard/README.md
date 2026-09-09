# bash-guard

Read a large token parser with bounded output.

## Install

```sh
brew install roshbhatia/tap/gate-provider-bash-guard
nix profile add 'github:roshbhatia/gate#provider-bash-guard'
```

Install the core utility separately, or select its all-provider bundle. Runtime tools still need their own credentials.

## Demo

![Read a large token parser with bounded output](demo.gif)

[Tape source](demo.tape) · [Task script](demo.sh)

Run `nix develop -c bash extras/bash-guard/demo.sh` to run the task without recording.
Run `nix develop -c python3 hack/extra-demos.py bash-guard` to record it.
