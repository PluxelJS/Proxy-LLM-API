{ config, lib, pkgs, ... }:
let
  cfg = config.services.devRuntime;
  command = lib.getExe cfg.package;
  quote = value: ''"${lib.replaceStrings [ "\\" "\"" "%" "$" ] [ "\\\\" "\\\"" "%%" "$$" ] value}"'';
in {
  options.services.devRuntime = {
    enable = lib.mkEnableOption "Dev Runtime local dashboard";
    package = lib.mkOption { type = lib.types.package; default = pkgs.callPackage ./package.nix { }; };
    stateDir = lib.mkOption { type = lib.types.str; default = "${config.xdg.stateHome}/dev-runtime"; };
    engine = lib.mkOption { type = lib.types.enum [ "auto" "podman" "docker" ]; default = "auto"; };
    endpoint = lib.mkOption { type = lib.types.str; default = ""; };
    listen = lib.mkOption { type = lib.types.str; default = "127.0.0.1:8318"; };
  };
  config = lib.mkIf cfg.enable {
    assertions = [{ assertion = lib.hasPrefix "/" cfg.stateDir; message = "devRuntime.stateDir must be absolute"; }];
    home.packages = [ cfg.package ];
    systemd.user.services.dev-runtime = {
      Unit = {
        Description = "Dev Runtime local management";
        Wants = lib.optional (cfg.engine == "podman") "podman.socket";
        After = [ "network.target" ] ++ lib.optional (cfg.engine == "podman") "podman.socket";
      };
      Service = {
        ExecStartPre = "${command} --state-dir ${quote cfg.stateDir} init --engine ${cfg.engine}" + lib.optionalString (cfg.endpoint != "") " --endpoint ${quote cfg.endpoint}";
        ExecStart = "${command} --state-dir ${quote cfg.stateDir} serve --autostart --listen ${quote cfg.listen}";
        Restart = "on-failure";
        RestartSec = 3;
        UMask = "0077";
        TimeoutStopSec = 30;
      };
      Install.WantedBy = [ "default.target" ];
    };
  };
}
