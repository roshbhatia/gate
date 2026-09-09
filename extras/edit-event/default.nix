{ mkProvider, git }:
mkProvider {
  name = "edit-event";
  manifest = ./provider.yaml;
  # The shadow repository is plain git; a hook with no git on PATH would record
  # the log line and silently drop the delta.
  runtimeInputs = [ git ];
}
