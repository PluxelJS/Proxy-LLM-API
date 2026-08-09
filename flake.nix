{
  description = "Proxy-LLM-API package and Home Manager module";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
    in
    {
      packages = forAllSystems (
        system:
        let
          pkgs = import nixpkgs { inherit system; };
        in
        rec {
          proxy-llm = pkgs.callPackage ./nix/package.nix { };
          default = proxy-llm;
        }
      );

      apps = forAllSystems (system: {
        default = {
          type = "app";
          program = "${self.packages.${system}.default}/bin/proxy-llm";
        };
      });

      checks = forAllSystems (
        system:
        let
          pkgs = import nixpkgs { inherit system; };
          package = self.packages.${system}.default;
        in
        {
          inherit package;
          state-directory = pkgs.runCommand "proxy-llm-state-directory-check" { } ''
            mkdir -p "$TMPDIR/fake-bin" "$TMPDIR/home" "$out"
            printf '%s\n' '#!${pkgs.runtimeShell}' 'exit 0' > "$TMPDIR/fake-bin/podman"
            chmod +x "$TMPDIR/fake-bin/podman"
            printf '%s\n' \
              '#!${pkgs.runtimeShell}' \
              'case "$*" in *is-active*) exit 1 ;; *) exit 0 ;; esac' \
              > "$TMPDIR/fake-bin/systemctl"
            chmod +x "$TMPDIR/fake-bin/systemctl"

            export PATH="$TMPDIR/fake-bin:$PATH"
            export HOME="$TMPDIR/home"
            export XDG_STATE_HOME="$TMPDIR/state home"
            ${package}/bin/proxy-llm init --no-show-secrets >/dev/null

            state="$XDG_STATE_HOME/proxy-llm"
            test -f "$state/.env"
            test -f "$state/cliproxyapi/config.yaml"
            test "$(stat -c %a "$state/.env")" = 600
            test "$(stat -c %a "$state/cliproxyapi/config.yaml")" = 600
            test ! -e ${self}/.env

            legacy="$TMPDIR/legacy-checkout"
            migrated="$TMPDIR/migrated-state"
            mkdir -p "$legacy/cliproxyapi/oa"
            cp ${self}/.env.example "$legacy/.env"
            cp ${self}/cliproxyapi/config.example.yaml "$legacy/cliproxyapi/config.yaml"
            touch "$legacy/cliproxyapi/oa/account.json"
            PROXY_LLM_STATE_DIR="$migrated" \
              ${package}/bin/proxy-llm migrate "$legacy" >/dev/null
            test -f "$migrated/.env"
            test -f "$migrated/cliproxyapi/config.yaml"
            test -f "$migrated/cliproxyapi/oa/account.json"
            test "$(cat "$migrated/.migrated-from")" = "$legacy"
            PROXY_LLM_STATE_DIR="$migrated" \
              ${package}/bin/proxy-llm migrate "$legacy" >/dev/null

            cutover_legacy="$TMPDIR/cutover-legacy"
            cutover_state="$TMPDIR/cutover-state"
            mkdir -p "$cutover_legacy/cliproxyapi"
            cp -r ${self}/scripts "$cutover_legacy/scripts"
            cp ${self}/manage.sh ${self}/.env.example \
              ${self}/docker-compose.yaml ${self}/compose.*.yaml \
              "$cutover_legacy/"
            cp ${self}/.env.example "$cutover_legacy/.env"
            cp ${self}/cliproxyapi/config.example.yaml \
              "$cutover_legacy/cliproxyapi/config.yaml"
            chmod -R u+w "$cutover_legacy"
            chmod +x "$cutover_legacy/manage.sh" "$cutover_legacy/scripts/"*
            for script in "$cutover_legacy/manage.sh" "$cutover_legacy/scripts/"*; do
              if head -n 1 "$script" | grep -q '/usr/bin/env bash'; then
                sed -i '1c #!${pkgs.bash}/bin/bash' "$script"
              fi
            done
            PROXY_LLM_STATE_DIR="$cutover_state" \
              ${package}/bin/proxy-llm cutover "$cutover_legacy" >/dev/null
            test -f "$cutover_state/.migrated-from"
            touch "$out/passed"
          '';
          shell-syntax = pkgs.runCommand "proxy-llm-shell-syntax-check" { } ''
            ${pkgs.bash}/bin/bash -n \
              ${self}/manage.sh \
              ${self}/scripts/build-cliproxy \
              ${self}/scripts/cliproxy-login \
              ${self}/scripts/compose \
              ${self}/scripts/cutover \
              ${self}/scripts/healthcheck \
              ${self}/scripts/init \
              ${self}/scripts/migrate-state \
              ${self}/scripts/runtime \
              ${self}/scripts/service-exec \
              ${self}/scripts/verify-proxy \
              ${self}/nix/proxy-llm-wrapper
            touch "$out"
          '';
        }
      );

      homeManagerModules.default = import ./nix/home-manager.nix;
      formatter = forAllSystems (system: nixpkgs.legacyPackages.${system}.nixfmt-tree);
    };
}
