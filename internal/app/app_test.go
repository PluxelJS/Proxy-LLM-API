package app

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/PluxelJS/Proxy-LLM-API/internal/engine"
	"github.com/PluxelJS/Proxy-LLM-API/internal/state"
)

type fakeEngine struct {
	containers map[string]*engine.Container
	creates    []engine.Spec
	failPull   bool
	cached     bool
}

func (f *fakeEngine) Ping(context.Context) error { return nil }
func (f *fakeEngine) Close() error               { return nil }
func (f *fakeEngine) Inspect(_ context.Context, n string) (*engine.Container, error) {
	return f.containers[n], nil
}
func (f *fakeEngine) HasImage(context.Context, string) (bool, error) { return f.cached, nil }
func (f *fakeEngine) Pull(context.Context, string) error {
	if f.failPull {
		return errors.New("private-token-in-registry-error")
	}
	return nil
}
func (f *fakeEngine) Create(_ context.Context, s engine.Spec) error {
	f.creates = append(f.creates, s)
	f.containers[s.Name] = &engine.Container{ID: s.Name, State: "created", Labels: s.Labels}
	return nil
}
func (f *fakeEngine) Start(_ context.Context, n string) error {
	f.containers[n].Running = true
	f.containers[n].State = "running"
	return nil
}
func (f *fakeEngine) Stop(_ context.Context, n string) error {
	f.containers[n].Running = false
	return nil
}
func (f *fakeEngine) Remove(_ context.Context, n string) error                           { delete(f.containers, n); return nil }
func (f *fakeEngine) Network(context.Context, string, bool) error                        { return nil }
func (f *fakeEngine) Logs(context.Context, string, io.Writer) error                      { return nil }
func (f *fakeEngine) Exec(context.Context, string, []string, io.Reader, io.Writer) error { return nil }
func testApp(t *testing.T) (*App, *fakeEngine) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	if e := Init(ctx, dir, "podman", "unix:///tmp/test-podman.sock"); e != nil {
		t.Fatal(e)
	}
	a, e := Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(a.Close)
	f := &fakeEngine{containers: map[string]*engine.Container{}}
	a.Engine = f
	return a, f
}
func TestDefaultsAndRevision(t *testing.T) {
	a, f := testApp(t)
	ctx := context.Background()
	s, e := a.Service(ctx, "dragonfly")
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"--logtostderr", "--dir=/data", "--dbfilename=dump", "--snapshot_cron=*/5 * * * *", "--maxmemory=1gb", "--proactor_threads=2", "--default_lua_flags=allow-undeclared-keys"}
	if !reflect.DeepEqual(s.Args, want) {
		t.Fatalf("defaults: %v", s.Args)
	}
	if _, e = a.Lifecycle(ctx, "start", s.ID, 1); e != nil {
		t.Fatal(e)
	}
	if len(f.creates) != 1 {
		t.Fatal("not created")
	}
	s.Args = append(s.Args, "--cache_mode=true")
	if e = a.Save(ctx, s); e != nil {
		t.Fatal(e)
	}
	if e = a.Save(ctx, s); !errors.Is(e, state.ErrConflict) {
		t.Fatal("stale edit accepted", e)
	}
	if _, e = a.Lifecycle(ctx, "apply", s.ID, 1); !errors.Is(e, state.ErrConflict) {
		t.Fatal("stale apply accepted", e)
	}
	if _, e = a.Lifecycle(ctx, "apply", s.ID, 2); e != nil {
		t.Fatal(e)
	}
	if len(f.creates) != 2 {
		t.Fatal("configuration did not recreate")
	}
	if !reflect.DeepEqual(f.creates[0].Service.Mounts, f.creates[1].Service.Mounts) {
		t.Fatal("data mount changed")
	}
}
func TestPullFailurePreservesContainer(t *testing.T) {
	a, f := testApp(t)
	ctx := context.Background()
	if _, e := a.Lifecycle(ctx, "start", "postgres", 1); e != nil {
		t.Fatal(e)
	}
	f.failPull = true
	s, _ := a.Service(ctx, "postgres")
	s.Args = []string{"-c", "max_connections=123"}
	if e := a.Save(ctx, s); e != nil {
		t.Fatal(e)
	}
	o, e := a.Lifecycle(ctx, "apply", "postgres", 2)
	if e == nil || o.Status != "failed" {
		t.Fatal("pull failure not recorded")
	}
	if o.Error == "private-token-in-registry-error" {
		t.Fatal("secret leaked")
	}
	if !f.containers[a.Name("postgres")].Running {
		t.Fatal("old container was stopped")
	}
}
func TestForeignContainerRejected(t *testing.T) {
	a, f := testApp(t)
	f.containers[a.Name("postgres")] = &engine.Container{Labels: map[string]string{"dev-runtime.instance": "someone-else"}, Running: true}
	if _, e := a.Lifecycle(context.Background(), "stop", "postgres", 1); e == nil {
		t.Fatal("foreign container modified")
	}
}
func TestLockCancelsWithoutMutation(t *testing.T) {
	a, _ := testApp(t)
	unlock, e := a.Store.Lock(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, e = a.Lifecycle(ctx, "start", "postgres", 1); e == nil {
		t.Fatal("operation lock ignored")
	}
}
func TestInitRefusesExistingDeployment(t *testing.T) {
	dir := t.TempDir()
	if e := os.WriteFile(filepath.Join(dir, ".env"), []byte("PRIVATE=keep"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := Init(context.Background(), dir, "podman", "unix:///tmp/x"); e == nil {
		t.Fatal("old deployment overwritten")
	}
	if _, e := os.Stat(filepath.Join(dir, "state.db")); !os.IsNotExist(e) {
		t.Fatal("init wrote into old directory")
	}
}
func TestOpenDoesNotInitialize(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	if _, e := Open(context.Background(), dir); e == nil {
		t.Fatal("open initialized state")
	}
	if _, e := os.Stat(dir); !os.IsNotExist(e) {
		t.Fatal("read operation created directory")
	}
}

func TestBundleConflictIsAtomic(t *testing.T) {
	a, _ := testApp(t)
	ctx := context.Background()
	b, err := a.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	old, _ := a.Service(ctx, "dragonfly")
	newer, _ := a.Service(ctx, "postgres")
	newer.Args = []string{"-c", "max_connections=150"}
	if err = a.Save(ctx, newer); err != nil {
		t.Fatal(err)
	}
	for i := range b.Services {
		if b.Services[i].ID == "dragonfly" {
			b.Services[i].Args = []string{"--cache_mode=true"}
		}
	}
	if err = a.SaveBundle(ctx, b); !errors.Is(err, state.ErrConflict) {
		t.Fatal("stale bundle accepted", err)
	}
	unchanged, _ := a.Service(ctx, "dragonfly")
	if !reflect.DeepEqual(old, unchanged) {
		t.Fatal("part of failed bundle was saved")
	}
	b, _ = a.Export(ctx)
	if err = a.SaveBundle(ctx, b); err != nil {
		t.Fatal(err)
	}
	updated, _ := a.Service(ctx, "dragonfly")
	if updated.Revision != old.Revision+1 {
		t.Fatal("bundle revision not advanced")
	}
}

func TestRepeatedInitPreservesIdentityAndSecrets(t *testing.T) {
	a, _ := testApp(t)
	ctx := context.Background()
	before, _ := a.Store.Secret(ctx, "postgres-admin")
	if err := Init(ctx, a.Store.Dir, "auto", ""); err != nil {
		t.Fatal(err)
	}
	after, _ := a.Store.Secret(ctx, "postgres-admin")
	if before != after {
		t.Fatal("idempotent init rotated credentials")
	}
	var config Config
	if err := a.Store.Get(ctx, "system", "config", &config); err != nil || config.Instance != a.Instance {
		t.Fatal("identity changed", err)
	}
}
