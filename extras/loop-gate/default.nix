{ mkProvider, bash }:
mkProvider {
  name = "loop-gate";
  manifest = ./provider.yaml;
  runtimeInputs = [ bash ];
  aliases = [ "loop-gate" ];
}
