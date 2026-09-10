package app

import (
	"context"
	"fmt"
	"os"

	"github.com/PluxelJS/Proxy-LLM-API/internal/state"
	"go.yaml.in/yaml/v3"
)

func (a *App) defaults(ctx context.Context, kind string) ([]state.Service, error) {
	pg, e := a.Store.EnsureSecret(ctx, "postgres-admin")
	if e != nil {
		return nil, e
	}
	session, e := a.Store.EnsureSecret(ctx, "new-api-session")
	if e != nil {
		return nil, e
	}
	key, e := a.Store.EnsureSecret(ctx, "management-key")
	if e != nil {
		return nil, e
	}
	apiKey, e := a.Store.EnsureSecret(ctx, "api-key")
	if e != nil {
		return nil, e
	}
	for _, dir := range []string{"cliproxy/auth", "cliproxy/settings", "cliproxy/logs", "new-api", "generated"} {
		if e = os.MkdirAll(a.Store.File(dir), 0700); e != nil {
			return nil, e
		}
	}
	config := map[string]any{"host": "", "port": 8317, "auth-dir": "/data/auth", "api-keys": []string{apiKey}, "proxy-url": "direct", "remote-management": map[string]any{"allow-remote": true, "secret-key": key, "disable-control-panel": false}, "usage-statistics-enabled": true}
	b, e := yaml.Marshal(config)
	if e != nil {
		return nil, e
	}
	if e = a.Store.Write("cliproxy/settings/config.yaml", b); e != nil {
		return nil, e
	}
	if e = a.Store.Write("generated/sing-box.json", []byte(`{"inbounds":[{"type":"mixed","listen":"0.0.0.0","listen_port":1080}],"outbounds":[{"type":"direct","tag":"proxy"}],"route":{"final":"proxy"}}`)); e != nil {
		return nil, e
	}
	relay := `pid /tmp/nginx.pid;
error_log /dev/stderr warn;
events { worker_connections 1024; }
http {
 access_log off;
 client_body_temp_path /tmp/client_body;
 proxy_temp_path /tmp/proxy;
 map $http_upgrade $connection_upgrade { default upgrade; '' close; }
 server {
  listen 8080;
  client_max_body_size 100m;
  location = /healthz { return 200 'ok'; }
  location ~ ^/(v1|v1beta)/ {
   proxy_pass http://cli-proxy-api:8317;
   proxy_http_version 1.1;
   proxy_set_header Upgrade $http_upgrade;
   proxy_set_header Connection $connection_upgrade;
   proxy_buffering off;
   proxy_request_buffering off;
   proxy_read_timeout 3600s;
  }
  location / { return 404; }
 }
}`
	if e = a.Store.Write("generated/nginx.conf", []byte(relay)); e != nil {
		return nil, e
	}
	base := func(id, image string, port uint16) state.Service {
		s := state.Service{ID: id, Revision: 1, Image: image, Restart: "unless-stopped", Args: []string{}, Env: map[string]string{}, Mounts: []state.Mount{}, Ports: []state.Port{}}
		if port > 0 {
			s.Ports = []state.Port{{Host: "127.0.0.1", Published: port, Target: port}}
		}
		return s
	}
	volume := func(id, target string) state.Mount {
		return state.Mount{Kind: "volume", Source: a.Name(id) + "-data", Target: target}
	}
	bind := func(path, target string, ro bool) state.Mount {
		return state.Mount{Kind: "bind", Source: a.Store.File(path), Target: target, ReadOnly: ro}
	}
	p := base("postgres", "docker.io/library/postgres:18", 5432)
	p.Enabled = true
	p.Env = map[string]string{"POSTGRES_USER": "postgres", "POSTGRES_PASSWORD": pg, "POSTGRES_DB": "postgres", "PGDATA": "/data/pgdata", "TZ": "Asia/Taipei"}
	p.Mounts = []state.Mount{volume("postgres", "/data")}
	d := base("dragonfly", "docker.dragonflydb.io/dragonflydb/dragonfly:v1.40.0", 6379)
	d.Enabled = true
	d.Args = []string{"--logtostderr", "--dir=/data", "--dbfilename=dump", "--snapshot_cron=*/5 * * * *", "--maxmemory=1gb", "--proactor_threads=2", "--default_lua_flags=allow-undeclared-keys"}
	d.Mounts = []state.Mount{volume("dragonfly", "/data")}
	m := base("vmetrics", "docker.io/victoriametrics/victoria-metrics:v1.151.0", 8428)
	m.Args = []string{"-storageDataPath=/storage", "-retentionPeriod=30d", "-httpListenAddr=:8428"}
	m.Mounts = []state.Mount{volume("vmetrics", "/storage")}
	l := base("vlogs", "docker.io/victoriametrics/victoria-logs:v1.52.0", 9428)
	l.Args = []string{"-storageDataPath=/storage", "-retentionPeriod=7d", "-httpListenAddr=:9428"}
	l.Mounts = []state.Mount{volume("vlogs", "/storage")}
	n := base("new-api", "docker.io/calciumion/new-api:v0.13.2", 23000)
	n.Ports[0].Target = 3000
	n.Env = map[string]string{"TZ": "Asia/Taipei", "SESSION_SECRET": session, "SQLITE_PATH": "/data/new-api.db", "ERROR_LOG_ENABLED": "true", "GIN_MODE": "release"}
	n.Mounts = []state.Mount{bind("new-api", "/data", false)}
	c := base("cliproxy", "docker.io/eceasy/cli-proxy-api:latest", 8317)
	c.Args = []string{"./CLIProxyAPI", "-config", "/data/config/config.yaml"}
	c.Env = map[string]string{"TZ": "Asia/Taipei", "MANAGEMENT_PASSWORD": key}
	c.Mounts = []state.Mount{bind("cliproxy/settings", "/data/config", false), bind("cliproxy/auth", "/data/auth", false), bind("cliproxy/logs", "/CLIProxyAPI/logs", false)}
	s := base("sing-box", "ghcr.io/sagernet/sing-box:v1.13.16", 0)
	s.Args = []string{"run", "-c", "/etc/sing-box/config.json"}
	s.Mounts = []state.Mount{bind("generated/sing-box.json", "/etc/sing-box/config.json", true)}
	t := base("cloudflared", "docker.io/cloudflare/cloudflared:2026.7.3", 0)
	t.Args = []string{"tunnel", "--no-autoupdate", "run"}
	t.Env["TUNNEL_TOKEN"] = ""
	r := base("api-entry", "docker.io/library/nginx:1.30.0-alpine", 0)
	r.Args = []string{"nginx", "-c", "/etc/nginx/nginx.conf", "-g", "daemon off;"}
	r.Mounts = []state.Mount{bind("generated/nginx.conf", "/etc/nginx/nginx.conf", true)}
	if kind == "docker" {
		user := fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
		n.User = user
		c.User = user
		s.User = user
		r.User = user
	}
	return []state.Service{p, d, m, l, n, c, s, t, r}, nil
}
