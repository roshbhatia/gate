{ mkProvider, vale }:
mkProvider {
  name = "prose-gate";
  manifest = ./provider.yaml;
  # The rule set is the caller's, named by args.style. Only the linter that
  # reads it is pinned here.
  runtimeInputs = [ vale ];
  aliases = [ "prose-gate" ];
}
