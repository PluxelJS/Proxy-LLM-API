package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PluxelJS/Proxy-LLM-API/internal/app"
	"github.com/PluxelJS/Proxy-LLM-API/internal/state"
	"github.com/PluxelJS/Proxy-LLM-API/internal/web"
	"github.com/alecthomas/kong"
	completion "github.com/jotaen/kong-completion"
	"github.com/posener/complete"
)

type CLI struct {
	Service    ServiceCommands       `cmd:""`
	StateDir   string                `help:"Private workspace directory" env:"DEV_RUNTIME_STATE_DIR" default:"${state_dir}"`
	JSON       bool                  `help:"Stable JSON output"`
	Timeout    time.Duration         `help:"Operation timeout" default:"15m"`
	Init       Init                  `cmd:"" help:"Create a new workspace; never import existing deployments"`
	Doctor     Doctor                `cmd:"" help:"Check the configured engine connection"`
	Serve      Serve                 `cmd:"" help:"Run the local web interface"`
	Status     Status                `cmd:""`
	Services   Services              `cmd:""`
	Config     Config                `cmd:""`
	Pg         PG                    `cmd:""`
	Nodes      Nodes                 `cmd:""`
	Diagnose   Diagnose              `cmd:""`
	Jobs       Jobs                  `cmd:""`
	Logs       Logs                  `cmd:""`
	Completion completion.Completion `cmd:""`
}
type Runtime struct {
	CLI *CLI
	app *app.App
}

func (r *Runtime) App(ctx context.Context) (*app.App, error) {
	if r.app == nil {
		a, e := app.Open(ctx, r.CLI.StateDir)
		if e != nil {
			return nil, e
		}
		r.app = a
	}
	return r.app, nil
}
func (r *Runtime) Print(v any) error {
	enc := json.NewEncoder(os.Stdout)
	if !r.CLI.JSON {
		enc.SetIndent("", "  ")
	}
	return enc.Encode(map[string]any{"schemaVersion": 1, "data": v})
}
func decode(path string, dst any) error {
	var reader io.Reader = os.Stdin
	if path != "-" {
		f, e := os.Open(path)
		if e != nil {
			return e
		}
		defer f.Close()
		reader = f
	}
	d := json.NewDecoder(io.LimitReader(reader, 4<<20))
	d.DisallowUnknownFields()
	if e := d.Decode(dst); e != nil {
		return errors.New("invalid configuration JSON")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("expected exactly one JSON value")
	}
	return nil
}
func strictJSON(raw []byte, dst any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(dst) != nil {
		return errors.New("invalid configuration JSON or unknown field")
	}
	return nil
}
func password(stdin bool, generate bool) (string, error) {
	if generate {
		return "", nil
	}
	if !stdin {
		return "", errors.New("use --stdin or --generate-password")
	}
	b, e := io.ReadAll(io.LimitReader(os.Stdin, 16385))
	if e != nil || len(b) > 16384 {
		return "", errors.New("invalid password input")
	}
	return strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r"), nil
}

type Init struct {
	Engine   string `enum:"auto,docker,podman" default:"auto"`
	Endpoint string
}

func (c *Init) Run(ctx context.Context, r *Runtime) error {
	if e := app.Init(ctx, r.CLI.StateDir, c.Engine, c.Endpoint); e != nil {
		return e
	}
	return r.Print(map[string]string{"message": "workspace initialized; run services start or serve --autostart"})
}

type Doctor struct{}

func (c *Doctor) Run(ctx context.Context, r *Runtime) error {
	a, e := r.App(ctx)
	if e != nil {
		return e
	}
	if e = a.Engine.Ping(ctx); e != nil {
		return errors.New("engine socket is unavailable; start Docker or podman.socket")
	}
	return r.Print(map[string]string{"engine": "reachable", "stateDir": a.Store.Dir})
}

type Serve struct {
	Listen    string `default:"127.0.0.1:8318"`
	Autostart bool
}

func (c *Serve) Run(ctx context.Context, r *Runtime) error {
	if e := app.Init(ctx, r.CLI.StateDir, "auto", ""); e != nil {
		return e
	}
	a, e := r.App(ctx)
	if e != nil {
		return e
	}
	return web.Serve(ctx, a, c.Listen, c.Autostart)
}

type Status struct{}

func (c *Status) Run(ctx context.Context, r *Runtime) error {
	a, e := r.App(ctx)
	if e != nil {
		return e
	}
	v, e := a.Status(ctx)
	if e != nil {
		return e
	}
	return r.Print(v)
}

func Main(ctx context.Context, args []string) int {
	if len(args) == 0 {
		args = []string{"serve", "--autostart"}
	}
	home, _ := os.UserHomeDir()
	defaultDir := filepath.Join(home, ".local/state/dev-runtime")
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		defaultDir = filepath.Join(d, "dev-runtime")
	}
	c := &CLI{}
	parser, e := kong.New(c, kong.Name("dev-runtime"), kong.Description("Local development services for people and agents"), kong.Vars{"state_dir": defaultDir}, kong.Writers(os.Stdout, os.Stderr))
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		return 2
	}
	nodePredictor := complete.PredictFunc(func(prediction complete.Args) []string {
		dir := os.Getenv("DEV_RUNTIME_STATE_DIR")
		if dir == "" {
			dir = defaultDir
		}
		for i, arg := range prediction.All {
			if strings.HasPrefix(arg, "--state-dir=") {
				dir = strings.TrimPrefix(arg, "--state-dir=")
			}
			if arg == "--state-dir" && i+1 < len(prediction.All) {
				dir = prediction.All[i+1]
			}
		}
		if _, e := os.Stat(filepath.Join(dir, "state.db")); e != nil {
			return []string{"direct"}
		}
		db, e := sql.Open("sqlite", "file:"+filepath.Join(dir, "state.db")+"?mode=ro")
		if e != nil {
			return nil
		}
		defer db.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		rows, e := db.QueryContext(ctx, "SELECT id FROM resources WHERE kind='node'")
		if e != nil {
			return nil
		}
		defer rows.Close()
		out := []string{"direct"}
		for rows.Next() {
			var id string
			if rows.Scan(&id) == nil {
				out = append(out, id)
			}
		}
		return out
	})
	completion.Register(parser, completion.WithPredictor("service", complete.PredictSet(app.IDs...)), completion.WithPredictor("node", nodePredictor))
	parsed, e := parser.Parse(args)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		return 2
	}
	r := &Runtime{CLI: c}
	defer func() {
		if r.app != nil {
			r.app.Close()
		}
	}()
	if parsed.Command() != "serve" {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	parsed.BindTo(ctx, (*context.Context)(nil))
	if e = parsed.Run(r); e != nil {
		code := "operation_failed"
		if errors.Is(e, state.ErrConflict) {
			code = "revision_conflict"
		}
		if c.JSON {
			_ = json.NewEncoder(os.Stderr).Encode(map[string]any{"schemaVersion": 1, "error": map[string]string{"code": code, "message": e.Error()}})
		} else {
			fmt.Fprintln(os.Stderr, "dev-runtime:", e)
		}
		return 1
	}
	return 0
}
