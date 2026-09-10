package cli

import (
	"context"
	"errors"

	"github.com/PluxelJS/Proxy-LLM-API/internal/app"
)

type PG struct {
	Backup     PGBackup     `cmd:""`
	Restore    PGRestore    `cmd:""`
	Database   PGDatabase   `cmd:""`
	User       PGUser       `cmd:""`
	Connection PGConnection `cmd:""`
}
type PGDatabase struct {
	List   PGList   `cmd:""`
	Create PGCreate `cmd:""`
	Drop   PGDrop   `cmd:""`
}
type PGList struct{}

func (c *PGList) Run(ctx context.Context, r *Runtime) error {
	a, e := r.App(ctx)
	if e != nil {
		return e
	}
	v, e := a.PGList(ctx)
	if e != nil {
		return e
	}
	return r.Print(v)
}

type PGCreate struct {
	Database         string `arg:""`
	User             string
	ExistingUser     bool
	GeneratePassword bool
	Stdin            bool
}

func (c *PGCreate) Run(ctx context.Context, r *Runtime) error {
	p := ""
	var e error
	if !c.ExistingUser {
		p, e = password(c.Stdin, c.GeneratePassword)
		if e != nil {
			return e
		}
	}
	a, e := r.App(ctx)
	if e != nil {
		return e
	}
	v, e := a.PGCreate(ctx, app.PGCreateInput{Database: c.Database, User: c.User, Password: p, GeneratePassword: c.GeneratePassword, ExistingUser: c.ExistingUser})
	if err := r.Print(v); err != nil {
		return err
	}
	return e
}

type PGDrop struct {
	Database string `arg:""`
	Confirm  bool
}

func (c *PGDrop) Run(ctx context.Context, r *Runtime) error {
	a, e := r.App(ctx)
	if e != nil {
		return e
	}
	v, e := a.PGDrop(ctx, c.Database, c.Confirm)
	if err := r.Print(v); err != nil {
		return err
	}
	return e
}

type PGUser struct {
	List        PGList        `cmd:""`
	Create      PGUserCreate  `cmd:""`
	SetPassword PGPassword    `cmd:""`
	Grant       PGGrant       `cmd:""`
	Drop        PGUserDrop    `cmd:""`
	Enable      PGUserEnable  `cmd:""`
	Disable     PGUserDisable `cmd:""`
}
type PGUserInput struct {
	User             string `arg:""`
	GeneratePassword bool
	Stdin            bool
}

func (c *PGUserInput) execute(ctx context.Context, r *Runtime, action string) error {
	p, e := password(c.Stdin, c.GeneratePassword)
	if e != nil {
		return e
	}
	a, e := r.App(ctx)
	if e != nil {
		return e
	}
	v, e := a.PGUser(ctx, action, app.PGUserInput{User: c.User, Password: p, GeneratePassword: c.GeneratePassword})
	if err := r.Print(v); err != nil {
		return err
	}
	return e
}

type PGUserCreate struct{ PGUserInput }

func (c *PGUserCreate) Run(ctx context.Context, r *Runtime) error { return c.execute(ctx, r, "create") }

type PGPassword struct{ PGUserInput }

func (c *PGPassword) Run(ctx context.Context, r *Runtime) error { return c.execute(ctx, r, "password") }

type PGGrant struct {
	User       string `arg:""`
	Database   string `required:""`
	Permission string `enum:"readonly,readwrite" required:""`
}

func (c *PGGrant) Run(ctx context.Context, r *Runtime) error {
	a, e := r.App(ctx)
	if e != nil {
		return e
	}
	v, e := a.PGUser(ctx, "grant", app.PGUserInput{User: c.User, Database: c.Database, Permission: c.Permission})
	if err := r.Print(v); err != nil {
		return err
	}
	return e
}

type PGUserDrop struct {
	User    string `arg:""`
	Confirm bool
}

func (c *PGUserDrop) Run(ctx context.Context, r *Runtime) error {
	return userAction(ctx, r, "drop", c.User, c.Confirm)
}

type PGUserEnable struct {
	User string `arg:""`
}

func (c *PGUserEnable) Run(ctx context.Context, r *Runtime) error {
	return userAction(ctx, r, "enable", c.User, false)
}

type PGUserDisable struct {
	User string `arg:""`
}

func (c *PGUserDisable) Run(ctx context.Context, r *Runtime) error {
	return userAction(ctx, r, "disable", c.User, false)
}
func userAction(ctx context.Context, r *Runtime, action, user string, confirm bool) error {
	a, e := r.App(ctx)
	if e != nil {
		return e
	}
	v, e := a.PGUser(ctx, action, app.PGUserInput{User: user, Confirm: confirm})
	if err := r.Print(v); err != nil {
		return err
	}
	return e
}

type PGConnection struct {
	Database string `arg:""`
	User     string
	Reveal   bool
}

func (c *PGConnection) Run(ctx context.Context, r *Runtime) error {
	if !c.Reveal {
		return errors.New("connection credentials require --reveal")
	}
	a, e := r.App(ctx)
	if e != nil {
		return e
	}
	v, e := a.PGConnection(ctx, c.Database, c.User)
	if e != nil {
		return e
	}
	return r.Print(v)
}

type PGBackup struct {
	Database string `arg:""`
	Name     string
}

func (c *PGBackup) Run(ctx context.Context, r *Runtime) error {
	a, e := r.App(ctx)
	if e != nil {
		return e
	}
	v, e := a.PGBackup(ctx, app.BackupInput{Database: c.Database, Name: c.Name})
	if err := r.Print(v); err != nil {
		return err
	}
	return e
}

type PGRestore struct {
	Name   string `arg:""`
	Target string `required:""`
}

func (c *PGRestore) Run(ctx context.Context, r *Runtime) error {
	a, e := r.App(ctx)
	if e != nil {
		return e
	}
	v, e := a.PGRestore(ctx, app.BackupInput{Name: c.Name, Target: c.Target})
	if err := r.Print(v); err != nil {
		return err
	}
	return e
}
