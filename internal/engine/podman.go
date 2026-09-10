package engine

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/opencontainers/runtime-spec/specs-go"
	nt "go.podman.io/common/libnetwork/types"
	"go.podman.io/podman/v6/pkg/api/handlers"
	"go.podman.io/podman/v6/pkg/bindings"
	"go.podman.io/podman/v6/pkg/bindings/containers"
	"go.podman.io/podman/v6/pkg/bindings/images"
	"go.podman.io/podman/v6/pkg/bindings/network"
	"go.podman.io/podman/v6/pkg/bindings/system"
	"go.podman.io/podman/v6/pkg/specgen"
)

type Podman struct{ endpoint string }

func NewPodman(endpoint string) (Engine, error) { return &Podman{endpoint}, nil }
func (p *Podman) connection(ctx context.Context) (context.Context, error) {
	if _, e := os.Stat(strings.TrimPrefix(p.endpoint, "unix://")); e != nil {
		return nil, e
	}
	return bindings.NewConnection(ctx, p.endpoint)
}
func (p *Podman) Close() error { return nil }
func (p *Podman) Ping(ctx context.Context) error {
	ctx, e := p.connection(ctx)
	if e != nil {
		return e
	}
	_, e = system.Info(ctx, nil)
	return e
}
func (p *Podman) Inspect(ctx context.Context, n string) (*Container, error) {
	ctx, e := p.connection(ctx)
	if e != nil {
		return nil, e
	}
	exists, e := containers.Exists(ctx, n, nil)
	if e != nil {
		return nil, e
	}
	if !exists {
		return nil, nil
	}
	r, e := containers.Inspect(ctx, n, nil)
	if e != nil {
		return nil, e
	}
	return &Container{ID: r.ID, State: r.State.Status, Running: r.State.Running, Labels: r.Config.Labels}, nil
}
func (p *Podman) Pull(ctx context.Context, image string) error {
	ctx, e := p.connection(ctx)
	if e != nil {
		return e
	}
	_, e = images.Pull(ctx, image, new(images.PullOptions).WithQuiet(true))
	return e
}
func (p *Podman) Create(ctx context.Context, s Spec) error {
	ctx, e := p.connection(ctx)
	if e != nil {
		return e
	}
	spec := specgen.NewSpecGenerator(s.Service.Image, false)
	spec.Name = s.Name
	spec.Command = s.Service.Args
	spec.Env = s.Service.Env
	spec.User = s.Service.User
	spec.Labels = s.Labels
	spec.RestartPolicy = s.Service.Restart
	spec.LogConfiguration = &specgen.LogConfig{Driver: "k8s-file", Size: 16 << 20}
	if s.Service.Memlock != nil {
		n := uint64(*s.Service.Memlock)
		spec.Rlimits = []specs.POSIXRlimit{{Type: "RLIMIT_MEMLOCK", Soft: n, Hard: n}}
	}
	spec.NetNS = specgen.Namespace{NSMode: specgen.Bridge}
	spec.Networks = map[string]nt.PerNetworkOptions{}
	for _, n := range s.Networks {
		spec.Networks[n] = nt.PerNetworkOptions{Aliases: s.Aliases}
	}
	for _, p := range s.Service.Ports {
		spec.PortMappings = append(spec.PortMappings, nt.PortMapping{HostIP: p.Host, HostPort: p.Published, ContainerPort: p.Target, Protocol: "tcp"})
	}
	for _, m := range s.Service.Mounts {
		opts := []string{"rw"}
		if m.ReadOnly {
			opts = []string{"ro"}
		}
		if m.Kind == "volume" {
			spec.Volumes = append(spec.Volumes, &specgen.NamedVolume{Name: m.Source, Dest: m.Target, Options: opts})
		} else {
			spec.Mounts = append(spec.Mounts, specs.Mount{Type: "bind", Source: m.Source, Destination: m.Target, Options: opts})
		}
	}
	if s.Service.Memory > 0 {
		spec.ResourceLimits = &specs.LinuxResources{Memory: &specs.LinuxMemory{Limit: &s.Service.Memory}}
	}
	if s.Service.CPUs > 0 {
		if spec.ResourceLimits == nil {
			spec.ResourceLimits = &specs.LinuxResources{}
		}
		quota := int64(s.Service.CPUs * 100000)
		period := uint64(100000)
		spec.ResourceLimits.CPU = &specs.LinuxCPU{Quota: &quota, Period: &period}
	}
	_, e = containers.CreateWithSpec(ctx, spec, nil)
	return e
}
func (p *Podman) Start(ctx context.Context, n string) error {
	ctx, e := p.connection(ctx)
	if e != nil {
		return e
	}
	return containers.Start(ctx, n, nil)
}
func (p *Podman) Stop(ctx context.Context, n string) error {
	ctx, e := p.connection(ctx)
	if e != nil {
		return e
	}
	return containers.Stop(ctx, n, nil)
}
func (p *Podman) Remove(ctx context.Context, n string) error {
	ctx, e := p.connection(ctx)
	if e != nil {
		return e
	}
	_, e = containers.Remove(ctx, n, nil)
	return e
}
func (p *Podman) Network(ctx context.Context, n string, internal bool) error {
	ctx, e := p.connection(ctx)
	if e != nil {
		return e
	}
	exists, e := network.Exists(ctx, n, nil)
	if e != nil || exists {
		return e
	}
	_, e = network.Create(ctx, &nt.Network{Name: n, Internal: internal, DNSEnabled: true})
	return e
}
func (p *Podman) Logs(ctx context.Context, n string, w io.Writer) error {
	ctx, e := p.connection(ctx)
	if e != nil {
		return e
	}
	a, b := make(chan string), make(chan string)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, ch := range []chan string{a, b} {
		wg.Add(1)
		go func(c chan string) {
			defer wg.Done()
			for line := range c {
				mu.Lock()
				_, _ = io.WriteString(w, line)
				mu.Unlock()
			}
		}(ch)
	}
	e = containers.Logs(ctx, n, new(containers.LogOptions).WithStdout(true).WithStderr(true).WithTail("200"), a, b)
	close(a)
	close(b)
	wg.Wait()
	return e
}
func (p *Podman) Exec(ctx context.Context, n string, args []string, in io.Reader, out io.Writer) error {
	ctx, e := p.connection(ctx)
	if e != nil {
		return e
	}
	cfg := &handlers.ExecCreateConfig{}
	cfg.Cmd = args
	cfg.AttachStdout = true
	cfg.AttachStderr = true
	cfg.AttachStdin = in != nil
	id, e := containers.ExecCreate(ctx, n, cfg)
	if e != nil {
		return e
	}
	if in == nil {
		in = strings.NewReader("")
	}
	e = containers.ExecStartAndAttach(ctx, id, new(containers.ExecStartAndAttachOptions).WithInputStream(*bufio.NewReader(in)).WithOutputStream(out).WithErrorStream(io.Discard).WithAttachOutput(true).WithAttachError(true).WithAttachInput(true))
	if e != nil {
		return e
	}
	r, e := containers.ExecInspect(ctx, id, nil)
	if e != nil {
		return e
	}
	if r.ExitCode != 0 {
		return errors.New("container command failed")
	}
	return nil
}

func (p *Podman) HasImage(ctx context.Context, image string) (bool, error) {
	ctx, e := p.connection(ctx)
	if e != nil {
		return false, e
	}
	return images.Exists(ctx, image, nil)
}
