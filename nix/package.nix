{ lib, buildGo126Module }:
buildGo126Module {
  pname = "dev-runtime";
  version = "0.1.0";
  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [ ../go.mod ../go.sum ../cmd ../internal ];
  };
  vendorHash = "sha256-ayIdSvE2DshehafuAMDnum3HoMq8AaiFhEIr730wm90=";
  env.CGO_ENABLED = "0";
  tags = [ "remote" "containers_image_openpgp" ];
  subPackages = [ "cmd/dev-runtime" ];
  # The embedded UI is checked into source and verified against Vite in CI.
  meta = {
    description = "Development service manager with a shared Go CLI and web core";
    homepage = "https://github.com/PluxelJS/Proxy-LLM-API";
    mainProgram = "dev-runtime";
    platforms = lib.platforms.linux;
  };
}
