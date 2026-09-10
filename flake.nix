{
  description = "Dev Runtime — Go CLI and local management dashboard";
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  outputs = { self, nixpkgs }:
    let forLinux = nixpkgs.lib.genAttrs [ "x86_64-linux" "aarch64-linux" ];
    in {
      packages = forLinux (system: let pkgs = import nixpkgs { inherit system; }; in rec {
        dev-runtime = pkgs.callPackage ./nix/package.nix { };
        default = dev-runtime;
      });
      apps = forLinux (system: { default = {
        type = "app";
        program = "${self.packages.${system}.default}/bin/dev-runtime";
      }; });
      checks = forLinux (system: { package = self.packages.${system}.default; });
      homeManagerModules.default = import ./nix/home-manager.nix;
      devShells = forLinux (system: let pkgs = import nixpkgs { inherit system; }; in {
        default = pkgs.mkShell { packages = [ pkgs.go_1_26 pkgs.nodejs pkgs.gnumake ]; CGO_ENABLED = "0"; };
      });
      formatter = forLinux (system: nixpkgs.legacyPackages.${system}.nixfmt-tree);
    };
}
