package proxy

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/bestnite/sub2sing-box/model"
	"github.com/bestnite/sub2sing-box/parser"
)

// Keep parser implementation upstream; normalize errors so malformed links never leak secrets.
func Parse(raw string) (out map[string]any, err error) {
	defer func() {
		if recover() != nil {
			out = nil
			err = errors.New("invalid node link")
		}
	}()
	if len(raw) > 16384 || strings.ContainsAny(raw, "\r\n\x00") {
		return nil, errors.New("invalid node link")
	}
	u, e := url.Parse(raw)
	if e != nil {
		return nil, errors.New("invalid node link")
	}
	if u.Scheme != "vmess" && u.Scheme != "ss" {
		port, e := strconv.Atoi(u.Port())
		if e != nil || port < 1 || port > 65535 || u.Hostname() == "" {
			return nil, errors.New("node requires a host and port")
		}
	}
	if u.Scheme == "vless" || u.Scheme == "trojan" {
		switch u.Query().Get("type") {
		case "", "tcp", "ws", "grpc", "http", "quic":
		default:
			return nil, errors.New("unsupported node transport")
		}
		switch u.Query().Get("security") {
		case "", "none", "tls":
		case "reality":
			if u.Query().Get("pbk") == "" {
				return nil, errors.New("Reality public key is required")
			}
		default:
			return nil, errors.New("unsupported node security")
		}
	}
	var parsed model.Outbound
	switch u.Scheme {
	case "vless":
		if u.User == nil || u.User.Username() == "" {
			return nil, errors.New("VLESS UUID required")
		}
		uuid, uuidErr := hex.DecodeString(strings.ReplaceAll(u.User.Username(), "-", ""))
		if uuidErr != nil || len(uuid) != 16 {
			return nil, errors.New("invalid VLESS UUID")
		}
		parsed, e = parser.ParseVless(raw)
	case "vmess":
		parsed, e = parser.ParseVmess(raw)
	case "trojan":
		parsed, e = parser.ParseTrojan(raw)
	case "ss":
		parsed, e = parser.ParseShadowsocks(raw)
	case "hysteria2", "hy2":
		parsed, e = parser.ParseHysteria2(strings.Replace(raw, "hy2://", "hysteria2://", 1))
	case "anytls":
		parsed, e = parser.ParseAnytls(raw)
	case "socks", "socks5", "socks5h", "http", "https":
		port, _ := strconv.Atoi(u.Port())
		kind := "http"
		if strings.HasPrefix(u.Scheme, "socks") {
			kind = "socks"
		}
		out = map[string]any{"type": kind, "tag": "proxy", "server": u.Hostname(), "server_port": port}
		if kind == "socks" {
			out["version"] = "5"
		}
		if u.User != nil {
			out["username"] = u.User.Username()
			p, _ := u.User.Password()
			out["password"] = p
		}
		if u.Scheme == "https" {
			out["tls"] = map[string]any{"enabled": true, "server_name": u.Hostname()}
		}
		return out, nil
	default:
		return nil, errors.New("unsupported node scheme")
	}
	if e != nil {
		return nil, errors.New("invalid node link or unsupported transport")
	}
	b, e := json.Marshal(&parsed)
	if e != nil {
		return nil, errors.New("cannot encode node")
	}
	if e = json.Unmarshal(b, &out); e != nil {
		return nil, e
	}
	out["tag"] = "proxy"
	if tls, ok := out["tls"].(map[string]any); ok {
		if alpn := u.Query().Get("alpn"); alpn != "" {
			tls["alpn"] = strings.Split(alpn, ",")
		}
	}
	return out, nil
}
