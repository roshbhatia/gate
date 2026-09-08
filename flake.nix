{
  description = "Composable hook dispatcher for coding-agent harnesses";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  };

  outputs =
    { self, nixpkgs, ... }:
    let
      supportedSystems = [
        "aarch64-darwin"
        "aarch64-linux"
        "x86_64-linux"
      ];
      eachSystem = nixpkgs.lib.genAttrs supportedSystems;
      providerDirectories = nixpkgs.lib.filterAttrs (
        name: type:
        type == "directory"
        && builtins.pathExists (./extras + "/${name}/default.nix")
        && builtins.pathExists (./extras + "/${name}/provider.yaml")
      ) (builtins.readDir ./extras);
      providerNames = builtins.attrNames providerDirectories;
    in
    {
      formatter = eachSystem (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        pkgs.writeShellApplication {
          name = "gate-format";
          runtimeInputs = [
            pkgs.fd
            pkgs.nixfmt
          ];
          text = ''
            if [ "$#" -gt 0 ] && [ "''${1#-}" = "$1" ]; then
              exec nixfmt "$@"
            fi
            exec fd --extension nix --type file --exec-batch nixfmt "$@"
          '';
        }
      );

      packages = eachSystem (
        system:
        let
          lib = nixpkgs.lib;
          pkgs = nixpkgs.legacyPackages.${system};
          version = "0.2.2";
          # Refresh with `nix build .#gate` after any go.mod or go.sum change; the
          # build prints the hash it expected.
          vendorHash = "sha256-GeGKie7MSLctgm3nW2gJzpKkE42be2QyO7OrJ5P5wCU=";
          buildGo =
            {
              name,
              subPackage,
              builtName ? builtins.baseNameOf subPackage,
              check ? false,
            }:
            pkgs.buildGoModule {
              pname = name;
              inherit version vendorHash;
              src = ./.;
              subPackages = [ subPackage ];
              nativeCheckInputs = lib.optionals check [ pkgs.git ];
              doCheck = check;
              checkPhase = lib.optionalString check ''
                runHook preCheck
                export HOME="$TMPDIR/home"
                mkdir -p "$HOME"
                go test -race ./...
                go run ./cmd/gate generate --root . --check
                runHook postCheck
              '';
              postInstall = lib.optionalString (builtName != name) ''
                mv "$out/bin/${builtName}" "$out/bin/${name}"
              '';
              meta.mainProgram = name;
            };
          gate =
            (buildGo {
              name = "gate";
              subPackage = "./cmd/gate";
              check = true;
            }).overrideAttrs
              (old: {
                nativeBuildInputs = (old.nativeBuildInputs or [ ]) ++ [ pkgs.installShellFiles ];
                postInstall = (old.postInstall or "") + ''
                  installShellCompletion \
                    --cmd gate \
                    --bash <("$out/bin/gate" completion bash) \
                    --fish <("$out/bin/gate" completion fish) \
                    --zsh <("$out/bin/gate" completion zsh)
                  mkdir -p "$out/share/nushell/vendor/autoload"
                  "$out/bin/gate" completion nu > "$out/share/nushell/vendor/autoload/gate.nu"
                  mkdir -p "$out/share/gate/schema"
                  cp schema/*.json "$out/share/gate/schema/"
                '';
              });
          mkProvider = import ./extras/package.nix {
            inherit
              buildGo
              lib
              pkgs
              version
              ;
          };
          providerScope = pkgs // {
            inherit mkProvider;
          };
          providers = lib.genAttrs providerNames (
            name: lib.callPackageWith providerScope (./extras + "/${name}/default.nix") { }
          );
          review = lib.callPackageWith (pkgs // { inherit buildGo; }) ./extras/review/default.nix { };
          extras = pkgs.symlinkJoin {
            name = "gate-extras-${version}";
            paths = lib.attrValues providers ++ [ review ];
            passthru = {
              inherit providers review;
            };
          };
          full = pkgs.symlinkJoin {
            name = "gate-full-${version}";
            paths = [
              gate
              extras
            ];
          };
          providerOutputs = lib.mapAttrs' (
            name: package: lib.nameValuePair "provider-${name}" package
          ) providers;
        in
        {
          inherit
            gate
            extras
            full
            review
            ;
          default = gate;
        }
        // providerOutputs
      );

      apps = eachSystem (system: {
        default = {
          type = "app";
          program = "${nixpkgs.lib.getExe self.packages.${system}.default}";
        };
      });

      checks = eachSystem (
        system:
        let
          lib = nixpkgs.lib;
          pkgs = nixpkgs.legacyPackages.${system};
          packages = self.packages.${system};
        in
        {
          default = packages.gate;
          # Every provider validates against the core, with nothing else on PATH.
          providers = pkgs.runCommand "gate-provider-validation" { nativeBuildInputs = [ pkgs.jq ]; } ''
            export HOME="$TMPDIR/home"
            export XDG_CONFIG_HOME="$TMPDIR/config"
            export XDG_STATE_HOME="$TMPDIR/state"
            mkdir -p "$HOME" "$XDG_CONFIG_HOME/gate/providers" "$XDG_STATE_HOME"
            cp ${packages.extras}/share/gate/providers/*.yaml "$XDG_CONFIG_HOME/gate/providers/"
            export PATH="${packages.full}/bin:${pkgs.coreutils}/bin:${pkgs.jq}/bin"
            cat > "$XDG_CONFIG_HOME/gate/config.yaml" <<EOF
            version: gate.config/v1
            chains:
              PreToolUse:
            ${lib.concatMapStringsSep "\n" (name: "    - { provider: ${name} }") providerNames}
            EOF
            gate config validate
            gate provider validate
            test "$(gate provider list --json | jq 'length')" -eq ${toString (builtins.length providerNames)}
            echo '{"hook_event_name":"PreToolUse","tool_name":"Read","tool_input":{"file_path":"/nonexistent"},"cwd":"/"}' \
              | gate hook --harness claude --event PreToolUse --format json > decision
            test ! -s decision
            touch "$out"
          '';
        }
      );

      devShells = eachSystem (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          default = pkgs.mkShellNoCC {
            packages = [
              pkgs.go
              pkgs.gopls
              pkgs.gotools
              pkgs.go-tools
              pkgs.goreleaser
              pkgs.git
              pkgs.jq
              pkgs.shfmt
            ];
            shellHook = ''
              export GOTOOLCHAIN=local
            '';
          };
        }
      );
    };
}
