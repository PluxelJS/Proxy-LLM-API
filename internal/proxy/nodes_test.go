package proxy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestVLESS(t *testing.T) {
	v, e := Parse("vless://a2cec88e-afe4-43c2-a35f-4f8a4f361ba5@example.com:443?security=tls&type=ws&path=%2Fws&alpn=h2#test")
	if e != nil {
		t.Fatal(e)
	}
	if v["type"] != "vless" || v["tag"] != "proxy" || v["uuid"] != "a2cec88e-afe4-43c2-a35f-4f8a4f361ba5" {
		t.Fatal(v)
	}
	if v["transport"].(map[string]any)["path"] != "/ws" {
		t.Fatal(v)
	}
	b, _ := json.Marshal(v)
	if !strings.Contains(string(b), `"alpn":["h2"]`) {
		t.Fatal(string(b))
	}
}
func TestBadLinksDoNotLeak(t *testing.T) {
	for _, url := range []string{"vless://SECRET@example.com:bad", "vless://example.com:443", "bad://SECRET@host:443", "vless://SECRET\n"} {
		_, e := Parse(url)
		if e == nil {
			t.Fatalf("accepted %q", url)
		}
		if strings.Contains(e.Error(), "SECRET") {
			t.Fatal("secret leaked")
		}
	}
}
