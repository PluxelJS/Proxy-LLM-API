package cli

import (
	"context"

	"github.com/PluxelJS/Proxy-LLM-API/internal/app"
)

type Services struct {
	List    Status  `cmd:""`
	Inspect Inspect `cmd:""`
	Start   Start   `cmd:""`
	Stop    Stop    `cmd:""`
	Restart Restart `cmd:""`
	Enable  Enable  `cmd:""`
	Disable Disable `cmd:""`
	Pull    Pull    `cmd:""`
	Plan    Plan    `cmd:""`
	Args    Args    `cmd:""`
}
type Target struct {
	Service  string `arg:"" completion-predictor:"service"`
	Revision int64  `help:"Reject changes to a different revision"`
}

func (t *Target) execute(ctx context.Context, r *Runtime, action string) error {
	a, e := r.App(ctx)
	if e != nil {
		return e
	}
	v, e := a.Lifecycle(ctx, action, t.Service, t.Revision)
	if err := r.Print(v); err != nil {
		return err
	}
	return e
}

type Start struct{ Target }

func (c *Start) Run(ctx context.Context, r *Runtime) error { return c.execute(ctx, r, "start") }

type Stop struct{ Target }

func (c *Stop) Run(ctx context.Context, r *Runtime) error { return c.execute(ctx, r, "stop") }

type Restart struct{ Target }

func (c *Restart) Run(ctx context.Context, r *Runtime) error { return c.execute(ctx, r, "restart") }

type Enable struct{ Target }

func (c *Enable) Run(ctx context.Context, r *Runtime) error { return c.execute(ctx, r, "enable") }

type Disable struct{ Target }

func (c *Disable) Run(ctx context.Context, r *Runtime) error { return c.execute(ctx, r, "disable") }

type Pull struct{ Target }

func (c *Pull) Run(ctx context.Context, r *Runtime) error { return c.execute(ctx, r, "pull") }

type Plan struct{ Target }

func (c *Plan) Run(ctx context.Context, r *Runtime) error {
	a, e := r.App(ctx)
	if e != nil {
		return e
	}
	v, e := a.Plan(ctx, c.Service)
	if e != nil {
		return e
	}
	return r.Print(v)
}

type Inspect struct {
	Target
	Reveal bool
}

func (c *Inspect) Run(ctx context.Context, r *Runtime) error {
	a, e := r.App(ctx)
	if e != nil {
		return e
	}
	v, e := a.Service(ctx, c.Service)
	if e != nil {
		return e
	}
	if !c.Reveal {
		v = app.Redact(v)
	}
	return r.Print(v)
}

type Args struct {
	Get Inspect `cmd:""`
	Set ArgsSet `cmd:""`
}
type ArgsSet struct {
	Service  string `arg:"" completion-predictor:"service"`
	File     string `required:"" help:"JSON argv array file, or - for stdin"`
	Revision int64  `required:""`
}

func (c *ArgsSet) Run(ctx context.Context, r *Runtime) error {
	var args []string
	if e := decode(c.File, &args); e != nil {
		return e
	}
	a, e := r.App(ctx)
	if e != nil {
		return e
	}
	s, e := a.Service(ctx, c.Service)
	if e != nil {
		return e
	}
	s.Args = args
	s.Revision = c.Revision
	if e = a.Save(ctx, s); e != nil {
		return e
	}
	return r.Print(map[string]string{"message": "saved; apply the service to activate"})
}
