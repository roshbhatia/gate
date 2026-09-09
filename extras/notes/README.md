# notes

Present an unanswered token validation note.

## Install

```sh
brew install roshbhatia/tap/gate-provider-notes
nix profile add 'github:roshbhatia/gate#provider-notes'
```

Install the core utility separately, or select its all-provider bundle. Runtime tools still need their own credentials.

## Demo

![Present an unanswered token validation note](demo.gif)

[Tape source](demo.tape) · [Task script](demo.sh)

Run `nix develop -c bash extras/notes/demo.sh` to run the task without recording.
Run `nix develop -c python3 hack/extra-demos.py notes` to record it.
