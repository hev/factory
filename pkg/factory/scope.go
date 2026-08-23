package factory

import (
	"strings"
	"time"
)

// Every factory session is <role>-<instance>, so a name says what it is
// before it says whose it is:
//
//	factory-acme                     reception for the acme factory
//	gaffer-acme                      its gaffer loop
//	worker-acme-search-index         a worker it dispatched
//
// ReceptionSession is the one exception: the bootstrap desk on a machine with
// no factory configured yet, which cannot be named after an instance because
// there is not one.
const ReceptionSession = "reception"

// ReceptionFor names an instance's front desk.
func ReceptionFor(instance string) string { return "factory-" + instance }

// GafferFor names an instance's gaffer loop.
func GafferFor(instance string) string { return "gaffer-" + instance }

// WorkerPrefix is what every one of an instance's worker sessions starts with.
func WorkerPrefix(instance string) string { return "worker-" + instance + "-" }

// Kind is what a session is to the factory.
type Kind int

const (
	// NotFactory is every other session on the machine — a shell, somebody
	// else's project, an editor. The picker leaves these alone.
	NotFactory Kind = iota
	// Reception is a front desk: factory-<instance>, or the bootstrap desk.
	Reception
	// Gaffer is an instance's loop: gaffer-<instance>.
	Gaffer
	// Worker is a child dispatched by a gaffer.
	Worker
)

// Membership is what the picker knows about one session.
type Membership struct {
	Kind     Kind
	Instance string
	Issue    string
	Tag      string
	Stale    bool
}

// Scope answers the only question the picker asks of a session name: is this
// the factory's, and if so, whose?
//
// A session is the factory's when it is one of three things:
//
//  1. a front desk — factory-<instance>, or the bootstrap `reception`;
//  2. a gaffer — gaffer-<instance>;
//  3. a worker — a session with a child-ledger entry, or one named
//     worker-<instance>-<slug>. Machine work is not issue-numbered: what a
//     worker is doing comes from the ledger's plan and step (queues.md).
//
// All three are keyed to instances this machine has configured, so somebody
// else's `gaffer-notmine` shell is not the factory's.
//
// Everything else on the machine is somebody's own work and stays off the
// screen. That is the boundary: the picker is the factory's front door, not a
// general session switcher.
type Scope struct {
	Instances []Instance
	Children  map[string]Child

	gaffers    map[string]string // gaffer-<instance>  -> instance name
	receptions map[string]string // factory-<instance> -> instance name
	names      []string          // configured instance names, for worker prefixes
	threshold  time.Duration
}

// NewScope loads the configured instances and the child ledger. Both are cheap
// file reads, so the picker re-runs this on every refresh and picks up a
// newly dispatched worker without being restarted.
func NewScope(root string) *Scope {
	instances := LoadInstances(root)
	scope := &Scope{
		Instances:  instances,
		Children:   LoadLedger(LedgerDir()),
		gaffers:    map[string]string{},
		receptions: map[string]string{},
		threshold:  StaleThreshold(),
	}
	for _, inst := range instances {
		scope.gaffers[GafferFor(inst.Name)] = inst.Name
		scope.receptions[ReceptionFor(inst.Name)] = inst.Name
		scope.names = append(scope.names, inst.Name)
	}
	return scope
}

// Classify decides whether a session belongs to the factory, and labels it.
func (s *Scope) Classify(session string, now time.Time) Membership {
	if session == ReceptionSession {
		return Membership{Kind: Reception}
	}

	// A ledger entry is the authoritative answer, and the only one carrying an
	// RFC tag or the stale flag.
	if child, ok := s.Children[session]; ok {
		return Membership{
			Kind:     Worker,
			Instance: child.Instance,
			Issue:    child.Issue.String(),
			Tag:      child.Tag(),
			Stale:    child.Stale(now, s.threshold),
		}
	}

	if instance, ok := s.gaffers[session]; ok {
		return Membership{Kind: Gaffer, Instance: instance}
	}
	if instance, ok := s.receptions[session]; ok {
		return Membership{Kind: Reception, Instance: instance}
	}

	// No ledger file: fall back to the dispatch naming convention, but only for
	// an instance this machine actually runs.
	for _, name := range s.names {
		if rest := strings.TrimPrefix(session, WorkerPrefix(name)); rest != session && rest != "" {
			return Membership{Kind: Worker, Instance: name}
		}
	}

	return Membership{Kind: NotFactory}
}

// Sessions filters a list of live session names down to the factory's, keeping
// the caller's order.
func (s *Scope) Sessions(names []string, now time.Time) map[string]Membership {
	out := make(map[string]Membership, len(names))
	for _, name := range names {
		if m := s.Classify(name, now); m.Kind != NotFactory {
			out[name] = m
		}
	}
	return out
}
