package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/PluxelJS/Proxy-LLM-API/internal/state"
)

type Nodes struct {
	List   NodeList   `cmd:""`
	Add    NodeAdd    `cmd:""`
	Delete NodeDelete `cmd:""`
	Apply  NodeApply  `cmd:""`
}
type NodeList struct{ Reveal bool }

func (c *NodeList) Run(ctx context.Context, r *Runtime) error {
	a, e := r.App(ctx)
	if e != nil {
		return e
	}
	n, e := a.Nodes(ctx, c.Reveal)
	if e != nil {
		return e
	}
	return r.Print(n)
}

type NodeAdd struct {
	Name  string `required:""`
	Stdin bool   `required:""`
}

func (c *NodeAdd) Run(ctx context.Context, r *Runtime) error {
	b, e := io.ReadAll(io.LimitReader(os.Stdin, 16385))
	if e != nil || len(b) > 16384 {
		return errors.New("invalid node input")
	}
	a, e := r.App(ctx)
	if e != nil {
		return e
	}
	n, e := a.SaveNode(ctx, state.Node{Name: c.Name, URL: strings.TrimSpace(string(b))})
	if e != nil {
		return e
	}
	return r.Print(n)
}

type NodeDelete struct {
	ID string `arg:"" completion-predictor:"node"`
}

func (c *NodeDelete) Run(ctx context.Context, r *Runtime) error {
	a, e := r.App(ctx)
	if e != nil {
		return e
	}
	if e = a.DeleteNode(ctx, c.ID); e != nil {
		return e
	}
	return r.Print(map[string]bool{"deleted": true})
}

type NodeApply struct {
	ID string `arg:"" help:"Node ID or direct" completion-predictor:"node"`
}

func (c *NodeApply) Run(ctx context.Context, r *Runtime) error {
	a, e := r.App(ctx)
	if e != nil {
		return e
	}
	v, e := a.ApplyNode(ctx, c.ID)
	if err := r.Print(v); err != nil {
		return err
	}
	return e
}
