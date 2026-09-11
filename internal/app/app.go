package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/PluxelJS/Proxy-LLM-API/internal/engine"
	"github.com/PluxelJS/Proxy-LLM-API/internal/state"
)

type operationKey struct{}

func WithOperation(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, operationKey{}, id)
}

type App struct {
	Store    *state.Store
	Engine   engine.Engine
	Instance string
}
type Config struct {
	SchemaVersion int    `json:"schemaVersion"`
	Engine        string `json:"engine"`
	Endpoint      string `json:"endpoint"`
	Instance      string `json:"instance"`
}

var IDs = []string{"postgres", "dragonfly", "vmetrics", "vlogs", "new-api", "cliproxy", "sing-box", "cloudflared", "api-entry"}

func Open(ctx context.Context, dir string) (*App, error) {
	if _, e := os.Stat(filepath.Join(dir, "state.db")); e != nil {
		return nil, errors.New("workspace is not initialized; run init")
	}
	s, e := state.Open(ctx, dir)
	if e != nil {
		return nil, e
	}
	var cfg Config
	if e = s.Get(ctx, "system", "config", &cfg); e != nil {
		s.Close()
		return nil, errors.New("workspace is not initialized; run init")
	}
	var eng engine.Engine
	switch cfg.Engine {
	case "podman":
		eng, e = engine.NewPodman(cfg.Endpoint)
	case "docker":
		eng, e = engine.NewDocker(cfg.Endpoint)
	default:
		e = errors.New("unsupported engine")
	}
	if e != nil {
		s.Close()
		return nil, e
	}
	return &App{Store: s, Engine: eng, Instance: cfg.Instance}, nil
}
func (a *App) Close() { _ = a.Engine.Close(); _ = a.Store.Close() }
func Init(ctx context.Context, dir, kind, endpoint string) error {
	for _, legacy := range []string{".env", "compose.json", "cliproxy/settings/config.yaml"} {
		if _, e := os.Stat(filepath.Join(dir, legacy)); e == nil {
			if _, ok := os.Stat(filepath.Join(dir, "state.db")); ok != nil {
				return errors.New("directory contains existing deployment data; choose a new empty --state-dir")
			}
		}
	}
	s, e := state.Open(ctx, dir)
	if e != nil {
		return e
	}
	defer s.Close()
	unlock, e := s.Lock(ctx)
	if e != nil {
		return e
	}
	defer unlock()
	var old Config
	if e = s.Get(ctx, "system", "config", &old); e == nil {
		if (kind != "auto" && kind != old.Engine) || (endpoint != "" && endpoint != old.Endpoint) {
			return errors.New("workspace already uses a different engine; choose a new state directory")
		}
		return nil
	}
	if !errors.Is(e, state.ErrNotFound) {
		return e
	}
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	if kind == "auto" {
		if endpoint != "" {
			return errors.New("choose docker or podman with an explicit endpoint")
		}
		if _, e = os.Stat(filepath.Join(runtimeDir, "podman/podman.sock")); e == nil {
			kind = "podman"
		} else if _, e = os.Stat("/var/run/docker.sock"); e == nil {
			kind = "docker"
		} else {
			return errors.New("no engine socket found; start podman.socket or Docker and specify --engine/--endpoint")
		}
	}
	if kind != "docker" && kind != "podman" {
		return errors.New("engine must be docker or podman")
	}
	if endpoint == "" {
		if kind == "docker" {
			endpoint = "unix:///var/run/docker.sock"
		} else {
			endpoint = "unix://" + filepath.Join(runtimeDir, "podman/podman.sock")
		}
	}
	if !strings.HasPrefix(endpoint, "unix:///") {
		return errors.New("this release supports local Unix engine sockets only")
	}
	id := "dr-" + state.ID()[:10]
	a := &App{Store: s, Instance: id}
	defaults, e := a.defaults(ctx, kind)
	if e != nil {
		return e
	}
	for _, v := range defaults {
		if e = s.Put(ctx, "service", v.ID, v); e != nil {
			return e
		}
	}
	return s.Put(ctx, "system", "config", Config{1, kind, endpoint, id})
}
func (a *App) Name(id string) string { return a.Instance + "-" + id }
func (a *App) Services(ctx context.Context) ([]state.Service, error) {
	var v []state.Service
	e := a.Store.List(ctx, "service", &v)
	return v, e
}
func (a *App) Service(ctx context.Context, id string) (state.Service, error) {
	var v state.Service
	e := a.Store.Get(ctx, "service", id, &v)
	return v, e
}

var envKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func Validate(v state.Service) error {
	found := false
	for _, id := range IDs {
		if v.ID == id {
			found = true
		}
	}
	if !found {
		return errors.New("unknown service")
	}
	if strings.TrimSpace(v.Image) == "" || strings.ContainsAny(v.Image, "\r\n\x00 ") {
		return errors.New("image reference is invalid")
	}
	if v.Args == nil || v.Env == nil || v.Ports == nil || v.Mounts == nil {
		return errors.New("args, env, ports and mounts must be present; use empty arrays or objects instead of null")
	}
	if v.Memlock != nil && *v.Memlock < -1 {
		return errors.New("memlock must be -1 or nonnegative")
	}
	if v.Memory < 0 || v.CPUs < 0 {
		return errors.New("resource limits must be nonnegative")
	}
	if v.Restart != "unless-stopped" && v.Restart != "on-failure" && v.Restart != "no" {
		return errors.New("unsupported restart policy")
	}
	for _, arg := range v.Args {
		if strings.ContainsRune(arg, 0) {
			return errors.New("arguments cannot contain NUL")
		}
	}
	for k, val := range v.Env {
		if !envKey.MatchString(k) || strings.ContainsRune(val, 0) {
			return errors.New("invalid environment")
		}
	}
	seen := map[string]bool{}
	for _, p := range v.Ports {
		ip := net.ParseIP(p.Host)
		if ip == nil || !ip.IsLoopback() || p.Published == 0 || p.Target == 0 {
			return errors.New("ports require loopback addresses and nonzero ports")
		}
		key := fmt.Sprintf("%s:%d", p.Host, p.Published)
		if seen[key] {
			return errors.New("duplicate published port")
		}
		seen[key] = true
	}
	for _, m := range v.Mounts {
		if m.Kind != "bind" && m.Kind != "volume" {
			return errors.New("mount kind must be bind or volume")
		}
		if !filepath.IsAbs(m.Target) || m.Source == "" {
			return errors.New("invalid mount")
		}
		if m.Kind == "bind" && !filepath.IsAbs(m.Source) {
			return errors.New("bind source must be absolute")
		}
	}
	return nil
}
func (a *App) Save(ctx context.Context, v state.Service) error {
	if e := Validate(v); e != nil {
		return e
	}
	unlock, e := a.Store.Lock(ctx)
	if e != nil {
		return e
	}
	defer unlock()
	old, e := a.Service(ctx, v.ID)
	if e != nil {
		return e
	}
	if old.Revision != v.Revision {
		return state.ErrConflict
	}
	if v.ID == "postgres" {
		for _, key := range []string{"POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_DB", "PGDATA"} {
			if old.Env[key] != v.Env[key] {
				return errors.New("PostgreSQL initialization fields are immutable; manage existing databases/users through pg commands")
			}
		}
	}
	all, e := a.Services(ctx)
	if e != nil {
		return e
	}
	for _, other := range all {
		if other.ID == v.ID {
			continue
		}
		for _, p := range v.Ports {
			for _, q := range other.Ports {
				if p.Host == q.Host && p.Published == q.Published {
					return fmt.Errorf("port conflicts with %s", other.ID)
				}
			}
		}
	}
	return a.Store.SaveService(ctx, v)
}

// Configuration is returned only on explicit reveal. Normal status never includes credentials.
func Redact(v state.Service) state.Service {
	v.Env = map[string]string{}
	v.Args = []string{"<hidden; use --reveal>"}
	return v
}

type Status struct {
	ID              string            `json:"id"`
	Revision        int64             `json:"revision"`
	Enabled         bool              `json:"enabled"`
	Container       *engine.Container `json:"container"`
	AppliedRevision int64             `json:"appliedRevision"`
	Error           string            `json:"error,omitempty"`
}

func (a *App) Status(ctx context.Context) ([]Status, error) {
	all, e := a.Services(ctx)
	if e != nil {
		return nil, e
	}
	result := []Status{}
	engineCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	for _, v := range all {
		c, inspectErr := a.Engine.Inspect(engineCtx, a.Name(v.ID))
		s := Status{ID: v.ID, Revision: v.Revision, Enabled: v.Enabled, Container: c}
		_ = a.Store.Get(ctx, "applied", v.ID, &s.AppliedRevision)
		if inspectErr != nil {
			s.Error = "engine unavailable"
		}
		result = append(result, s)
	}
	return result, nil
}

type Plan struct {
	Service  string `json:"service"`
	Revision int64  `json:"revision"`
	Exists   bool   `json:"exists"`
	Recreate bool   `json:"recreate"`
	Message  string `json:"message"`
}

func (a *App) spec(v state.Service) engine.Spec {
	spec := engine.Spec{Name: a.Name(v.ID), Service: v, Labels: map[string]string{"dev-runtime.instance": a.Instance, "dev-runtime.service": v.ID}, Aliases: []string{v.ID}, Networks: []string{a.Name("default")}}
	if v.ID == "cliproxy" {
		spec.Aliases = append(spec.Aliases, "cli-proxy-api")
	}
	if v.ID == "cloudflared" {
		spec.Networks = []string{a.Name("tunnel"), a.Name("tunnel-egress")}
	}
	if v.ID == "api-entry" {
		spec.Networks = []string{a.Name("default"), a.Name("tunnel")}
	}
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	spec.Labels["dev-runtime.spec"] = hex.EncodeToString(sum[:])
	return spec
}
func (a *App) Plan(ctx context.Context, id string) (Plan, error) {
	v, e := a.Service(ctx, id)
	if e != nil {
		return Plan{}, e
	}
	if e = Validate(v); e != nil {
		return Plan{}, e
	}
	c, e := a.Engine.Inspect(ctx, a.Name(id))
	if e != nil {
		return Plan{}, errors.New("cannot inspect engine")
	}
	p := Plan{Service: id, Revision: v.Revision, Exists: c != nil, Message: "create container; preserve persistent data"}
	if c != nil {
		if c.Labels["dev-runtime.instance"] != a.Instance {
			return p, errors.New("container is not owned by this workspace")
		}
		p.Recreate = c.Labels["dev-runtime.spec"] != a.spec(v).Labels["dev-runtime.spec"]
		p.Message = "start existing container"
		if p.Recreate {
			p.Message = "recreate container; persistent data is retained"
		}
	}
	return p, nil
}
func (a *App) Run(ctx context.Context, kind, id string, fn func(context.Context) error) (state.Operation, error) {
	unlock, e := a.Store.Lock(ctx)
	if e != nil {
		return state.Operation{}, e
	}
	defer unlock()
	if e = a.Store.Recover(ctx); e != nil {
		return state.Operation{}, e
	}
	operationID, _ := ctx.Value(operationKey{}).(string)
	if operationID == "" {
		operationID = state.ID()
	}
	o := state.Operation{ID: operationID, Kind: kind, Resource: id, Status: "running", Stage: "executing", Started: time.Now().UTC()}
	if e = a.Store.Record(ctx, o); e != nil {
		return o, e
	}
	e = fn(ctx)
	now := time.Now().UTC()
	o.Finished = &now
	o.Status = "succeeded"
	o.Stage = "complete"
	if e != nil {
		o.Status = "failed"
		o.Error = e.Error()
		if ctx.Err() != nil {
			o.Status = "interrupted"
			o.Error = "operation interrupted; inspect actual state before retry"
		}
	}
	recordCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.Store.Record(recordCtx, o); err != nil {
		return o, err
	}
	return o, e
}
func (a *App) Lifecycle(ctx context.Context, command, id string, revision int64) (state.Operation, error) {
	return a.Run(ctx, command, id, func(ctx context.Context) error {
		v, e := a.Service(ctx, id)
		if e != nil {
			return e
		}
		if revision > 0 && revision != v.Revision {
			return state.ErrConflict
		}
		switch command {
		case "enable", "disable":
			v.Enabled = command == "enable"
			v.Revision++
			if e = a.Store.Put(ctx, "service", id, v); e != nil {
				return e
			}
			if command == "disable" {
				return a.stop(ctx, id)
			}
			return a.up(ctx, id, false)
		case "start", "apply":
			return a.up(ctx, id, false)
		case "restart":
			return a.up(ctx, id, true)
		case "stop":
			return a.stop(ctx, id)
		case "pull":
			if e = a.Engine.Pull(ctx, v.Image); e != nil {
				return errors.New("image pull failed; check engine connectivity and registry credentials")
			}
			// Pulling a mutable tag invalidates the previous container even if the tag text did not change.
			return a.Store.SaveService(ctx, v)
		default:
			return errors.New("unknown lifecycle action")
		}
	})
}
func (a *App) stop(ctx context.Context, id string) error {
	c, e := a.Engine.Inspect(ctx, a.Name(id))
	if e != nil {
		return errors.New("engine inspection failed")
	}
	if c == nil {
		return nil
	}
	if c.Labels["dev-runtime.instance"] != a.Instance {
		return errors.New("container ownership mismatch")
	}
	if c.Running {
		if e = a.Engine.Stop(ctx, a.Name(id)); e != nil {
			return errors.New("container stop failed")
		}
	}
	return nil
}
func (a *App) up(ctx context.Context, id string, force bool) error {
	if id == "cloudflared" {
		service, e := a.Service(ctx, id)
		if e != nil {
			return e
		}
		if service.Env["TUNNEL_TOKEN"] == "" {
			return errors.New("set cloudflared TUNNEL_TOKEN before starting the tunnel")
		}
	}
	if id == "cliproxy" {
		var node string
		_ = a.Store.Get(ctx, "system", "active-node", &node)
		if node != "" {
			if e := a.up(ctx, "sing-box", false); e != nil {
				return e
			}
		}
	}
	if id == "cloudflared" {
		if e := a.up(ctx, "cliproxy", false); e != nil {
			return e
		}
		if e := a.up(ctx, "api-entry", false); e != nil {
			return e
		}
	}
	v, e := a.Service(ctx, id)
	if e != nil {
		return e
	}
	if e = Validate(v); e != nil {
		return e
	}
	if id == "sing-box" {
		if _, e = os.Stat(a.Store.File("generated/sing-box.json")); e != nil {
			return errors.New("choose and apply a proxy node first")
		}
	}
	p, e := a.Plan(ctx, id)
	if e != nil {
		return e
	}
	if !p.Exists || p.Recreate || force {
		// Pull before stopping the current container. A registry failure leaves it running.
		cached, err := a.Engine.HasImage(ctx, v.Image)
		if err != nil {
			return errors.New("image inspection failed")
		}
		if !cached {
			if e = a.Engine.Pull(ctx, v.Image); e != nil {
				return errors.New("image pull failed; existing container was preserved")
			}
		}
		for _, n := range a.spec(v).Networks {
			if e = a.Engine.Network(ctx, n, n == a.Name("tunnel")); e != nil {
				return errors.New("network setup failed")
			}
		}
		if p.Exists {
			if e = a.stop(ctx, id); e != nil {
				return e
			}
			if e = a.Engine.Remove(ctx, a.Name(id)); e != nil {
				return errors.New("container removal failed")
			}
		}
		if e = a.Engine.Create(ctx, a.spec(v)); e != nil {
			return errors.New("container creation failed; saved configuration and volumes retained")
		}
	}
	c, e := a.Engine.Inspect(ctx, a.Name(id))
	if e != nil || c == nil {
		return errors.New("created container cannot be inspected")
	}
	if !c.Running {
		if e = a.Engine.Start(ctx, a.Name(id)); e != nil {
			return errors.New("container start failed")
		}
	}
	if e = a.Store.Put(ctx, "applied", id, v.Revision); e != nil {
		return e
	}
	return nil
}
func (a *App) StartEnabled(ctx context.Context) error {
	all, e := a.Services(ctx)
	if e != nil {
		return e
	}
	var failures []error
	for _, s := range all {
		if ctx.Err() != nil {
			return errors.Join(append(failures, ctx.Err())...)
		}
		if s.Enabled {
			step, cancel := context.WithTimeout(ctx, 15*time.Minute)
			_, e = a.Lifecycle(step, "start", s.ID, 0)
			cancel()
			if e != nil {
				failures = append(failures, fmt.Errorf("%s: %w", s.ID, e))
			}
		}
	}
	return errors.Join(failures...)
}
func HTTPClient() *http.Client {
	return &http.Client{Timeout: 35 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
