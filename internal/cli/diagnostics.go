package cli

import (
	"context"
	"os"

	"github.com/PluxelJS/Proxy-LLM-API/internal/state"
)

type Diagnose struct {
	Service string `arg:"" completion-predictor:"service"`
}

func (c *Diagnose) Run(ctx context.Context, r *Runtime) error {
	a, e := r.App(ctx)
	if e != nil {
		return e
	}
	v, e := a.Diagnose(ctx, c.Service)
	if err := r.Print(v); err != nil {
		return err
	}
	return e
}

type Jobs struct {
	List    JobList    `cmd:""`
	Inspect JobInspect `cmd:""`
}
type JobList struct{}

func (c *JobList) Run(ctx context.Context, r *Runtime) error {
	a, e := r.App(ctx)
	if e != nil {
		return e
	}
	var all []state.Operation
	if e = a.Store.List(ctx, "operation", &all); e != nil {
		return e
	}
	return r.Print(all)
}

type JobInspect struct {
	ID string `arg:""`
}

func (c *JobInspect) Run(ctx context.Context, r *Runtime) error {
	a, e := r.App(ctx)
	if e != nil {
		return e
	}
	var v state.Operation
	if e = a.Store.Get(ctx, "operation", c.ID, &v); e != nil {
		return e
	}
	return r.Print(v)
}

type Logs struct {
	Service string `arg:"" completion-predictor:"service"`
}

func (c *Logs) Run(ctx context.Context, r *Runtime) error {
	a, e := r.App(ctx)
	if e != nil {
		return e
	}
	return a.Engine.Logs(ctx, a.Name(c.Service), os.Stdout)
}
