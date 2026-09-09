{ mkProvider, agent-notes }:
mkProvider {
  name = "notes";
  manifest = ./provider.yaml;
  runtimeInputs = [ agent-notes ];
}
