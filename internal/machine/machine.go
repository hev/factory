// Package machine is the box the floor is running on, in the numbers a header
// has room for.
//
// The picker has always answered "what are the agents doing" and never "what
// is this costing the machine they are doing it on". On a Mac mini running
// three lines those are the same question by mid-afternoon: a floor of green
// dots on a box at 100% is a floor that is about to start timing out, and the
// screen that shows the first should show the second.
//
// It is deliberately four numbers. A picker is not a monitor, and anything
// that wants per-core graphs wants htop.
package machine

import (
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// sampleInterval is how often the box is re-read. The floor redraws every two
// seconds, and these numbers move on a slower clock than panes do: a CPU
// figure five seconds old still answers "is this box busy", while a sampler on
// the refresh path would put three processes between a keypress and the frame
// it draws.
const sampleInterval = 5 * time.Second

// Stats is one reading of the box. Known is false until the first sample has
// landed, which is the difference between "nothing to say yet" and "idle" —
// and a header that claims 0% before it has looked is a header that lies for
// its first five seconds.
type Stats struct {
	CPU      float64 // percent of the whole machine, 0-100
	Cores    int
	Load1    float64
	MemUsed  uint64
	MemTotal uint64
	Uptime   time.Duration
	Known    bool
}

var (
	mu      sync.RWMutex
	latest  Stats
	started bool
)

// fixed is what does not change while the picker is open: the core count, the
// installed memory and the boot time. Reading them once is what makes the
// steady state two processes rather than three. Only the sampler goroutine
// touches it, so it needs no lock of its own.
var fixed struct {
	cores int
	mem   uint64
	boot  time.Time
	read  bool
}

// Start begins sampling in the background. Calling it again is a no-op: the
// picker opens a floor per chooser round trip, and a second floor should not
// mean a second sampler.
func Start() {
	mu.Lock()
	defer mu.Unlock()
	if started {
		return
	}
	started = true
	go func() {
		for {
			s := sample()
			mu.Lock()
			latest = s
			mu.Unlock()
			time.Sleep(sampleInterval)
		}
	}()
}

// Read is the last sample. It never blocks and never waits for a fresh one:
// this is a header, and one that stalls a redraw to go and run `ps` is worse
// than one that is five seconds behind.
func Read() Stats {
	mu.RLock()
	defer mu.RUnlock()
	return latest
}

func sample() Stats {
	readFixed()
	s := Stats{
		Cores:    fixed.cores,
		MemTotal: fixed.mem,
		Load1:    loadavg(),
		MemUsed:  memUsed(),
	}
	if !fixed.boot.IsZero() {
		s.Uptime = time.Since(fixed.boot)
	}
	s.CPU = cpuPercent(s.Cores)
	s.Known = s.Cores > 0
	return s
}

// readFixed reads the constants of the box, once. A failure is not retried
// forever: a machine where hw.ncpu does not answer is not a machine where the
// eleventh attempt will, and the header simply has nothing to say.
func readFixed() {
	if fixed.read {
		return
	}
	fixed.read = true
	out := sysctl("hw.ncpu", "hw.memsize", "kern.boottime")
	if len(out) < 3 {
		return
	}
	fixed.cores, _ = strconv.Atoi(strings.TrimSpace(out[0]))
	fixed.mem, _ = strconv.ParseUint(strings.TrimSpace(out[1]), 10, 64)
	fixed.boot = bootTime(out[2])
}

// bootTime reads kern.boottime, which sysctl prints as a struct and then, on
// macOS, as a human date on the same line:
//
//	{ sec = 1788089519, usec = 829380 } Sun Aug 30 05:31:59 2026
//
// The seconds are taken out of the struct rather than off the date, because
// the date is localised and the struct is not.
func bootTime(line string) time.Time {
	_, rest, ok := strings.Cut(line, "sec = ")
	if !ok {
		return time.Time{}
	}
	digits := rest
	for i, r := range rest {
		if r < '0' || r > '9' {
			digits = rest[:i]
			break
		}
	}
	secs, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || secs <= 0 {
		return time.Time{}
	}
	return time.Unix(secs, 0)
}

// loadavg is the one-minute load, which sysctl prints as `{ 3.40 3.41 3.83 }`.
// It is kept alongside the CPU percentage rather than instead of it: the
// percentage says how much of the machine is being used right now, and the
// load says how many things are queued for it, which is the number that goes
// through the roof when a factory dispatches four workers at once.
func loadavg() float64 {
	out := sysctl("vm.loadavg")
	if len(out) == 0 {
		return 0
	}
	fields := strings.Fields(strings.Trim(strings.TrimSpace(out[0]), "{}"))
	if len(fields) == 0 {
		return 0
	}
	load, _ := strconv.ParseFloat(fields[0], 64)
	return load
}

// cpuPercent sums every process's share and divides by the core count, so 100
// means the whole box rather than one core of it — the reading somebody
// glancing at a header expects, and not the one `top` prints.
//
// ps's %cpu is a decaying average over the last minute of real time, which is
// the right smoothing for a number that is redrawn every five seconds: an
// instantaneous sample would flicker between 4 and 90 while a build ran.
func cpuPercent(cores int) float64 {
	if cores <= 0 {
		return 0
	}
	var total float64
	for _, line := range run("ps", "-A", "-o", "%cpu=") {
		if v, err := strconv.ParseFloat(strings.TrimSpace(line), 64); err == nil {
			total += v
		}
	}
	pct := total / float64(cores)
	if pct > 100 {
		pct = 100 // rounding across a few hundred processes can overshoot
	}
	return pct
}

// memUsed is what macOS itself calls memory used: what is resident and cannot
// simply be handed to somebody else — active pages, wired pages, and the pages
// the compressor is holding.
//
// Inactive pages are deliberately *not* counted. They are the largest number
// in vm_stat on a machine that has been up a week, they are reclaimable on
// demand, and counting them is how you get a header that reports 94% on an
// idle box and teaches everybody to ignore it.
func memUsed() uint64 { return parseVMStat(run("vm_stat")) }

// parseVMStat is the reading itself, kept apart from the process that produces
// it so the arithmetic can be tested against a real vm_stat dump.
func parseVMStat(lines []string) uint64 {
	if len(lines) == 0 {
		return 0
	}
	pageSize := uint64(4096)
	if _, rest, ok := strings.Cut(lines[0], "page size of "); ok {
		if n, err := strconv.ParseUint(strings.Fields(rest)[0], 10, 64); err == nil && n > 0 {
			pageSize = n
		}
	}

	var pages uint64
	for _, line := range lines[1:] {
		label, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(label) {
		case "Pages active", "Pages wired down", "Pages occupied by compressor":
			n, err := strconv.ParseUint(strings.Trim(strings.TrimSpace(value), "."), 10, 64)
			if err == nil {
				pages += n
			}
		}
	}
	return pages * pageSize
}

func sysctl(names ...string) []string {
	return run("sysctl", append([]string{"-n"}, names...)...)
}

func run(name string, args ...string) []string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimRight(string(out), "\n"), "\n")
}
