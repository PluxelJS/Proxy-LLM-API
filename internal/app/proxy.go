package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	nodeparser "github.com/PluxelJS/Proxy-LLM-API/internal/proxy"
	"github.com/PluxelJS/Proxy-LLM-API/internal/state"
	"github.com/redis/go-redis/v9"
	"go.yaml.in/yaml/v3"
)

func (a *App) Nodes(ctx context.Context, reveal bool) ([]state.Node, error) {
	var n []state.Node
	e := a.Store.List(ctx, "node", &n)
	if !reveal {
		for i := range n {
			n[i].URL = ""
		}
	}
	return n, e
}
func (a *App) SaveNode(ctx context.Context, n state.Node) (state.Node, error) {
	if len(strings.TrimSpace(n.Name)) == 0 || len(n.Name) > 80 {
		return n, errors.New("node name must contain 1–80 characters")
	}
	if _, e := nodeparser.Parse(n.URL); e != nil {
		return n, e
	}
	unlock, e := a.Store.Lock(ctx)
	if e != nil {
		return n, e
	}
	defer unlock()
	if n.ID == "" {
		n.ID = state.ID()
	} else {
		var old state.Node
		if e = a.Store.Get(ctx, "node", n.ID, &old); e != nil {
			return n, e
		}
		var active string
		_ = a.Store.Get(ctx, "system", "active-node", &active)
		if active == n.ID {
			return n, errors.New("switch away from an active node before editing it")
		}
	}
	e = a.Store.Put(ctx, "node", n.ID, n)
	n.URL = ""
	return n, e
}
func (a *App) DeleteNode(ctx context.Context, id string) error {
	unlock, e := a.Store.Lock(ctx)
	if e != nil {
		return e
	}
	defer unlock()
	var active string
	_ = a.Store.Get(ctx, "system", "active-node", &active)
	if active == id {
		return errors.New("switch away from this node before deleting")
	}
	return a.Store.Delete(ctx, "node", id)
}
func (a *App) ApplyNode(ctx context.Context, id string) (state.Operation, error) {
	return a.Run(ctx, "apply-node", id, func(ctx context.Context) error {
		proxyURL := "direct"
		if id != "direct" {
			var n state.Node
			if e := a.Store.Get(ctx, "node", id, &n); e != nil {
				return e
			}
			out, e := nodeparser.Parse(n.URL)
			if e != nil {
				return e
			}
			cfg := map[string]any{"log": map[string]any{"level": "info"}, "inbounds": []any{map[string]any{"type": "mixed", "listen": "0.0.0.0", "listen_port": 1080}}, "outbounds": []any{out}, "route": map[string]any{"final": "proxy"}}
			b, e := json.MarshalIndent(cfg, "", "  ")
			if e != nil {
				return e
			}
			// Validate inside the selected image before replacing the current configuration.
			// Initial workspaces have a valid direct config for this bootstrap.
			if e = a.up(ctx, "sing-box", false); e != nil {
				return e
			}
			var check bytes.Buffer
			if e = a.Engine.Exec(ctx, a.Name("sing-box"), []string{"sing-box", "check", "-c", "stdin"}, bytes.NewReader(b), &check); e != nil {
				return errors.New("sing-box rejected candidate configuration; current node was preserved")
			}
			if e = a.Store.Write("generated/sing-box.json", b); e != nil {
				return e
			}
			s, e := a.Service(ctx, "sing-box")
			if e != nil {
				return e
			}
			s.Revision++
			if e = a.Store.Put(ctx, "service", s.ID, s); e != nil {
				return e
			}
			if e = a.up(ctx, "sing-box", false); e != nil {
				return e
			}
			proxyURL = "socks5h://sing-box:1080"
		}
		// Preserve the application's writable config, only modify our global proxy field.
		b, e := os.ReadFile(a.Store.File("cliproxy/settings/config.yaml"))
		if e != nil {
			return e
		}
		cfg := map[string]any{}
		if e = yaml.Unmarshal(b, &cfg); e != nil {
			return errors.New("CLIProxyAPI config is invalid YAML")
		}
		cfg["proxy-url"] = proxyURL
		b, e = yaml.Marshal(cfg)
		if e != nil {
			return e
		}
		if e = a.Store.Write("cliproxy/settings/config.yaml", b); e != nil {
			return e
		}
		active := id
		if id == "direct" {
			active = ""
		}
		if e = a.Store.Put(ctx, "system", "active-node", active); e != nil {
			return e
		}
		c, e := a.Service(ctx, "cliproxy")
		if e != nil {
			return e
		}
		c.Revision++
		if e = a.Store.Put(ctx, "service", c.ID, c); e != nil {
			return e
		}
		return a.up(ctx, "cliproxy", false)
	})
}
func (a *App) management(ctx context.Context, path string, payload any) (map[string]any, error) {
	c, e := a.Service(ctx, "cliproxy")
	if e != nil {
		return nil, e
	}
	if len(c.Ports) != 1 {
		return nil, errors.New("CLIProxyAPI requires one management port")
	}
	key, e := a.Store.Secret(ctx, "management-key")
	if e != nil {
		return nil, e
	}
	address := "http://" + net.JoinHostPort(c.Ports[0].Host, fmt.Sprint(c.Ports[0].Published)) + "/v0/management/" + path
	method := http.MethodGet
	var b []byte
	if payload != nil {
		method = http.MethodPost
		b, e = json.Marshal(payload)
		if e != nil {
			return nil, e
		}
	}
	req, e := http.NewRequestWithContext(ctx, method, address, bytes.NewReader(b))
	if e != nil {
		return nil, e
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	res, e := HTTPClient().Do(req)
	if e != nil {
		return nil, errors.New("CLIProxyAPI management connection failed")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("CLIProxyAPI management HTTP %d", res.StatusCode)
	}
	var result map[string]any
	e = json.NewDecoder(res.Body).Decode(&result)
	if e != nil {
		return nil, errors.New("invalid management response")
	}
	return result, nil
}

type Diagnosis struct {
	Service string    `json:"service"`
	OK      bool      `json:"ok"`
	Scope   string    `json:"scope"`
	Message string    `json:"message"`
	Time    time.Time `json:"time"`
}

func (a *App) Diagnose(ctx context.Context, id string) (Diagnosis, error) {
	result := Diagnosis{Service: id, Time: time.Now().UTC(), Scope: "application connectivity"}
	var e error
	switch id {
	case "postgres":
		e = a.PGCheck(ctx)
	case "dragonfly":
		s, err := a.Service(ctx, id)
		if err != nil {
			return result, err
		}
		if len(s.Ports) != 1 {
			return result, errors.New("Dragonfly requires one published port")
		}
		password := s.Env["DFLY_requirepass"]
		for i, arg := range s.Args {
			if arg == "--requirepass" && i+1 < len(s.Args) {
				password = s.Args[i+1]
			}
			if strings.HasPrefix(arg, "--requirepass=") {
				password = strings.TrimPrefix(arg, "--requirepass=")
			}
		}
		c := redis.NewClient(&redis.Options{Addr: net.JoinHostPort(s.Ports[0].Host, fmt.Sprint(s.Ports[0].Published)), Password: password, DialTimeout: 5 * time.Second})
		defer c.Close()
		e = c.Ping(ctx).Err()
	case "cliproxy":
		result.Scope = "CLIProxyAPI global outbound; account overrides and model inference are not tested"
		live, err := a.management(ctx, "proxy-url", nil)
		if err != nil {
			e = err
			break
		}
		probe, err := a.management(ctx, "api-call", map[string]string{"method": "GET", "url": "https://www.gstatic.com/generate_204"})
		if err != nil {
			e = err
			break
		}
		after, err := a.management(ctx, "proxy-url", nil)
		if err != nil || after["proxy-url"] != live["proxy-url"] {
			e = errors.New("proxy changed during diagnosis; retry")
			break
		}
		if probe["status_code"] != float64(204) {
			e = errors.New("upstream did not return HTTP 204")
			break
		}
		mode := "custom proxy"
		if live["proxy-url"] == "socks5h://sing-box:1080" {
			mode = "sing-box"
		} else if live["proxy-url"] == "direct" || live["proxy-url"] == "" {
			mode = "direct"
		}
		result.Message = "HTTP 204 using " + mode
	case "sing-box":
		var output bytes.Buffer
		e = a.Engine.Exec(ctx, a.Name(id), []string{"sing-box", "check", "-c", "/etc/sing-box/config.json"}, nil, &output)
		result.Scope = "configuration validity only; use CLIProxyAPI diagnosis for outbound connectivity"
	case "vmetrics", "vlogs", "new-api":
		s, err := a.Service(ctx, id)
		if err != nil {
			return result, err
		}
		if len(s.Ports) != 1 {
			return result, errors.New("expected one published port")
		}
		path := "/health"
		if id == "new-api" {
			path = "/api/status"
		}
		req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+net.JoinHostPort(s.Ports[0].Host, fmt.Sprint(s.Ports[0].Published))+path, nil)
		res, err := HTTPClient().Do(req)
		if err != nil {
			e = errors.New("service HTTP connection failed")
		} else {
			res.Body.Close()
			if res.StatusCode != 200 {
				e = fmt.Errorf("service returned HTTP %d", res.StatusCode)
			}
		}
	default:
		return result, errors.New("no application probe for this service; inspect its container and logs")
	}
	result.OK = e == nil
	if e != nil {
		result.Message = "connectivity check failed"
		if id == "cliproxy" {
			result.Message = e.Error()
		}
	} else if result.Message == "" {
		result.Message = "check succeeded"
	}
	if err := a.Store.Put(context.WithoutCancel(ctx), "diagnosis", id, result); err != nil {
		return result, err
	}
	return result, e
}
