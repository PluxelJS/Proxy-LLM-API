package state

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS
var ErrConflict = errors.New("configuration revision conflict; reload and retry")
var ErrNotFound = errors.New("resource not found")

type Store struct {
	DB       *sql.DB
	Dir      string
	provider *goose.Provider
}

func ID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func Open(ctx context.Context, dir string) (*Store, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Join(abs, "locks"), 0700); err != nil {
		return nil, err
	}
	if err = os.Chmod(abs, 0700); err != nil {
		return nil, err
	}
	privateFile, err := os.OpenFile(filepath.Join(abs, "state.db"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	err = privateFile.Chmod(0600)
	privateFile.Close()
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(abs, "state.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, s := range []string{"PRAGMA busy_timeout=5000", "PRAGMA journal_mode=WAL", "PRAGMA foreign_keys=ON"} {
		if _, err = db.ExecContext(ctx, s); err != nil {
			db.Close()
			return nil, err
		}
	}
	migrationFS, _ := fs.Sub(migrations, "migrations")
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, migrationFS)
	if err != nil {
		db.Close()
		return nil, err
	}
	st := &Store{DB: db, Dir: abs, provider: provider}
	schemaLock := flock.New(filepath.Join(abs, "locks", "schema.lock"))
	ok, err := schemaLock.TryLockContext(ctx, 100*time.Millisecond)
	if err != nil || !ok {
		db.Close()
		return nil, errors.New("cannot acquire schema lock")
	}
	defer schemaLock.Close()
	if _, err = provider.Up(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err = os.Chmod(filepath.Join(abs, "state.db"), 0600); err != nil {
		db.Close()
		return nil, err
	}
	return st, nil
}
func (s *Store) Close() error { return s.DB.Close() }

// One workspace-wide mutation lock initially: all entry points share it, reads remain available.
func (s *Store) Lock(ctx context.Context) (func(), error) {
	f := flock.New(filepath.Join(s.Dir, "locks", "operation.lock"))
	ok, err := f.TryLockContext(ctx, 100*time.Millisecond)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("workspace is busy")
	}
	return func() { _ = f.Unlock(); _ = f.Close() }, nil
}
func (s *Store) Get(ctx context.Context, kind, id string, dst any) error {
	var raw string
	err := s.DB.QueryRowContext(ctx, "SELECT document FROM resources WHERE kind=? AND id=?", kind, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(raw), dst)
}
func (s *Store) Put(ctx context.Context, kind, id string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, "INSERT INTO resources(kind,id,document) VALUES(?,?,?) ON CONFLICT(kind,id) DO UPDATE SET document=excluded.document", kind, id, string(raw))
	return err
}
func (s *Store) Delete(ctx context.Context, kind, id string) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM resources WHERE kind=? AND id=?", kind, id)
	return err
}
func (s *Store) List(ctx context.Context, kind string, dst any) error {
	rows, err := s.DB.QueryContext(ctx, "SELECT document FROM resources WHERE kind=? ORDER BY id", kind)
	if err != nil {
		return err
	}
	defer rows.Close()
	all := []json.RawMessage{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return err
		}
		all = append(all, json.RawMessage(raw))
	}
	if err = rows.Err(); err != nil {
		return err
	}
	raw, err := json.Marshal(all)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}
func (s *Store) SaveService(ctx context.Context, v Service) error {
	old := Service{}
	if err := s.Get(ctx, "service", v.ID, &old); err != nil {
		return err
	}
	if old.Revision != v.Revision {
		return ErrConflict
	}
	v.Revision++
	return s.Put(ctx, "service", v.ID, v)
}
func (s *Store) Secret(ctx context.Context, name string) (string, error) {
	var value string
	err := s.Get(ctx, "secret", name, &value)
	return value, err
}
func (s *Store) EnsureSecret(ctx context.Context, name string) (string, error) {
	v, err := s.Secret(ctx, name)
	if err == nil {
		return v, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return "", err
	}
	v = ID() + ID()
	return v, s.Put(ctx, "secret", name, v)
}
func (s *Store) File(name string) string { return filepath.Join(s.Dir, name) }
func (s *Store) Write(name string, data []byte) error {
	dst := s.File(name)
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(dst), ".runtime-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), dst)
}
func (s *Store) Record(ctx context.Context, o Operation) error {
	return s.Put(ctx, "operation", o.ID, o)
}
func (s *Store) Recover(ctx context.Context, includeQueued ...bool) error {
	var all []Operation
	if err := s.List(ctx, "operation", &all); err != nil {
		return err
	}
	for _, o := range all {
		if o.Status == "running" || (len(includeQueued) > 0 && includeQueued[0] && o.Status == "queued") {
			o.Status = "interrupted"
			o.Error = "execution interrupted; inspect actual service state before retrying"
			now := time.Now().UTC()
			o.Finished = &now
			if err := s.Record(ctx, o); err != nil {
				return fmt.Errorf("recover operation: %w", err)
			}
		}
	}
	return nil
}
