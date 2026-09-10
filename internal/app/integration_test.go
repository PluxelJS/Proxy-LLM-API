package app

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/PluxelJS/Proxy-LLM-API/internal/state"
	"github.com/jackc/pgx/v5"
)

func freePort(t *testing.T) uint16 {
	t.Helper()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	p := uint16(l.Addr().(*net.TCPAddr).Port)
	l.Close()
	return p
}
func TestEngineIntegration(t *testing.T) {
	kind := os.Getenv("DEV_RUNTIME_INTEGRATION")
	if kind == "" {
		t.Skip("set DEV_RUNTIME_INTEGRATION=podman or docker for isolated real-container tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	dir := t.TempDir()
	endpoint := os.Getenv("DEV_RUNTIME_TEST_ENDPOINT")
	if endpoint == "" {
		if kind == "podman" {
			endpoint = "unix:///run/user/" + fmt.Sprint(os.Getuid()) + "/podman/podman.sock"
		} else {
			endpoint = "unix:///var/run/docker.sock"
		}
	}
	if e := Init(ctx, dir, kind, endpoint); e != nil {
		t.Fatal(e)
	}
	a, e := Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	ids := []string{"postgres", "dragonfly", "vmetrics", "vlogs", "new-api", "cliproxy"}
	defer func() {
		clean, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		for _, id := range append(ids, "sing-box") {
			_ = a.stop(clean, id)
			_ = a.Engine.Remove(clean, a.Name(id))
			_ = exec.CommandContext(clean, kind, "volume", "rm", a.Name(id)+"-data").Run()
		}
		_ = exec.CommandContext(clean, kind, "network", "rm", a.Name("default")).Run()
	}()
	for _, id := range ids {
		s, e := a.Service(ctx, id)
		if e != nil {
			t.Fatal(e)
		}
		s.Ports[0].Published = freePort(t)
		if e = a.Save(ctx, s); e != nil {
			t.Fatal(e)
		}
		if _, e = a.Lifecycle(ctx, "start", id, 2); e != nil {
			t.Fatalf("start %s: %v", id, e)
		}
		t.Log("started", id)
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		if a.PGCheck(ctx) == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("PostgreSQL did not become ready")
		}
		time.Sleep(time.Second)
	}
	if _, e = a.PGCreate(ctx, PGCreateInput{Database: "demo", User: "demo_owner", Password: "start-password!$@x"}); e != nil {
		t.Fatal(e)
	}
	connection, e := a.PGConnection(ctx, "demo", "")
	if e != nil {
		t.Fatal(e)
	}
	db, e := pgx.Connect(ctx, connection["url"])
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, "CREATE TABLE public.test_data (id int PRIMARY KEY, value text); INSERT INTO public.test_data VALUES(1,'persistent')"); e != nil {
		db.Close(ctx)
		t.Fatal(e)
	}
	db.Close(ctx)
	if _, e = a.PGUser(ctx, "password", PGUserInput{User: "demo_owner", Password: "new-password!$@x"}); e != nil {
		t.Fatal(e)
	}
	old, e := pgx.Connect(ctx, connection["url"])
	if e == nil {
		old.Close(ctx)
		t.Fatal("old password still authenticates")
	}
	connection, e = a.PGConnection(ctx, "demo", "")
	if e != nil {
		t.Fatal(e)
	}
	db, e = pgx.Connect(ctx, connection["url"])
	if e != nil {
		t.Fatal(e)
	}
	db.Close(ctx)
	if _, e = a.PGUser(ctx, "create", PGUserInput{User: "reader", Password: "reader-password"}); e != nil {
		t.Fatal(e)
	}
	if _, e = a.PGUser(ctx, "grant", PGUserInput{User: "reader", Database: "demo", Permission: "readonly"}); e != nil {
		t.Fatal(e)
	}
	readerURL, _ := a.PGURL(ctx, "demo", "reader", "reader-password")
	reader, e := pgx.Connect(ctx, readerURL)
	if e != nil {
		t.Fatal(e)
	}
	var value string
	if e = reader.QueryRow(ctx, "SELECT value FROM public.test_data WHERE id=1").Scan(&value); e != nil {
		reader.Close(ctx)
		t.Fatal(e)
	}
	if _, e = reader.Exec(ctx, "INSERT INTO public.test_data VALUES(2,'no')"); e == nil {
		reader.Close(ctx)
		t.Fatal("readonly role can write")
	}
	reader.Close(ctx)
	if _, e = a.PGBackup(ctx, BackupInput{Database: "demo", Name: "integration"}); e != nil {
		t.Fatal(e)
	}
	if _, e = a.PGRestore(ctx, BackupInput{Name: "integration", Target: "restored"}); e != nil {
		t.Fatal(e)
	}
	restored, e := a.pgConnect(ctx, "restored")
	if e != nil {
		t.Fatal(e)
	}
	if e = restored.QueryRow(ctx, "SELECT value FROM public.test_data WHERE id=1").Scan(&value); e != nil || value != "persistent" {
		t.Fatal("backup round trip", e)
	}
	restored.Close(ctx)
	if _, e = a.PGRestore(ctx, BackupInput{Name: "integration", Target: "demo"}); e == nil {
		t.Fatal("restore overwrote existing database")
	}
	if _, e = a.Lifecycle(ctx, "restart", "postgres", 2); e != nil {
		t.Fatal(e)
	}
	deadline = time.Now().Add(30 * time.Second)
	for {
		db, e = pgx.Connect(ctx, connection["url"])
		if e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(e)
		}
		time.Sleep(time.Second)
	}
	defer db.Close(ctx)
	if e = db.QueryRow(ctx, "SELECT value FROM public.test_data WHERE id=1").Scan(&value); e != nil || value != "persistent" {
		t.Fatal("data not preserved", e)
	}
	for _, id := range []string{"dragonfly", "vmetrics", "vlogs", "new-api"} {
		d, e := a.Diagnose(ctx, id)
		if e != nil || !d.OK {
			t.Fatalf("diagnose %s: %v", id, e)
		}
	}
	if _, e = a.management(ctx, "proxy-url", nil); e != nil {
		t.Fatal("CLIProxyAPI management", e)
	}
	// Exercise the actual global proxy selection without relying on public internet or accounts.
	probe := map[string]string{"method": "GET", "url": "http://new-api:3000/api/status"}
	response, e := a.management(ctx, "api-call", probe)
	if e != nil || response["status_code"] != float64(200) {
		t.Fatal("direct internal probe", e)
	}
	node, e := a.SaveNode(ctx, state.Node{Name: "unreachable-test", URL: "http://127.0.0.1:1"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.ApplyNode(ctx, node.ID); e != nil {
		t.Fatal("apply sing-box node", e)
	}
	deadline = time.Now().Add(20 * time.Second)
	for {
		response, e = a.management(ctx, "proxy-url", nil)
		if e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(e)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if response["proxy-url"] != "socks5h://sing-box:1080" {
		t.Fatal("node not selected by CLIProxyAPI")
	}
	response, e = a.management(ctx, "api-call", probe)
	if e == nil && response["status_code"] == float64(200) {
		t.Fatal("request bypassed deliberately unreachable proxy")
	}
	if _, e = a.ApplyNode(ctx, "direct"); e != nil {
		t.Fatal(e)
	}
	deadline = time.Now().Add(20 * time.Second)
	for {
		response, e = a.management(ctx, "api-call", probe)
		if e == nil && response["status_code"] == float64(200) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("direct probe did not recover", e)
		}
		time.Sleep(200 * time.Millisecond)
	}
	s, _ := a.Service(ctx, "dragonfly")
	s.Args = append(s.Args, "--cache_mode=true")
	if e = a.Save(ctx, s); e != nil {
		t.Fatal(e)
	}
	if _, e = a.Lifecycle(ctx, "apply", "dragonfly", s.Revision+1); e != nil {
		t.Fatal(e)
	}
	t.Log("real engine lifecycle, PostgreSQL credentials/permissions/persistence, service probes passed")
}
