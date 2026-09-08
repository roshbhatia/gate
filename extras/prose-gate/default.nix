{ mkProvider, vale }:
mkProvider {
  name = "prose-gate";
  manifest = ./provider.yaml;
  # The rule set is the caller's, named by args.style. Only the linter that
  # reads it is pinned here. It is pinned and not declared in requires.commands:
  # gate resolves a requirement against the caller's PATH at invoke time, so a
  # declared vale would fail the call wherever the harness has none, which is
  # exactly where the wrapper is supposed to supply it.
  runtimeInputs = [ vale ];
  aliases = [ "prose-gate" ];
}
