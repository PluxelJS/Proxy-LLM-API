{
  description = "New API runtime: one container, SQLite accounting, no external database";
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  outputs = { self, nixpkgs }:
    let
      forAllSystems = nixpkgs.lib.genAttrs [ "x86_64-linux" "aarch64-linux" ];
    in {
      packages = forAllSystems (system:
        let pkgs = import nixpkgs { inherit system; };
        in rec {
          new-api-runtime = pkgs.callPackage ./nix/package.nix { };
          default = new-api-runtime;
        });
      apps = forAllSystems (system: {
        default = {
          type = "app";
          program = "${self.packages.${system}.default}/bin/new-api-runtime";
        };
      });
      checks = forAllSystems (system:
        let pkgs = import nixpkgs { inherit system; };
        in {
          package = self.packages.${system}.default;
          runtime = pkgs.runCommand "new-api-runtime-tests" {
            nativeBuildInputs = [ pkgs.python3 ];
          } ''
            cp -r ${self} source
            chmod -R u+w source
            cd source
            python3 -m unittest discover -s tests -v
            touch "$out"
          '';
        });
      homeManagerModules.default = import ./nix/home-manager.nix;
      formatter = forAllSystems (system: nixpkgs.legacyPackages.${system}.nixfmt-tree);
    };
}
