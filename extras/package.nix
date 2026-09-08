# One gate provider: the Go adapter under extras/<name>, published as
# gate-provider-<name>, with its manifest installed where `gate` discovers it.
{
  buildGo,
  lib,
  pkgs,
  version,
}:
{
  name,
  manifest,
  adapterSubpackage ? "./extras/${name}",
  # Commands the provider shells out to. They are pinned on its PATH rather than
  # inherited, so a missing checker is a build failure and not a silent pass.
  runtimeInputs ? [ ],
  # Extra names for the same binary. loop-gate is also a CLI the owner types.
  aliases ? [ ],
}:
let
  adapter = buildGo {
    name = "gate-provider-${name}-raw";
    subPackage = adapterSubpackage;
    builtName = name;
  };
  executable = "gate-provider-${name}";
in
pkgs.runCommand "gate-provider-${name}-${version}"
  {
    nativeBuildInputs = [ pkgs.makeBinaryWrapper ];
    meta = {
      description = "gate provider ${name}";
      mainProgram = executable;
    };
  }
  ''
    mkdir -p "$out/bin" "$out/share/gate/providers"
    makeWrapper "${adapter}/bin/gate-provider-${name}-raw" "$out/bin/${executable}" \
      ${lib.optionalString (runtimeInputs != [ ]) "--prefix PATH : ${lib.makeBinPath runtimeInputs}"}
    ${lib.concatMapStringsSep "\n" (alias: ''
      makeWrapper "${adapter}/bin/gate-provider-${name}-raw" "$out/bin/${alias}" \
        ${lib.optionalString (runtimeInputs != [ ]) "--prefix PATH : ${lib.makeBinPath runtimeInputs}"}
    '') aliases}
    install -m 0444 ${manifest} "$out/share/gate/providers/${name}.yaml"
  ''
