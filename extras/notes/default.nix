{ mkProvider }:
mkProvider {
  name = "notes";
  manifest = ./provider.yaml;
  aliases = [ "note" ];
}
