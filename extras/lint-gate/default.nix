{
  mkProvider,
  deadnix,
  ruff,
  shellcheck,
  statix,
  stylua,
  taplo,
}:
mkProvider {
  name = "lint-gate";
  manifest = ./provider.yaml;
  # The checkers the gate knows how to run. gofmt comes with the Go toolchain
  # on a machine that edits Go, so it is not pinned here.
  runtimeInputs = [
    deadnix
    ruff
    shellcheck
    statix
    stylua
    taplo
  ];
}
