{ lib, stdenvNoCC, makeWrapper, python3, bash, coreutils, podman-compose }:
stdenvNoCC.mkDerivation {
  pname = "new-api-runtime";
  version = "1.0.0";
  # Explicit allowlist: credentials and SQLite files cannot enter the Nix store.
  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [ ../docker-compose.yaml ../scripts/runtime.py ];
  };
  nativeBuildInputs = [ makeWrapper ];
  installPhase = ''
    resourceRoot="$out/share/new-api-runtime"
    mkdir -p "$resourceRoot/scripts" "$out/bin"
    install -m644 docker-compose.yaml "$resourceRoot/"
    install -m644 scripts/runtime.py "$resourceRoot/scripts/"
    makeWrapper ${python3}/bin/python3 "$out/bin/new-api-runtime" \
      --add-flags "$resourceRoot/scripts/runtime.py" \
      --set PODMAN_COMPOSE_PROVIDER ${lib.getExe podman-compose} \
      --prefix PATH : ${lib.makeBinPath [ bash coreutils ]}
  '';
  meta = {
    description = "New API single-service runtime with persistent SQLite accounting";
    homepage = "https://github.com/PluxelJS/Proxy-LLM-API";
    mainProgram = "new-api-runtime";
    platforms = lib.platforms.linux;
  };
}
