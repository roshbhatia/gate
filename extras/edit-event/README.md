# edit-event

Record the token parser change for the editor.

## Install

```sh
brew install roshbhatia/tap/gate-provider-edit-event
nix profile add 'github:roshbhatia/gate#provider-edit-event'
```

Install the core utility separately, or select its all-provider bundle. Runtime tools still need their own credentials.

## Demo

![Record the token parser change for the editor](demo.gif)

[Tape source](demo.tape) · [Task script](demo.sh)

Run `nix develop -c bash extras/edit-event/demo.sh` to run the task without recording.
Run `nix develop -c python3 hack/extra-demos.py edit-event` to record it.
