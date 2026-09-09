{
  description = "CLIProxyAPI with cloudflared and/or sing-box; independent New API runtime";
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  outputs = { self, nixpkgs }:
    let
      forAllSystems = nixpkgs.lib.genAttrs [ "x86_64-linux" "aarch64-linux" ];
    in {
      packages = forAllSystems (system:
        let pkgs = import nixpkgs { inherit system; };
        in rec {
          new-api-runtime = pkgs.callPackage ./nix/package.nix { };
          cliproxy-runtime = pkgs.callPackage ./nix/cliproxy-package.nix { };
          default = cliproxy-runtime;
        });
      apps = forAllSystems (system: {
        default = {
          type = "app";
          program = "${self.packages.${system}.default}/bin/cliproxy-runtime";
        };
      });
      checks = forAllSystems (system:
        let pkgs = import nixpkgs { inherit system; };
        in {
          package = self.packages.${system}.default;
          runtime = pkgs.runCommand "gateway-runtime-tests" {
            nativeBuildInputs = [ (pkgs.python3.withPackages (p: [ p.pyyaml ])) ];
          } ''
            cp -r ${self} source
            chmod -R u+w source
            cd source
            python3 -m unittest discover -s tests -v
            touch "$out"
          '';
        });
      homeManagerModules = {
        default = { imports = [ ./nix/home-manager.nix ./nix/cliproxy-home-manager.nix ]; };
        cliproxy = import ./nix/cliproxy-home-manager.nix;
        new-api = import ./nix/home-manager.nix;
      };
      formatter = forAllSystems (system: nixpkgs.legacyPackages.${system}.nixfmt-tree);
    };
}
