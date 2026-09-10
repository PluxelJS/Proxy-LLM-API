package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/PluxelJS/Proxy-LLM-API/internal/state"
)

type BackupInput struct {
	Database string `json:"database"`
	Name     string `json:"name"`
	Target   string `json:"target,omitempty"`
}

func (a *App) PGBackup(ctx context.Context, in BackupInput) (state.Operation, error) {
	return a.Run(ctx, "pg-backup", in.Database, func(ctx context.Context) error {
		if e := validPGName(in.Database); e != nil {
			return e
		}
		if in.Name == "" {
			in.Name = in.Database + "-" + time.Now().UTC().Format("20060102T150405Z")
		}
		if e := validBackupName(in.Name); e != nil {
			return e
		}
		dir := a.Store.File("backups")
		if e := os.MkdirAll(dir, 0700); e != nil {
			return e
		}
		dst := filepath.Join(dir, in.Name+".dump")
		if _, e := os.Stat(dst); e == nil {
			return errors.New("backup name already exists")
		}
		f, e := os.CreateTemp(dir, ".backup-*")
		if e != nil {
			return e
		}
		defer os.Remove(f.Name())
		defer f.Close()
		s, e := a.Service(ctx, "postgres")
		if e != nil {
			return e
		}
		if e = a.Engine.Exec(ctx, a.Name("postgres"), []string{"pg_dump", "-U", s.Env["POSTGRES_USER"], "-d", in.Database, "-Fc"}, nil, f); e != nil {
			return errors.New("pg_dump failed; incomplete output was discarded")
		}
		if e = f.Sync(); e != nil {
			return e
		}
		if e = f.Close(); e != nil {
			return e
		}
		if e = os.Rename(f.Name(), dst); e != nil {
			return e
		}
		meta := map[string]any{"name": in.Name, "database": in.Database, "image": s.Image, "created": time.Now().UTC(), "format": "pg_dump-custom"}
		return a.Store.Put(ctx, "backup", in.Name, meta)
	})
}
func validBackupName(name string) error {
	if name == "" || len(name) > 100 || filepath.Base(name) != name || name == "." || name == ".." {
		return errors.New("invalid backup name")
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return errors.New("backup name permits letters, digits, hyphen and underscore")
		}
	}
	return nil
}
func (a *App) PGRestore(ctx context.Context, in BackupInput) (state.Operation, error) {
	return a.Run(ctx, "pg-restore", in.Target, func(ctx context.Context) error {
		if e := validBackupName(in.Name); e != nil {
			return e
		}
		if e := validPGName(in.Target); e != nil {
			return e
		}
		var meta map[string]any
		if e := a.Store.Get(ctx, "backup", in.Name, &meta); e != nil {
			return e
		}
		f, e := os.Open(a.Store.File("backups/" + in.Name + ".dump"))
		if e != nil {
			return e
		}
		defer f.Close()
		c, e := a.pgConnect(ctx, "postgres")
		if e != nil {
			return e
		}
		defer c.Close(ctx)
		var exists bool
		if e = c.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=$1)", in.Target).Scan(&exists); e != nil {
			return errors.New("cannot inspect restore target")
		}
		if exists {
			return errors.New("restore requires a new database; existing data will not be overwritten")
		}
		if e = pgDDL(ctx, c, "CREATE DATABASE %I", in.Target); e != nil {
			return e
		}
		s, e := a.Service(ctx, "postgres")
		if e != nil {
			return e
		}
		if e = a.Engine.Exec(ctx, a.Name("postgres"), []string{"pg_restore", "-U", s.Env["POSTGRES_USER"], "--no-owner", "--no-privileges", "--exit-on-error", "--single-transaction", "-d", in.Target}, f, io.Discard); e != nil {
			return fmt.Errorf("restore failed; new target %s remains for inspection", in.Target)
		}
		return nil
	})
}

type Bundle struct {
	Redacted      bool            `json:"redacted,omitempty"`
	SchemaVersion int             `json:"schemaVersion"`
	Services      []state.Service `json:"services"`
}

func (a *App) Export(ctx context.Context) (Bundle, error) {
	all, e := a.Services(ctx)
	return Bundle{SchemaVersion: 1, Services: all}, e
}
func (a *App) SaveBundle(ctx context.Context, b Bundle) error {
	if b.Redacted {
		return errors.New("redacted exports cannot be imported; use config export --reveal")
	}
	if b.SchemaVersion != 1 || len(b.Services) == 0 {
		return errors.New("expected schemaVersion 1 and a nonempty services list")
	}
	unlock, e := a.Store.Lock(ctx)
	if e != nil {
		return e
	}
	defer unlock()
	seen := map[string]bool{}
	all, e := a.Services(ctx)
	if e != nil {
		return e
	}
	merged := map[string]state.Service{}
	for _, s := range all {
		merged[s.ID] = s
	}
	for _, s := range b.Services {
		if e = Validate(s); e != nil {
			return e
		}
		if seen[s.ID] {
			return errors.New("duplicate service")
		}
		seen[s.ID] = true
		old, ok := merged[s.ID]
		if !ok {
			return state.ErrNotFound
		}
		if old.Revision != s.Revision {
			return state.ErrConflict
		}
		if s.ID == "postgres" {
			for _, key := range []string{"POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_DB", "PGDATA"} {
				if old.Env[key] != s.Env[key] {
					return errors.New("PostgreSQL initialization fields cannot be changed")
				}
			}
		}
		s.Revision++
		merged[s.ID] = s
	}
	ports := map[string]string{}
	for _, s := range merged {
		for _, p := range s.Ports {
			key := fmt.Sprintf("%s:%d", p.Host, p.Published)
			if _, ok := ports[key]; ok {
				return errors.New("conflicting service ports")
			}
			ports[key] = s.ID
		}
	}
	tx, e := a.Store.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, s := range b.Services {
		raw, e := json.Marshal(merged[s.ID])
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "UPDATE resources SET document=? WHERE kind='service' AND id=?", string(raw), s.ID); e != nil {
			return e
		}
	}
	return tx.Commit()
}
