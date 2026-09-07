{ config, lib, pkgs, ... }:
let
  cfg = config.services.newApiRuntime;
  command = lib.getExe cfg.package;
in
{
  options.services.newApiRuntime = {
    enable = lib.mkEnableOption "standalone New API SQLite runtime";
    package = lib.mkOption {
      type = lib.types.package;
      default = pkgs.callPackage ./package.nix { };
      description = "Runtime helper package.";
    };
    stateDir = lib.mkOption {
      type = lib.types.str;
      default = "${config.xdg.stateHome}/new-api-runtime";
      description = "Private writable configuration and SQLite data; never put secrets in Nix options.";
    };
    autoStart = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = "Start the standalone runtime at login. Leave this module disabled when dev-runtime owns the gateway.";
    };
  };
  config = lib.mkIf cfg.enable {
    assertions = [{
      assertion = lib.hasPrefix "/" cfg.stateDir;
      message = "services.newApiRuntime.stateDir must be absolute";
    }];
    home.packages = [ cfg.package ];
    systemd.user.services.new-api-runtime = {
      Unit = {
        Description = "New API with SQLite";
        Wants = [ "podman.socket" ];
        After = [ "podman.socket" ];
        X-SwitchMethod = "keep-old";
      };
      Service = {
        Type = "oneshot";
        RemainAfterExit = true;
        Environment = [ "NEW_API_STATE_DIR=${cfg.stateDir}" ];
        ExecStartPre = "${command} init";
        ExecStart = "${command} up";
        ExecStop = "${command} down";
        TimeoutStartSec = 900;
        TimeoutStopSec = 120;
      };
      Install.WantedBy = lib.optional cfg.autoStart "default.target";
    };
  };
}
