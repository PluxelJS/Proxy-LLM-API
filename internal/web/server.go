package web

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/PluxelJS/Proxy-LLM-API/internal/app"
	"github.com/PluxelJS/Proxy-LLM-API/internal/state"
	"github.com/go-chi/chi/v5"
	"github.com/gofrs/flock"
)

//go:embed dist/*
var files embed.FS

type Server struct {
	App           *app.App
	Origin, Token string
	ctx           context.Context
	wg            sync.WaitGroup
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, e error) {
	code := 400
	kind := "invalid_request"
	if errors.Is(e, state.ErrConflict) {
		code = 409
		kind = "revision_conflict"
	}
	if errors.Is(e, state.ErrNotFound) {
		code = 404
		kind = "not_found"
	}
	reply(w, code, map[string]any{"error": map[string]string{"code": kind, "message": e.Error()}})
}
func decode(w http.ResponseWriter, r *http.Request, dst any) error {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	d.DisallowUnknownFields()
	if d.Decode(dst) != nil {
		return errors.New("invalid JSON request")
	}
	var more any
	if d.Decode(&more) != io.EOF {
		return errors.New("request must contain exactly one JSON value")
	}
	return nil
}
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'")
			if r.Host != strings.TrimPrefix(s.Origin, "http://") {
				reply(w, 403, map[string]string{"error": "invalid Host"})
				return
			}
			if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/session" {
				if r.Header.Get("X-Runtime-Token") != s.Token {
					reply(w, 403, map[string]string{"error": "session expired; reload page"})
					return
				}
			}
			if r.Method != "GET" && r.Method != "HEAD" {
				if r.Header.Get("Origin") != s.Origin || r.Header.Get("Content-Type") != "application/json" {
					reply(w, 403, map[string]string{"error": "invalid origin or content type"})
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	})
	r.Get("/api/session", func(w http.ResponseWriter, r *http.Request) { reply(w, 200, map[string]string{"token": s.Token}) })
	r.Get("/api/status", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		v, e := s.App.Status(ctx)
		if e != nil {
			fail(w, e)
			return
		}
		reply(w, 200, v)
	})
	r.Get("/api/services", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.App.Services(r.Context())
		if e != nil {
			fail(w, e)
			return
		}
		if r.URL.Query().Get("reveal") != "true" {
			for i := range v {
				v[i] = app.Redact(v[i])
			}
		}
		reply(w, 200, v)
	})
	r.Put("/api/services/{id}", func(w http.ResponseWriter, r *http.Request) {
		var v state.Service
		if e := decode(w, r, &v); e != nil {
			fail(w, e)
			return
		}
		if v.ID != chi.URLParam(r, "id") {
			fail(w, errors.New("service ID mismatch"))
			return
		}
		if e := s.App.Save(r.Context(), v); e != nil {
			fail(w, e)
			return
		}
		reply(w, 200, map[string]string{"message": "配置已保存，应用后生效"})
	})
	r.Get("/api/services/{id}/plan", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.App.Plan(r.Context(), chi.URLParam(r, "id"))
		if e != nil {
			fail(w, e)
			return
		}
		reply(w, 200, v)
	})
	r.Post("/api/services/{id}/{action}", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Revision int64 `json:"revision"`
		}
		if e := decode(w, r, &body); e != nil {
			fail(w, e)
			return
		}
		id, action := chi.URLParam(r, "id"), chi.URLParam(r, "action")
		switch action {
		case "start", "stop", "restart", "enable", "disable", "pull", "apply":
		default:
			fail(w, errors.New("invalid action"))
			return
		}
		s.submit(w, action, id, func(ctx context.Context) (state.Operation, error) {
			return s.App.Lifecycle(ctx, action, id, body.Revision)
		})
	})
	r.Post("/api/diagnose/{id}", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		v, e := s.App.Diagnose(ctx, chi.URLParam(r, "id"))
		if e != nil && v.Message == "" {
			fail(w, e)
			return
		}
		reply(w, 200, v)
	})

	r.Get("/api/config", func(w http.ResponseWriter, r *http.Request) {
		b, e := s.App.Export(r.Context())
		if e != nil {
			fail(w, e)
			return
		}
		reply(w, 200, b)
	})
	r.Put("/api/config", func(w http.ResponseWriter, r *http.Request) {
		var b app.Bundle
		if e := decode(w, r, &b); e != nil {
			fail(w, e)
			return
		}
		if e := s.App.SaveBundle(r.Context(), b); e != nil {
			fail(w, e)
			return
		}
		reply(w, 200, map[string]string{"message": "配置已导入；请检查计划后应用相应服务"})
	})
	r.Get("/api/backups", func(w http.ResponseWriter, r *http.Request) {
		var b []map[string]any
		if e := s.App.Store.List(r.Context(), "backup", &b); e != nil {
			fail(w, e)
			return
		}
		reply(w, 200, b)
	})
	r.Post("/api/pg/backup", func(w http.ResponseWriter, r *http.Request) {
		var in app.BackupInput
		if e := decode(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		s.submit(w, "pg-backup", in.Database, func(ctx context.Context) (state.Operation, error) { return s.App.PGBackup(ctx, in) })
	})
	r.Post("/api/pg/restore", func(w http.ResponseWriter, r *http.Request) {
		var in app.BackupInput
		if e := decode(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		s.submit(w, "pg-restore", in.Target, func(ctx context.Context) (state.Operation, error) { return s.App.PGRestore(ctx, in) })
	})
	r.Get("/api/jobs", func(w http.ResponseWriter, r *http.Request) {
		var v []state.Operation
		if e := s.App.Store.List(r.Context(), "operation", &v); e != nil {
			fail(w, e)
			return
		}
		reply(w, 200, v)
	})
	r.Get("/api/logs/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if _, e := s.App.Service(r.Context(), id); e != nil {
			fail(w, e)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		var b boundedBuffer
		e := s.App.Engine.Logs(ctx, s.App.Name(id), &b)
		if e != nil {
			fail(w, errors.New("cannot read container logs"))
			return
		}
		reply(w, 200, map[string]string{"text": b.String()})
	})
	r.Get("/api/pg", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.App.PGList(r.Context())
		if e != nil {
			fail(w, e)
			return
		}
		reply(w, 200, v)
	})
	r.Post("/api/pg/databases", func(w http.ResponseWriter, r *http.Request) {
		var in app.PGCreateInput
		if e := decode(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		s.submit(w, "pg-create", in.Database, func(ctx context.Context) (state.Operation, error) { return s.App.PGCreate(ctx, in) })
	})
	r.Post("/api/pg/databases/{id}/drop", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Confirm bool `json:"confirm"`
		}
		if e := decode(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		id := chi.URLParam(r, "id")
		s.submit(w, "pg-drop", id, func(ctx context.Context) (state.Operation, error) { return s.App.PGDrop(ctx, id, in.Confirm) })
	})
	r.Post("/api/pg/users/{action}", func(w http.ResponseWriter, r *http.Request) {
		var in app.PGUserInput
		if e := decode(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		action := chi.URLParam(r, "action")
		s.submit(w, "pg-user-"+action, in.User, func(ctx context.Context) (state.Operation, error) { return s.App.PGUser(ctx, action, in) })
	})
	r.Post("/api/pg/connection", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Database string `json:"database"`
			User     string `json:"user"`
		}
		if e := decode(w, r, &in); e != nil {
			fail(w, e)
			return
		}
		v, e := s.App.PGConnection(r.Context(), in.Database, in.User)
		if e != nil {
			fail(w, e)
			return
		}
		reply(w, 200, v)
	})
	r.Get("/api/nodes", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.App.Nodes(r.Context(), false)
		if e != nil {
			fail(w, e)
			return
		}
		var active string
		_ = s.App.Store.Get(r.Context(), "system", "active-node", &active)
		reply(w, 200, map[string]any{"nodes": v, "active": active})
	})
	r.Post("/api/nodes", func(w http.ResponseWriter, r *http.Request) {
		var n state.Node
		if e := decode(w, r, &n); e != nil {
			fail(w, e)
			return
		}
		v, e := s.App.SaveNode(r.Context(), n)
		if e != nil {
			fail(w, e)
			return
		}
		reply(w, 200, v)
	})
	r.Delete("/api/nodes/{id}", func(w http.ResponseWriter, r *http.Request) {
		if e := s.App.DeleteNode(r.Context(), chi.URLParam(r, "id")); e != nil {
			fail(w, e)
			return
		}
		reply(w, 200, map[string]bool{"deleted": true})
	})
	r.Post("/api/nodes/{id}/apply", func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		s.submit(w, "apply-node", id, func(ctx context.Context) (state.Operation, error) { return s.App.ApplyNode(ctx, id) })
	})
	r.Post("/api/management-key", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.App.Store.Secret(r.Context(), "management-key")
		if e != nil {
			fail(w, e)
			return
		}
		reply(w, 200, map[string]string{"key": v})
	})
	sub, _ := fs.Sub(files, "dist")
	r.Handle("/*", http.FileServer(http.FS(sub)))
	return r
}

type boundedBuffer struct{ strings.Builder }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len() < 1<<20 {
		if len(p) > (1<<20)-b.Len() {
			p = p[:(1<<20)-b.Len()]
		}
		_, _ = b.Builder.Write(p)
	}
	return n, nil
}
func (s *Server) submit(w http.ResponseWriter, kind, id string, fn func(context.Context) (state.Operation, error)) {
	o := state.Operation{ID: state.ID(), Kind: kind, Resource: id, Status: "queued", Stage: "waiting for workspace lock", Started: time.Now().UTC()}
	if e := s.App.Store.Record(s.ctx, o); e != nil {
		fail(w, e)
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx, cancel := context.WithTimeout(app.WithOperation(s.ctx, o.ID), 15*time.Minute)
		defer cancel()
		_, e := fn(ctx)
		if e != nil {
			var current state.Operation
			check, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if s.App.Store.Get(check, "operation", o.ID, &current) == nil && current.Status == "queued" {
				current.Status = "interrupted"
				current.Error = "operation cancelled before execution"
				_ = s.App.Store.Record(check, current)
			}
		}
	}()
	reply(w, http.StatusAccepted, o)
}
func Serve(ctx context.Context, a *app.App, listen string, autostart bool) error {
	ctx, cancelServer := context.WithCancel(ctx)
	defer cancelServer()
	host, _, e := net.SplitHostPort(listen)
	if e != nil || host != "127.0.0.1" {
		return errors.New("web listener must use 127.0.0.1")
	}
	lock := flock.New(a.Store.File("locks/server.lock"))
	ok, e := lock.TryLock()
	if e != nil {
		return e
	}
	if !ok {
		return errors.New("another web daemon owns this workspace")
	}
	defer lock.Close()
	// Holding both locks proves no previous web worker or CLI operation is active.
	recoveryCtx, recoveryCancel := context.WithTimeout(ctx, 30*time.Second)
	unlock, e := a.Store.Lock(recoveryCtx)
	if e != nil {
		recoveryCancel()
		return e
	}
	e = a.Store.Recover(recoveryCtx, true)
	unlock()
	recoveryCancel()
	if e != nil {
		return e
	}
	s := &Server{App: a, Origin: "http://" + listen, Token: state.ID() + state.ID(), ctx: ctx}
	defer s.wg.Wait()
	defer cancelServer()
	if autostart {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			if e := a.StartEnabled(ctx); e != nil {
				slog.Error("autostart failed", "error", e)
			}
		}()
	}
	srv := &http.Server{Addr: listen, Handler: s.Router(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(c); err != nil {
			_ = srv.Close()
		}
	}()
	slog.Info("dashboard ready", "url", s.Origin)
	e = srv.ListenAndServe()
	cancelServer()
	<-shutdownDone
	if e != nil && !errors.Is(e, http.ErrServerClosed) {
		return fmt.Errorf("web listener: %w", e)
	}
	return nil
}
