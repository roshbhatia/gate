{ mkProvider }:
mkProvider {
  name = "nix-guard";
  manifest = ./provider.yaml;
}
