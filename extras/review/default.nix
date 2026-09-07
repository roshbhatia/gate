# review is a tool, not a provider: it has no manifest. review-gate requires it
# on PATH, so the two are installed together by the flake's `extras`.
{ buildGo, git }:
(buildGo {
  name = "review";
  subPackage = "./extras/review";
  builtName = "review";
}).overrideAttrs
  (old: {
    passthru = (old.passthru or { }) // {
      runtimeInputs = [ git ];
    };
  })
