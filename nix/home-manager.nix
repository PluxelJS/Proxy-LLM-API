{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.services.proxyLlm;
  command = lib.getExe cfg.package;
  escapedStateDir = lib.escapeShellArg cfg.stateDir;
  escapedLegacyStateDir = lib.escapeShellArg (
    if cfg.legacyStateDir == null then "" else cfg.legacyStateDir
  );
in
{
  options.services.proxyLlm = {
    enable = lib.mkEnableOption "Proxy-LLM-API compose stack";

    package = lib.mkOption {
      type = lib.types.package;
      default = pkgs.callPackage ./package.nix { };
      defaultText = lib.literalExpression "pkgs.callPackage ./nix/package.nix { }";
      description = "Proxy-LLM-API helper package to run.";
    };

    stateDir = lib.mkOption {
      type = lib.types.str;
      default = "${config.xdg.stateHome}/proxy-llm";
      description = "Writable local configuration, credentials, and bind-mounted runtime state.";
    };

    autoStart = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = "Add proxy-llm.service to default.target and queue its first start asynchronously.";
    };

    initialize = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = "Create missing local configuration and random credentials without printing secrets.";
    };

    legacyStateDir = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Optional old checkout state to migrate once without overwriting the target.";
    };
  };

  config = lib.mkMerge [
    (lib.mkIf cfg.enable {
      assertions = [
        {
          assertion = lib.hasPrefix "/" cfg.stateDir;
          message = "services.proxyLlm.stateDir must be an absolute path";
        }
        {
          assertion = cfg.legacyStateDir == null || lib.hasPrefix "/" cfg.legacyStateDir;
          message = "services.proxyLlm.legacyStateDir must be null or an absolute path";
        }
      ];

      home.packages = [ cfg.package ];

      systemd.user.services.proxy-llm = {
        Unit = {
          Description = "Proxy-LLM-API compose stack";
          Wants = [ "podman.socket" ];
          After = [ "podman.socket" ];
          # Keep a healthy running stack across Home Manager switches. A
          # deliberate service restart (or the next login) adopts new code.
          X-SwitchMethod = "keep-old";
        };
        Service = {
          Type = "oneshot";
          RemainAfterExit = true;
          Environment = [ "PROXY_LLM_STATE_DIR=${cfg.stateDir}" ];
          ExecStartPre = lib.optional cfg.initialize "${command} init --no-show-secrets";
          ExecStart = "${command} up";
          ExecStop = "${command} down";
          TimeoutStartSec = 900;
          TimeoutStopSec = 120;
        };
        # The activation below owns this enable symlink so first start can be
        # queued with --no-block after Home Manager finishes sd-switch.
        Install.WantedBy = [ ];
      };

      home.activation.prepareProxyLlmUnit = lib.hm.dag.entryBefore [ "checkLinkTargets" ] ''
        ${lib.optionalString (cfg.legacyStateDir != null) ''
          marker=${lib.escapeShellArg "${cfg.stateDir}/.migrated-from"}
          if [ ! -f "$marker" ] || [ "$(${lib.getExe' pkgs.coreutils "cat"} "$marker")" != ${escapedLegacyStateDir} ]; then
            echo "Proxy-LLM-API 旧状态尚未安全切换。" >&2
            echo "请先执行: PROXY_LLM_STATE_DIR=${escapedStateDir} ${command} cutover ${escapedLegacyStateDir}" >&2
            exit 1
          fi
        ''}
        # Retire symlinks created by an older generation before Home Manager
        # installs the new unit. `disable` without `--now` never stops the
        # currently loaded service, which sd-switch keeps active below.
        if command -v systemctl >/dev/null 2>&1; then
          systemctl --user disable proxy-llm.service >/dev/null 2>&1 || true
        fi
      '';

      home.activation.enableProxyLlmPodmanSocket = lib.hm.dag.entryAfter [ "reloadSystemd" ] ''
        if command -v systemctl >/dev/null 2>&1; then
          systemctl --user daemon-reload
          systemctl --user enable --now podman.socket
          ${
            if cfg.autoStart then
              ''
                # The unit deliberately has no [Install] target: declaring
                # WantedBy in Home Manager would make sd-switch wait for the
                # first, potentially image-pulling start. add-wants establishes
                # the same boot relationship without starting it synchronously.
                systemctl --user add-wants default.target proxy-llm.service
                if ! systemctl --user is-active --quiet proxy-llm.service; then
                  systemctl --user start --no-block proxy-llm.service
                fi
              ''
            else
              ""
          }
        else
          echo "Proxy-LLM-API requires a systemd user session." >&2
          exit 1
        fi
      '';
    })

    (lib.mkIf (!cfg.enable) {
      # Stop only a unit previously materialized from the Nix store. A regular
      # user-owned unit with the same name remains untouched.
      home.activation.retireProxyLlm = lib.hm.dag.entryBefore [ "checkLinkTargets" ] ''
        unit="$HOME/.config/systemd/user/proxy-llm.service"
        if [ -L "$unit" ]; then
          resolved="$(${lib.getExe' pkgs.coreutils "readlink"} -f "$unit" 2>/dev/null || true)"
          case "$resolved" in
            /nix/store/*)
              if command -v systemctl >/dev/null 2>&1; then
                systemctl --user disable --now proxy-llm.service >/dev/null 2>&1 || true
                systemctl --user reset-failed proxy-llm.service >/dev/null 2>&1 || true
              fi
              ;;
          esac
        fi
      '';
    })
  ];
}
