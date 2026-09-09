# loop-gate

Keep a repair task active until its tests pass.

## Install

```sh
brew install roshbhatia/tap/gate-provider-loop-gate
nix profile add 'github:roshbhatia/gate#provider-loop-gate'
```

Install the core utility separately, or select its all-provider bundle. Runtime tools still need their own credentials.

## Demo

![Keep a repair task active until its tests pass](demo.gif)

[Tape source](demo.tape) · [Task script](demo.sh)

Run `nix develop -c bash extras/loop-gate/demo.sh` to run the task without recording.
Run `nix develop -c python3 hack/extra-demos.py loop-gate` to record it.
