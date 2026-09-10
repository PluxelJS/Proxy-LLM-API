package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/PluxelJS/Proxy-LLM-API/internal/app"
	"github.com/coreos/go-systemd/v22/unit"
)

type ServiceCommands struct {
	Install ServiceInstall `cmd:"" help:"Install and start a systemd user service"`
}
type ServiceInstall struct {
	Listen string `default:"127.0.0.1:8318"`
}

func unitArg(s string) string {
	return strconv.Quote(strings.NewReplacer("%", "%%", "$", "$$").Replace(s))
}

func (c *ServiceInstall) Run(ctx context.Context, r *Runtime) error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return errors.New("systemd is unavailable; run dev-runtime serve --autostart in the foreground")
	}
	if err := app.Init(ctx, r.CLI.StateDir, "auto", ""); err != nil {
		return err
	}
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	stateDir, err := filepath.Abs(r.CLI.StateDir)
	if err != nil {
		return err
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(configDir, "systemd", "user")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	options := []*unit.UnitOption{
		unit.NewUnitOption("Unit", "Description", "Dev Runtime local management"),
		unit.NewUnitOption("Unit", "After", "network.target"),
		unit.NewUnitOption("Service", "ExecStart", unitArg(bin)+" --state-dir "+unitArg(stateDir)+" serve --autostart --listen "+unitArg(c.Listen)),
		unit.NewUnitOption("Service", "Restart", "on-failure"),
		unit.NewUnitOption("Service", "RestartSec", "3"),
		unit.NewUnitOption("Service", "TimeoutStopSec", "30"),
		unit.NewUnitOption("Service", "UMask", "0077"),
		unit.NewUnitOption("Install", "WantedBy", "default.target"),
	}
	a, err := r.App(ctx)
	if err != nil {
		return err
	}
	var config app.Config
	if err = a.Store.Get(ctx, "system", "config", &config); err != nil {
		return err
	}
	if config.Engine == "podman" {
		options = append(options, unit.NewUnitOption("Unit", "Wants", "podman.socket"), unit.NewUnitOption("Unit", "After", "podman.socket"))
	}
	data, err := io.ReadAll(unit.Serialize(options))
	if err != nil {
		return err
	}
	file := filepath.Join(dir, "dev-runtime.service")
	if _, err = os.Stat(file); err == nil {
		return errors.New("dev-runtime.service already exists; edit or remove it explicitly before installing")
	}
	if err = os.WriteFile(file, data, 0600); err != nil {
		return err
	}
	for _, args := range [][]string{{"--user", "daemon-reload"}, {"--user", "enable", "--now", "dev-runtime.service"}} {
		command := exec.CommandContext(ctx, "systemctl", args...)
		command.Stdout, command.Stderr = os.Stderr, os.Stderr
		if err = command.Run(); err != nil {
			return errors.New("unit installed but systemctl failed; inspect systemctl --user status dev-runtime.service")
		}
	}
	return r.Print(map[string]string{"unit": file, "url": "http://" + c.Listen})
}
