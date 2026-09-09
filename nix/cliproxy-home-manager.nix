{ config, lib, pkgs, ... }:
let
  cfg = config.services.cliProxyRuntime;
  command = "${cfg.package}/bin/cliproxy-runtime";
in {
  options.services.cliProxyRuntime = {
    enable = lib.mkEnableOption "CLIProxyAPI with cloudflared and/or sing-box";
    package = lib.mkOption {
      type = lib.types.package;
      default = pkgs.callPackage ./cliproxy-package.nix { };
      description = "CLIProxyAPI runtime helper package.";
    };
    stateDir = lib.mkOption {
      type = lib.types.str;
      default = "${config.xdg.stateHome}/cliproxy-runtime";
      description = "Private runtime state; configure credentials in .env outside the Nix store.";
    };
    autoStart = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = "Start at login. Direct access is the default; sing-box and cloudflared are optional.";
    };
  };
  config = lib.mkIf cfg.enable {
    assertions = [{
      assertion = lib.hasPrefix "/" cfg.stateDir;
      message = "services.cliProxyRuntime.stateDir must be absolute";
    }];
    home.packages = [ cfg.package ];
    systemd.user.services.cliproxy-runtime = {
      Unit = {
        Description = "CLIProxyAPI runtime";
        Wants = [ "podman.socket" ];
        After = [ "podman.socket" ];
      };
      Service = {
        Type = "oneshot";
        RemainAfterExit = true;
        Environment = [ "CLIPROXY_STATE_DIR=${cfg.stateDir}" ];
        ExecStart = "${command} up";
        ExecStop = "${command} down";
        TimeoutStartSec = 900;
        TimeoutStopSec = 120;
      };
      Install.WantedBy = lib.optional cfg.autoStart "default.target";
    };
  };
}
