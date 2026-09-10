package engine

import (
	"context"
	"io"

	"github.com/PluxelJS/Proxy-LLM-API/internal/state"
)

type Spec struct {
	Name     string
	Service  state.Service
	Labels   map[string]string
	Networks []string
	Aliases  []string
}
type Container struct {
	ID      string            `json:"id"`
	State   string            `json:"state"`
	Running bool              `json:"running"`
	Labels  map[string]string `json:"labels"`
}
type Engine interface {
	Ping(context.Context) error
	Inspect(context.Context, string) (*Container, error)
	HasImage(context.Context, string) (bool, error)
	Pull(context.Context, string) error
	Create(context.Context, Spec) error
	Start(context.Context, string) error
	Stop(context.Context, string) error
	Remove(context.Context, string) error
	Network(context.Context, string, bool) error
	Logs(context.Context, string, io.Writer) error
	Exec(context.Context, string, []string, io.Reader, io.Writer) error
	Close() error
}
