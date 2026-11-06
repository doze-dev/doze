// Package hub is the app's one view of every stack on this machine. It finds
// each running daemon by its control socket, keeps one subscription open to
// it, remembers the stopped stacks from their project records, and tells the
// rest of the app when anything changes. The tray, the window and the
// notifications all read from here; nothing else talks to a daemon.
//
// It is built on control.Daemon, so a test drives it with a fakedaemon the
// same way the CLI goldens are driven.
package hub

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/daemon"
	"github.com/doze-dev/doze/internal/journal"
	"github.com/doze-dev/doze/internal/projects"
)

// Options configures a Hub. The zero value is a real hub on the user's
// doze home, polling every two seconds.
type Options struct {
	// Home is doze's folder: $DOZE_HOME, else ~/.doze.
	Home string
	// Dial returns the daemon behind a control socket. Tests give fakes.
	Dial func(socket string) control.Daemon
	// Every is how often the socket folder is looked at for stacks that
	// appeared or went away.
	Every time.Duration
	// Now is the clock, for tests.
	Now func() time.Time
}

// Conn is what the hub knows about one daemon's connection.
type Conn int

const (
	// ConnConnecting: a subscription is being opened; nothing shown is live yet.
	ConnConnecting Conn = iota
	// ConnLive: frames are arriving.
	ConnLive
	// ConnDown: the daemon stopped answering; the hub retries.
	ConnDown
)

// StackView is one stack as the app sees it. Running stacks come from their
// daemon; stopped ones from the project record doze wrote when they were up.
type StackView struct {
	// Key identifies the stack across refreshes: its state folder under home.
	Key string
	// Name is the stack's name; Dir is the folder its doze.hcl is in.
	Name, Dir string
	// IdleTimeout is the stack's idle time before a service sleeps.
	IdleTimeout time.Duration
	// Running says a daemon holds the control socket.
	Running bool
	// Conn, RetryAt and LastFrame describe the connection to a running daemon.
	Conn      Conn
	RetryAt   time.Time
	LastFrame time.Time
	// Metered says a page is watching this stack's meters, so a frame is due
	// every second and silence means something.
	Metered bool
	// Loaded is when the daemon read doze.hcl; zero from an older daemon.
	Loaded time.Time
	// Instances is the stack as last seen, in name order.
	Instances []control.InstanceView
	// Events is the journal kept, oldest first.
	Events []journal.Event
	// Series is each instance's meters over time, while a page watches stats.
	Series map[string]Series
	// Consoles are the module consoles a page is showing, by instance.
	Consoles map[string]ConsoleView
	// LastUp and LastDown are from the project record, for stopped stacks.
	LastUp, LastDown time.Time
	// LastSet is what doze up started last time, from the project record;
	// empty means everything.
	LastSet []string
	// Since is when the hub first saw this stack's daemon: "up 14m".
	Since time.Time
}

// Hub watches every stack.
type Hub struct {
	o Options

	mu        sync.Mutex
	stacks    map[string]*stack // by Key
	stopped   map[string]projects.Project
	refreshed bool // Refresh has run once; the first run always reports
	// wants counts the pages watching each topic of each key, kept here so
	// a stack that appears or is replaced later streams what is wanted.
	wants map[string]map[string]int
	// consoleWants counts the pages showing each instance's console, by
	// stack key and instance, as wants does for topics.
	consoleWants map[string]map[string]int
	onChange     []func()
	ctx          context.Context
}

// New makes a hub; Run starts it.
func New(o Options) *Hub {
	if o.Home == "" {
		o.Home = home()
	}
	if o.Dial == nil {
		o.Dial = func(sock string) control.Daemon { return control.NewClient(sock).Daemon() }
	}
	if o.Every == 0 {
		o.Every = 2 * time.Second
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Hub{o: o, stacks: map[string]*stack{}, stopped: map[string]projects.Project{}, wants: map[string]map[string]int{}, consoleWants: map[string]map[string]int{}}
}

// home is doze's folder: $DOZE_HOME, else ~/.doze.
func home() string {
	if h := os.Getenv(config.EnvHome); h != "" {
		return h
	}
	d, _ := os.UserHomeDir()
	return filepath.Join(d, ".doze")
}

// OnChange registers fn to run, on the hub's goroutine, whenever a stack
// appears, goes away, or sends a frame. It must return quickly.
func (h *Hub) OnChange(fn func()) {
	h.mu.Lock()
	h.onChange = append(h.onChange, fn)
	h.mu.Unlock()
}

func (h *Hub) changed() {
	h.mu.Lock()
	fns := append([]func(){}, h.onChange...)
	h.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// Run discovers stacks until ctx ends. It returns when ctx does.
func (h *Hub) Run(ctx context.Context) {
	h.mu.Lock()
	h.ctx = ctx
	h.mu.Unlock()
	t := time.NewTicker(h.o.Every)
	defer t.Stop()
	for {
		h.Refresh()
		select {
		case <-ctx.Done():
			h.mu.Lock()
			for _, s := range h.stacks {
				s.cancel()
			}
			h.mu.Unlock()
			return
		case <-t.C:
		}
	}
}

// Refresh looks once for stacks that appeared or went away. Run calls it on
// its interval; a verb calls it after, so the menu is right at once.
func (h *Hub) Refresh() {
	socks, _ := filepath.Glob(daemon.ControlSocketPathIn(filepath.Join(h.o.Home, "projects", "*")))
	seen := map[string]bool{}
	changed := false
	h.mu.Lock()
	ctx := h.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	for _, sock := range socks {
		key := filepath.Dir(filepath.Dir(sock))
		if _, alive, ok := DaemonPID(key); ok && !alive {
			// The socket outlived its daemon (it was killed, or the machine
			// restarted): the stack is stopped, not "not answering".
			continue
		}
		seen[key] = true
		if _, ok := h.stacks[key]; ok {
			continue
		}
		s := newStack(key, sock, h.o.Dial(sock), h.o.Now, h.changed)
		for topic, n := range h.wants[key] {
			s.want[topic] = n // what pages already watch; the stack streams it from the start
		}
		h.stacks[key] = s
		s.start(ctx)
		for inst := range h.consoleWants[key] {
			s.setConsoleWanted(inst, true) // a console a page still shows
		}
		changed = true
	}
	for key, s := range h.stacks {
		if !seen[key] {
			// The socket file is gone: the daemon exited and cleaned up. What
			// it last showed is kept as a stopped stack.
			s.cancel()
			s.stopConsoles()
			delete(h.stacks, key)
			changed = true
		}
	}
	stopped := map[string]projects.Project{}
	if list, err := projects.List(h.o.Home); err == nil {
		for _, p := range list {
			if !seen[p.Dir()] {
				stopped[p.Dir()] = p
			} else if s := h.stacks[p.Dir()]; s != nil {
				s.setRecord(p.LastSet) // what Up as last time would start
			}
		}
	}
	if !h.refreshed || !sameKeys(stopped, h.stopped) {
		changed = true
	}
	h.stopped, h.refreshed = stopped, true
	h.mu.Unlock()
	if changed {
		h.changed()
	}
}

func sameKeys(a, b map[string]projects.Project) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

// Stacks is every stack known, running ones first, each group by name.
func (h *Hub) Stacks() []StackView {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []StackView
	for _, s := range h.stacks {
		v := s.view()
		if v.Name == "" && v.Dir == "" {
			// A daemon from an older doze says neither: its state folder
			// ("ai-interview-bed9cc55") still carries the name.
			v.Name = nameFromKey(v.Key)
		}
		out = append(out, v)
	}
	for key, p := range h.stopped {
		out = append(out, StackView{
			Key: key, Name: p.Name, Dir: filepath.Dir(p.Config),
			LastUp: p.LastUp, LastDown: p.LastDown, LastSet: p.LastSet,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Running != out[j].Running {
			return out[i].Running
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// Watch says a page is showing a stack's stats or logs (control.TopicStats,
// control.TopicLogs), so the hub streams them; the returned func says it
// stopped. Nothing streams for a page nobody is looking at (design D14).
// The interest is kept by key, so a stack that is not running yet, or
// whose daemon restarts, streams it as soon as it is there.
func (h *Hub) Watch(key, topic string) (release func()) {
	h.mu.Lock()
	if h.wants[key] == nil {
		h.wants[key] = map[string]int{}
	}
	h.wants[key][topic]++
	s := h.stacks[key]
	h.mu.Unlock()
	var end func()
	if s != nil {
		end = s.watch(topic)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			h.mu.Lock()
			if h.wants[key] != nil && h.wants[key][topic] > 0 {
				h.wants[key][topic]--
			}
			h.mu.Unlock()
			if end != nil {
				end()
			}
		})
	}
}

// LogsVersion counts the lines an instance ("" for all) of a stack has ever
// added, so a page can tell new lines from a ring that only slid.
func (h *Hub) LogsVersion(key, instance string) uint64 {
	h.mu.Lock()
	s := h.stacks[key]
	h.mu.Unlock()
	if s == nil {
		return 0
	}
	return s.logsVersion(instance)
}

// Logs is what an instance of a stack printed, oldest first, as far back as
// the hub keeps; "" is every instance merged. Empty for a stopped stack.
func (h *Hub) Logs(key, instance string) []control.LogFrame {
	h.mu.Lock()
	s := h.stacks[key]
	h.mu.Unlock()
	if s == nil {
		return nil
	}
	return s.logs(instance)
}

// Do runs a verb on one running stack and waits for it. While the stack's
// daemon is not answering it refuses at once, as the dash did: dialling
// would hang and then say something worse.
func (h *Hub) Do(ctx context.Context, key string, verb control.Verb) error {
	h.mu.Lock()
	s := h.stacks[key]
	h.mu.Unlock()
	if s == nil {
		return ErrNotRunning
	}
	return s.do(ctx, verb)
}

// Home is doze's folder: where projects, and the app's own prefs, live.
func (h *Hub) Home() string { return h.o.Home }

// nameFromKey is a state folder's name without its hash: "shop-918fa699"
// is shop.
func nameFromKey(key string) string {
	key = filepath.Base(key)
	i := strings.LastIndexByte(key, '-')
	if i <= 0 || len(key)-i-1 != 8 {
		return key
	}
	for _, c := range key[i+1:] {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return key
		}
	}
	return key[:i]
}
