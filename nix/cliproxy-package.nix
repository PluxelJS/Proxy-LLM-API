{ lib, stdenvNoCC, makeWrapper, python3, podman-compose }:
let
  python = python3.withPackages (p: [ p.pyyaml ]);
in stdenvNoCC.mkDerivation {
  pname = "cliproxy-runtime";
  version = "2.0.0";
  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [
      ../scripts/cliproxy.py ../scripts/runtime.py ../scripts/singbox.py
      ../build/CLIProxyAPI.Dockerfile
    ];
  };
  nativeBuildInputs = [ makeWrapper ];
  installPhase = ''
    resourceRoot="$out/share/cliproxy-runtime"
    mkdir -p "$resourceRoot/scripts" "$resourceRoot/build" "$out/bin"
    install -m644 scripts/*.py "$resourceRoot/scripts/"
    install -m644 build/CLIProxyAPI.Dockerfile "$resourceRoot/build/"
    makeWrapper ${python}/bin/python3 "$out/bin/cliproxy-runtime" \
      --add-flags "$resourceRoot/scripts/cliproxy.py" \
      --set PODMAN_COMPOSE_PROVIDER ${lib.getExe podman-compose}
  '';
  meta = {
    description = "Web-managed CLIProxyAPI with optional sing-box forwarding checks";
    homepage = "https://github.com/PluxelJS/Proxy-LLM-API";
    mainProgram = "cliproxy-runtime";
    platforms = lib.platforms.linux;
  };
}
