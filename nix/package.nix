{
  lib,
  stdenvNoCC,
  makeWrapper,
  bash,
  coreutils,
  curl,
  findutils,
  gawk,
  git,
  gnugrep,
  gnused,
  openssl,
  python3,
}:
let
  runtimeInputs = [
    bash
    coreutils
    curl
    findutils
    gawk
    git
    gnugrep
    gnused
    openssl
    python3
  ];
in
stdenvNoCC.mkDerivation {
  pname = "proxy-llm";
  version = "0-unstable-2026-08-09";
  # Use an allowlist so a direct callPackage can never copy ignored runtime
  # credentials or databases from a mutable checkout into the Nix store.
  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [
      ../.env.example
      ../compose.build.yaml
      ../compose.cloudflare-tunnel.yaml
      ../compose.internal-dragonfly.yaml
      ../compose.internal-postgres.yaml
      ../compose.podman.yaml
      ../compose.singbox.yaml
      ../docker-compose.yaml
      ../manage.sh
      ../scripts
      ../cliproxyapi/Dockerfile
      ../cliproxyapi/config.example.yaml
      ./proxy-llm-wrapper
    ];
  };

  nativeBuildInputs = [ makeWrapper ];

  installPhase = ''
    runHook preInstall

    resourceRoot="$out/share/proxy-llm"
    mkdir -p "$resourceRoot/cliproxyapi" "$resourceRoot/scripts" "$out/bin"

    install -m755 manage.sh "$resourceRoot/manage.sh"
    install -m755 scripts/* "$resourceRoot/scripts/"
    install -m644 .env.example docker-compose.yaml compose.*.yaml "$resourceRoot/"
    install -m644 cliproxyapi/Dockerfile cliproxyapi/config.example.yaml \
      "$resourceRoot/cliproxyapi/"

    install -m755 nix/proxy-llm-wrapper "$out/bin/proxy-llm"
    substituteInPlace "$out/bin/proxy-llm" \
      --replace-fail '@resourceRoot@' "$resourceRoot"
    wrapProgram "$out/bin/proxy-llm" \
      --prefix PATH : ${lib.makeBinPath runtimeInputs}

    runHook postInstall
  '';

  meta = {
    description = "Compose lifecycle helper for Proxy-LLM-API";
    homepage = "https://github.com/PluxelJS/Proxy-LLM-API";
    mainProgram = "proxy-llm";
    platforms = lib.platforms.linux;
  };
}
