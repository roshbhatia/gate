# prose-gate

Check a release note before publication.

## Install

```sh
brew install roshbhatia/tap/gate-provider-prose-gate
nix profile add 'github:roshbhatia/gate#provider-prose-gate'
```

Install the core utility separately, or select its all-provider bundle. Runtime tools still need their own credentials.

## Demo

![Check a release note before publication](demo.gif)

[Tape source](demo.tape) · [Task script](demo.sh)

Run `nix develop -c bash extras/prose-gate/demo.sh` to run the task without recording.
Run `nix develop -c python3 hack/extra-demos.py prose-gate` to record it.
