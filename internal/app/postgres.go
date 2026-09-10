package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PluxelJS/Proxy-LLM-API/internal/state"
	"github.com/jackc/pgx/v5"
)

var pgName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

func validPGName(n string) error {
	if !pgName.MatchString(n) || n == "postgres" || n == "template0" || n == "template1" || strings.HasPrefix(n, "pg_") {
		return errors.New("use a non-system PostgreSQL name: lowercase letters, digits and underscores")
	}
	return nil
}
func (a *App) PGURL(ctx context.Context, database, user, password string) (string, error) {
	s, e := a.Service(ctx, "postgres")
	if e != nil {
		return "", e
	}
	if len(s.Ports) != 1 {
		return "", errors.New("PostgreSQL management requires one published port")
	}
	u := url.URL{Scheme: "postgresql", Host: net.JoinHostPort(s.Ports[0].Host, fmt.Sprint(s.Ports[0].Published)), Path: "/" + database, User: url.UserPassword(user, password)}
	q := u.Query()
	q.Set("sslmode", "disable")
	q.Set("connect_timeout", "5")
	u.RawQuery = q.Encode()
	return u.String(), nil
}
func (a *App) pgConnect(ctx context.Context, db string) (*pgx.Conn, error) {
	s, e := a.Service(ctx, "postgres")
	if e != nil {
		return nil, e
	}
	password, e := a.Store.Secret(ctx, "postgres-admin")
	if e != nil {
		return nil, e
	}
	u, e := a.PGURL(ctx, db, s.Env["POSTGRES_USER"], password)
	if e != nil {
		return nil, e
	}
	c, e := pgx.Connect(ctx, u)
	if e != nil {
		return nil, errors.New("PostgreSQL admin connection failed; check service and credentials")
	}
	return c, nil
}

// PostgreSQL format safely quotes identifiers/literals for DDL, which does not accept value placeholders directly.
func pgDDL(ctx context.Context, c *pgx.Conn, format string, args ...any) error {
	q := "SELECT format($1::text"
	params := []any{format}
	for i, arg := range args {
		q += fmt.Sprintf(",$%d::text", i+2)
		params = append(params, arg)
	}
	q += ")"
	var statement string
	if e := c.QueryRow(ctx, q, params...).Scan(&statement); e != nil {
		return errors.New("could not prepare PostgreSQL administration statement")
	}
	if _, e := c.Exec(ctx, statement); e != nil {
		return errors.New("PostgreSQL rejected operation; check resource names, ownership and active connections")
	}
	return nil
}

type PGCreateInput struct {
	Database         string `json:"database"`
	User             string `json:"user"`
	Password         string `json:"password"`
	GeneratePassword bool   `json:"generatePassword"`
	ExistingUser     bool   `json:"existingUser"`
}

func (a *App) PGCreate(ctx context.Context, in PGCreateInput) (state.Operation, error) {
	return a.Run(ctx, "pg-create", in.Database, func(ctx context.Context) error {
		if e := validPGName(in.Database); e != nil {
			return e
		}
		if in.User == "" {
			in.User = in.Database + "_owner"
		}
		if e := validPGName(in.User); e != nil {
			return e
		}
		c, e := a.pgConnect(ctx, "postgres")
		if e != nil {
			return e
		}
		defer c.Close(ctx)
		var exists bool
		if e = c.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=$1)", in.Database).Scan(&exists); e != nil {
			return errors.New("cannot inspect databases")
		}
		if exists {
			return errors.New("database exists; use inspect instead of recreating or resetting credentials")
		}
		if e = c.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)", in.User).Scan(&exists); e != nil {
			return errors.New("cannot inspect roles")
		}
		if exists && !in.ExistingUser {
			return errors.New("role exists; explicitly select existing user")
		}
		if !exists && in.ExistingUser {
			return errors.New("selected role does not exist")
		}
		if !exists {
			if in.GeneratePassword {
				in.Password = state.ID() + state.ID()
			}
			if len(in.Password) < 8 {
				return errors.New("password must have at least 8 characters or use generate-password")
			}
			// Retain chosen credentials for recovery if a later DDL step fails.
			if e = a.Store.Put(ctx, "pg-password", in.User, in.Password); e != nil {
				return e
			}
			if e = pgDDL(ctx, c, "CREATE ROLE %I LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD %L", in.User, in.Password); e != nil {
				return e
			}
		}
		if e = pgDDL(ctx, c, "CREATE DATABASE %I OWNER %I", in.Database, in.User); e != nil {
			return e
		}
		if e = pgDDL(ctx, c, "REVOKE CONNECT, TEMPORARY ON DATABASE %I FROM PUBLIC", in.Database); e != nil {
			return e
		}
		if e = a.Store.Put(ctx, "pg-database", in.Database, state.PostgresAccount{Database: in.Database, User: in.User}); e != nil {
			return e
		}
		if in.Password != "" {
			u, _ := a.PGURL(ctx, in.Database, in.User, in.Password)
			test, e := pgx.Connect(ctx, u)
			if e != nil {
				return errors.New("database created but user connection verification failed")
			}
			defer test.Close(ctx)
			if e = test.Ping(ctx); e != nil {
				return errors.New("new database connection check failed")
			}
		}
		return nil
	})
}

type PGOverview struct {
	Databases []map[string]any `json:"databases"`
	Users     []map[string]any `json:"users"`
}

func (a *App) PGList(ctx context.Context) (PGOverview, error) {
	r := PGOverview{Databases: []map[string]any{}, Users: []map[string]any{}}
	c, e := a.pgConnect(ctx, "postgres")
	if e != nil {
		return r, e
	}
	defer c.Close(ctx)
	rows, e := c.Query(ctx, "SELECT datname,pg_get_userbyid(datdba) FROM pg_database WHERE NOT datistemplate ORDER BY datname")
	if e != nil {
		return r, errors.New("cannot list databases")
	}
	for rows.Next() {
		var name, owner string
		if e = rows.Scan(&name, &owner); e != nil {
			rows.Close()
			return r, e
		}
		var managed state.PostgresAccount
		e = a.Store.Get(ctx, "pg-database", name, &managed)
		r.Databases = append(r.Databases, map[string]any{"name": name, "owner": owner, "managed": e == nil})
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return r, e
	}
	rows, e = c.Query(ctx, "SELECT rolname,rolcanlogin,rolsuper FROM pg_roles WHERE rolname NOT LIKE 'pg_%' ORDER BY rolname")
	if e != nil {
		return r, errors.New("cannot list users")
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var login, super bool
		if e = rows.Scan(&name, &login, &super); e != nil {
			return r, e
		}
		var password string
		e = a.Store.Get(ctx, "pg-password", name, &password)
		r.Users = append(r.Users, map[string]any{"name": name, "login": login, "superuser": super, "passwordKnown": e == nil})
	}
	return r, rows.Err()
}

type PGUserInput struct {
	User             string `json:"user"`
	Password         string `json:"password"`
	GeneratePassword bool   `json:"generatePassword"`
	Database         string `json:"database"`
	Permission       string `json:"permission"`
	Confirm          bool   `json:"confirm"`
}

func (a *App) PGUser(ctx context.Context, action string, in PGUserInput) (state.Operation, error) {
	return a.Run(ctx, "pg-user-"+action, in.User, func(ctx context.Context) error {
		if e := validPGName(in.User); e != nil {
			return e
		}
		c, e := a.pgConnect(ctx, "postgres")
		if e != nil {
			return e
		}
		defer c.Close(ctx)
		switch action {
		case "create", "password":
			if in.GeneratePassword {
				in.Password = state.ID() + state.ID()
			}
			if len(in.Password) < 8 {
				return errors.New("password must have at least 8 characters")
			}
			if e = a.Store.Put(ctx, "pg-pending-password", in.User, in.Password); e != nil {
				return e
			}
			format := "ALTER ROLE %I PASSWORD %L"
			if action == "create" {
				format = "CREATE ROLE %I LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD %L"
			}
			if e = pgDDL(ctx, c, format, in.User, in.Password); e != nil {
				return e
			}
			if e = a.Store.Put(ctx, "pg-password", in.User, in.Password); e != nil {
				return e
			}
			return a.Store.Delete(ctx, "pg-pending-password", in.User)
		case "enable":
			return pgDDL(ctx, c, "ALTER ROLE %I LOGIN", in.User)
		case "disable":
			return pgDDL(ctx, c, "ALTER ROLE %I NOLOGIN", in.User)
		case "drop":
			if !in.Confirm {
				return errors.New("role deletion requires explicit confirmation; dependent objects are never cascaded")
			}
			if e = pgDDL(ctx, c, "DROP ROLE %I", in.User); e != nil {
				return e
			}
			return a.Store.Delete(ctx, "pg-password", in.User)
		case "grant":
			var managed state.PostgresAccount
			if e = a.Store.Get(ctx, "pg-database", in.Database, &managed); e != nil {
				return errors.New("permission templates apply only to managed databases")
			}
			if in.Permission != "readonly" && in.Permission != "readwrite" {
				return errors.New("permission must be readonly or readwrite")
			}
			if e = pgDDL(ctx, c, "GRANT CONNECT ON DATABASE %I TO %I", in.Database, in.User); e != nil {
				return e
			}
			db, e := a.pgConnect(ctx, in.Database)
			if e != nil {
				return e
			}
			defer db.Close(ctx)
			if e = pgDDL(ctx, db, "GRANT USAGE ON SCHEMA public TO %I", in.User); e != nil {
				return e
			}
			privileges := "SELECT"
			seq := "SELECT"
			if in.Permission == "readwrite" {
				privileges = "SELECT, INSERT, UPDATE, DELETE"
				seq = "USAGE, SELECT, UPDATE"
			}
			for _, sql := range []string{"GRANT " + privileges + " ON ALL TABLES IN SCHEMA public TO %I", "GRANT " + seq + " ON ALL SEQUENCES IN SCHEMA public TO %I"} {
				if e = pgDDL(ctx, db, sql, in.User); e != nil {
					return e
				}
			}
			if e = pgDDL(ctx, db, "ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public GRANT "+privileges+" ON TABLES TO %I", managed.User, in.User); e != nil {
				return e
			}
			return pgDDL(ctx, db, "ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public GRANT "+seq+" ON SEQUENCES TO %I", managed.User, in.User)
		default:
			return errors.New("unknown user action")
		}
	})
}
func (a *App) PGDrop(ctx context.Context, database string, confirm bool) (state.Operation, error) {
	return a.Run(ctx, "pg-drop", database, func(ctx context.Context) error {
		if !confirm {
			return errors.New("database deletion requires explicit confirmation")
		}
		if e := validPGName(database); e != nil {
			return e
		}
		c, e := a.pgConnect(ctx, "postgres")
		if e != nil {
			return e
		}
		defer c.Close(ctx)
		if e = pgDDL(ctx, c, "DROP DATABASE %I", database); e != nil {
			return e
		}
		return a.Store.Delete(ctx, "pg-database", database)
	})
}
func (a *App) PGConnection(ctx context.Context, database, user string) (map[string]string, error) {
	if user == "" {
		var rec state.PostgresAccount
		if e := a.Store.Get(ctx, "pg-database", database, &rec); e != nil {
			return nil, e
		}
		user = rec.User
	}
	var password string
	if e := a.Store.Get(ctx, "pg-password", user, &password); e != nil {
		return nil, errors.New("password unknown; PostgreSQL cannot return plaintext passwords")
	}
	u, e := a.PGURL(ctx, database, user, password)
	return map[string]string{"url": u, "database": database, "user": user, "password": password}, e
}
func (a *App) PGCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c, e := a.pgConnect(ctx, "postgres")
	if e != nil {
		return e
	}
	defer c.Close(ctx)
	return c.Ping(ctx)
}
