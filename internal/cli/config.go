package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/PluxelJS/Proxy-LLM-API/internal/app"
	"github.com/PluxelJS/Proxy-LLM-API/internal/state"
)

type Config struct {
	Export   Export         `cmd:""`
	Validate ConfigValidate `cmd:""`
	Save     ConfigSave     `cmd:""`
	Apply    ConfigApply    `cmd:""`
}
type Export struct{ Reveal bool }

func (c *Export) Run(ctx context.Context, r *Runtime) error {
	a, e := r.App(ctx)
	if e != nil {
		return e
	}
	b, e := a.Export(ctx)
	if e != nil {
		return e
	}
	if !c.Reveal {
		b.Redacted = true
		for i := range b.Services {
			b.Services[i] = app.Redact(b.Services[i])
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(b)
}

type ConfigValidate struct {
	File string `arg:""`
}

func (c *ConfigValidate) Run(r *Runtime) error {
	var raw json.RawMessage
	if e := decode(c.File, &raw); e != nil {
		return e
	}
	var h struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if e := json.Unmarshal(raw, &h); e != nil {
		return e
	}
	var services []state.Service
	if h.SchemaVersion != 0 {
		var b app.Bundle
		if e := strictJSON(raw, &b); e != nil {
			return e
		}
		if b.SchemaVersion != 1 || b.Redacted || len(b.Services) == 0 {
			return errors.New("expected an unredacted schemaVersion 1 bundle")
		}
		services = b.Services
	} else {
		var s state.Service
		if e := strictJSON(raw, &s); e != nil {
			return e
		}
		services = []state.Service{s}
	}
	for _, s := range services {
		if e := app.Validate(s); e != nil {
			return e
		}
	}
	return r.Print(map[string]bool{"valid": true})
}

type ConfigSave struct {
	File string `arg:""`
}

func (c *ConfigSave) Run(ctx context.Context, r *Runtime) error {
	var raw json.RawMessage
	if e := decode(c.File, &raw); e != nil {
		return e
	}
	a, e := r.App(ctx)
	if e != nil {
		return e
	}
	var header struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if e = json.Unmarshal(raw, &header); e != nil {
		return e
	}
	if header.SchemaVersion > 0 {
		var b app.Bundle
		if e = strictJSON(raw, &b); e != nil {
			return e
		}
		e = a.SaveBundle(ctx, b)
	} else {
		var v state.Service
		if e = strictJSON(raw, &v); e != nil {
			return e
		}
		e = a.Save(ctx, v)
	}
	if e != nil {
		return e
	}
	return r.Print(map[string]string{"message": "configuration saved; apply selected services explicitly"})
}

type ConfigApply struct{ Target }

func (c *ConfigApply) Run(ctx context.Context, r *Runtime) error { return c.execute(ctx, r, "apply") }
