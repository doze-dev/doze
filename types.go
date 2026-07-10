package doze

import "time"

// Instance is a snapshot of one declared service's live state — the facade's
// public mirror of the daemon's internal view (never an alias, so the internal
// wire type can evolve without breaking embedders).
type Instance struct {
	Name      string    // declared instance name
	Engine    string    // engine type ("postgres", "kafka", "s3", …)
	Version   string    // declared version
	State     string    // "reaped"/"booting"/"active"/"idle"/"disabled"
	PID       int       // backend pid, 0 when not running
	Conns     int       // live client connections
	RAM       int64     // resident bytes of the backend (+ subtree), 0 when reaped
	CPU       float64   // CPU usage percent (one core = 100), 0 when reaped
	StartedAt time.Time // when the backend booted
	IdleSince time.Time // when Conns last hit zero (drives the reap countdown)

	Endpoint string // the address a client connects to (host:port or unix path)
	Domain   string // local DNS name, e.g. "orders-pg.<stack>.doze"
	URL      string // full connection string
	EnvVar   string // conventional env var (DATABASE_URL, KAFKA_BROKERS, …)
	Resource string // directly-addressable path behind a shared front door (AWS URL/ARN)
	DataDir  string // where this instance's data is written

	Declared  bool   // present in the config
	Disabled  bool   // declared with enabled = false
	KeepAwake bool   // pinned: exempt from the idle reaper
	Tainted   bool   // last convergence failed/incomplete
	LastError string // most recent boot/convergence/crash failure
	Healthy   *bool  // latest liveness probe (supervised processes); nil = not probed
}

// Resource is one of a running service's addressable sub-resources — an S3
// bucket, an SQS queue, an SNS topic, a database — with a live status line.
type Resource struct {
	Kind   string            // "bucket" | "queue" | "topic" | "database" | …
	Name   string            // resource name
	Status string            // a short live status line
	Info   map[string]string // extra key/value detail
}

// Action is a data operation a service's engine offers on its resources (used to
// drive an admin UI, mirroring the dash's actions).
type Action struct {
	ID          string // stable action id, passed to Session.Admin
	Label       string // human label
	Kind        string // action category
	Destructive bool   // needs confirmation
	InputHint   string // what Admin's input string should contain, if anything
}

// Node is one instance in the declared topology — the static model from the
// config, available without the daemon running.
type Node struct {
	Name      string   // instance name
	Engine    string   // engine type
	Version   string   // declared version ("" for versionless engines)
	Port      int      // client-facing port (0 if none/unix)
	Enabled   bool     // false = declared but paused
	DependsOn []string // names this instance boots after
}
