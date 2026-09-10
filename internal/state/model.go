package state

import "time"

type Port struct {
	Host      string `json:"host"`
	Published uint16 `json:"published"`
	Target    uint16 `json:"target"`
}
type Mount struct {
	Kind     string `json:"kind"`
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"readOnly"`
}
type Service struct {
	Memlock  *int64            `json:"memlock,omitempty"`
	ID       string            `json:"id"`
	Revision int64             `json:"revision"`
	Enabled  bool              `json:"enabled"`
	Image    string            `json:"image"`
	Args     []string          `json:"args"`
	Env      map[string]string `json:"env"`
	Ports    []Port            `json:"ports"`
	Mounts   []Mount           `json:"mounts"`
	Memory   int64             `json:"memory"`
	CPUs     float64           `json:"cpus"`
	User     string            `json:"user"`
	Restart  string            `json:"restart"`
}
type Node struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}
type Operation struct {
	ID       string     `json:"id"`
	Kind     string     `json:"kind"`
	Resource string     `json:"resource"`
	Status   string     `json:"status"`
	Stage    string     `json:"stage"`
	Error    string     `json:"error,omitempty"`
	Started  time.Time  `json:"started"`
	Finished *time.Time `json:"finished,omitempty"`
}
type PostgresAccount struct {
	Database string `json:"database"`
	User     string `json:"user"`
	Password string `json:"password,omitempty"`
}
