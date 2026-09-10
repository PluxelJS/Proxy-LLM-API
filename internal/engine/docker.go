package engine

import (
	"context"
	"errors"
	"io"
	"net/netip"
	"sort"
	"strconv"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	"github.com/containerd/errdefs"
)

type Docker struct{ c *client.Client }

func NewDocker(endpoint string) (Engine, error) {
	c, err := client.New(client.WithHost(endpoint))
	if err != nil {
		return nil, err
	}
	return &Docker{c}, nil
}
func (d *Docker) Close() error { return d.c.Close() }
func (d *Docker) Ping(ctx context.Context) error {
	_, err := d.c.Ping(ctx, client.PingOptions{})
	return err
}
func (d *Docker) Inspect(ctx context.Context, name string) (*Container, error) {
	r, err := d.c.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
	if errdefs.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &Container{ID: r.Container.ID, State: string(r.Container.State.Status), Running: r.Container.State.Running, Labels: r.Container.Config.Labels}, nil
}
func (d *Docker) Pull(ctx context.Context, image string) error {
	r, err := d.c.ImagePull(ctx, image, client.ImagePullOptions{})
	if err != nil {
		return err
	}
	defer r.Close()
	return r.Wait(ctx)
}
func (d *Docker) Create(ctx context.Context, s Spec) error {
	cfg := &container.Config{Image: s.Service.Image, Cmd: s.Service.Args, User: s.Service.User, Labels: s.Labels, ExposedPorts: network.PortSet{}}
	keys := []string{}
	for k := range s.Service.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		cfg.Env = append(cfg.Env, k+"="+s.Service.Env[k])
	}
	host := &container.HostConfig{PortBindings: network.PortMap{}, RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyMode(s.Service.Restart)}, LogConfig: container.LogConfig{Type: "json-file", Config: map[string]string{"max-size": "16m", "max-file": "3"}}}
	if s.Service.Memlock != nil {
		host.Ulimits = []*container.Ulimit{{Name: "memlock", Soft: *s.Service.Memlock, Hard: *s.Service.Memlock}}
	}
	host.Memory = s.Service.Memory
	host.NanoCPUs = int64(s.Service.CPUs * 1e9)
	for _, p := range s.Service.Ports {
		k := network.MustParsePort(strconv.Itoa(int(p.Target)) + "/tcp")
		cfg.ExposedPorts[k] = struct{}{}
		host.PortBindings[k] = []network.PortBinding{{HostIP: netip.MustParseAddr(p.Host), HostPort: strconv.Itoa(int(p.Published))}}
	}
	for _, m := range s.Service.Mounts {
		host.Mounts = append(host.Mounts, mount.Mount{Type: mount.Type(m.Kind), Source: m.Source, Target: m.Target, ReadOnly: m.ReadOnly})
	}
	nc := &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{}}
	for _, n := range s.Networks {
		nc.EndpointsConfig[n] = &network.EndpointSettings{Aliases: s.Aliases}
	}
	_, err := d.c.ContainerCreate(ctx, client.ContainerCreateOptions{Name: s.Name, Config: cfg, HostConfig: host, NetworkingConfig: nc})
	return err
}
func (d *Docker) Start(ctx context.Context, n string) error {
	_, e := d.c.ContainerStart(ctx, n, client.ContainerStartOptions{})
	return e
}
func (d *Docker) Stop(ctx context.Context, n string) error {
	_, e := d.c.ContainerStop(ctx, n, client.ContainerStopOptions{})
	return e
}
func (d *Docker) Remove(ctx context.Context, n string) error {
	_, e := d.c.ContainerRemove(ctx, n, client.ContainerRemoveOptions{})
	return e
}
func (d *Docker) Network(ctx context.Context, n string, internal bool) error {
	_, e := d.c.NetworkInspect(ctx, n, client.NetworkInspectOptions{})
	if e == nil {
		return nil
	}
	if !errdefs.IsNotFound(e) {
		return e
	}
	_, e = d.c.NetworkCreate(ctx, n, client.NetworkCreateOptions{Internal: internal})
	return e
}
func (d *Docker) Logs(ctx context.Context, n string, w io.Writer) error {
	r, e := d.c.ContainerLogs(ctx, n, client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true, Tail: "200"})
	if e != nil {
		return e
	}
	defer r.Close()
	_, e = stdcopy.StdCopy(w, w, r)
	return e
}
func (d *Docker) Exec(ctx context.Context, n string, args []string, in io.Reader, out io.Writer) error {
	r, e := d.c.ExecCreate(ctx, n, client.ExecCreateOptions{Cmd: args, AttachStdout: true, AttachStderr: true, AttachStdin: in != nil})
	if e != nil {
		return e
	}
	attached, e := d.c.ExecAttach(ctx, r.ID, client.ExecAttachOptions{})
	if e != nil {
		return e
	}
	defer attached.Close()
	stopCancel := context.AfterFunc(ctx, func() { attached.Close() })
	defer stopCancel()
	if in != nil {
		go func() { _, _ = io.Copy(attached.Conn, in); _ = attached.CloseWrite() }()
	}
	if _, e = stdcopy.StdCopy(out, io.Discard, attached.Reader); e != nil {
		return e
	}
	status, e := d.c.ExecInspect(ctx, r.ID, client.ExecInspectOptions{})
	if e != nil {
		return e
	}
	if status.ExitCode != 0 {
		return errors.New("container command failed")
	}
	return nil
}

func (d *Docker) HasImage(ctx context.Context, image string) (bool, error) {
	_, e := d.c.ImageInspect(ctx, image)
	if errdefs.IsNotFound(e) {
		return false, nil
	}
	return e == nil, e
}
