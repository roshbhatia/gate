# read-router

Select the token parser lines needed for review.

## Install

```sh
brew install roshbhatia/tap/gate-provider-read-router
nix profile add 'github:roshbhatia/gate#provider-read-router'
```

Install the core utility separately, or select its all-provider bundle. Runtime tools still need their own credentials.

## Demo

![Select the token parser lines needed for review](demo.gif)

[Tape source](demo.tape) · [Task script](demo.sh)

Run `nix develop -c bash extras/read-router/demo.sh` to run the task without recording.
Run `nix develop -c python3 hack/extra-demos.py read-router` to record it.
