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
