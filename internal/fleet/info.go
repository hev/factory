package fleet

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Version is the build's version, set by main.
var Version = "dev"

// Info is a host as placement sees it: who it acts as, what it has free,
// and how much of each subscription's week is left.
type Info struct {
	Name       string            `json:"name"`
	Version    string            `json:"version"`
	Login      string            `json:"login"`
	GitAuthor  string            `json:"git_author"`
	Cores      int               `json:"cores"`
	Load       float64           `json:"load"`
	MemFreePct int               `json:"mem_free_pct"` // -1 when unknown
	Live       int               `json:"live"`         // running or interactive sessions
	Harnesses  []string          `json:"harnesses"`
	Usage      map[string]*Usage `json:"usage,omitempty"`
}

// Usage is one subscription's weekly window.
type Usage struct {
	UsedPct  float64   `json:"used_pct"`
	ResetsAt time.Time `json:"resets_at"`
}

// HostInfo reads this host.
func HostInfo() Info {
	in := Info{Name: hostname(), Version: Version, Cores: runtime.NumCPU(), MemFreePct: -1, Usage: map[string]*Usage{}}
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); in.Login = ghLogin() }()
	go func() { defer wg.Done(); in.Load, in.MemFreePct = loadAndMemory() }()
	go func() {
		defer wg.Done()
		if u := claudeUsage(); u != nil {
			in.Usage["claude"] = u
		}
		if u := codexUsage(); u != nil {
			in.Usage["codex"] = u
		}
	}()
	if out, err := exec.Command("git", "config", "--global", "user.name").Output(); err == nil {
		in.GitAuthor = strings.TrimSpace(string(out))
	}
	for _, h := range Harnesses {
		if _, err := exec.LookPath(h); err == nil {
			in.Harnesses = append(in.Harnesses, h)
		}
	}
	if sessions, err := List(false); err == nil {
		for _, s := range sessions {
			if s.Status == Running || s.Status == Interactive {
				in.Live++
			}
		}
	}
	wg.Wait()
	return in
}

func loadAndMemory() (float64, int) {
	if runtime.GOOS == "linux" {
		var load float64
		if data, err := os.ReadFile("/proc/loadavg"); err == nil {
			load, _ = strconv.ParseFloat(strings.Fields(string(data))[0], 64)
		}
		free := -1
		if data, err := os.ReadFile("/proc/meminfo"); err == nil {
			var total, avail float64
			for _, line := range strings.Split(string(data), "\n") {
				f := strings.Fields(line)
				if len(f) < 2 {
					continue
				}
				v, _ := strconv.ParseFloat(f[1], 64)
				switch f[0] {
				case "MemTotal:":
					total = v
				case "MemAvailable:":
					avail = v
				}
			}
			if total > 0 {
				free = int(100 * avail / total)
			}
		}
		return load, free
	}
	var load float64
	if out, err := exec.Command("sysctl", "-n", "vm.loadavg").Output(); err == nil {
		f := strings.Fields(strings.Trim(strings.TrimSpace(string(out)), "{}"))
		if len(f) > 0 {
			load, _ = strconv.ParseFloat(f[0], 64)
		}
	}
	free := -1
	if out, err := exec.Command("memory_pressure").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, "free percentage:") {
				v := strings.TrimSuffix(strings.TrimSpace(line[strings.LastIndex(line, ":")+1:]), "%")
				free, _ = strconv.Atoi(v)
			}
		}
	}
	return load, free
}

// claudeUsage asks Anthropic for the subscription's weekly utilization. The
// endpoint rate-limits hard, so an answer is kept for ten minutes and a
// refusal falls back to the last answer.
func claudeUsage() *Usage {
	cache := filepath.Join(Home(), "usage-claude.json")
	var cached struct {
		At    time.Time `json:"at"`
		Usage Usage     `json:"usage"`
	}
	haveCache := readJSON(cache, &cached) == nil
	if haveCache && time.Since(cached.At) < 10*time.Minute {
		return &cached.Usage
	}
	stale := func() *Usage {
		if haveCache && time.Since(cached.At) < 6*time.Hour {
			return &cached.Usage
		}
		return nil
	}
	token := claudeToken()
	if token == "" {
		return stale()
	}
	req, _ := http.NewRequest("GET", "https://api.anthropic.com/api/oauth/usage", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	resp, err := (&http.Client{Timeout: 6 * time.Second}).Do(req)
	if err != nil {
		return stale()
	}
	defer resp.Body.Close()
	var body struct {
		SevenDay *struct {
			Utilization float64   `json:"utilization"`
			ResetsAt    time.Time `json:"resets_at"`
		} `json:"seven_day"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&body) != nil || body.SevenDay == nil {
		return stale()
	}
	u := Usage{UsedPct: body.SevenDay.Utilization, ResetsAt: body.SevenDay.ResetsAt}
	cached.At, cached.Usage = time.Now(), u
	writeJSON(cache, cached)
	return &u
}

// claudeToken finds Claude Code's OAuth token where each machine keeps it:
// the environment on a laptop that exports it, the login keychain on a Mac,
// the credentials file elsewhere.
func claudeToken() string {
	if t := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN"); t != "" {
		return t
	}
	var creds struct {
		ClaudeAiOauth struct {
			AccessToken string `json:"accessToken"`
		} `json:"claudeAiOauth"`
	}
	if out, err := exec.Command("security", "find-generic-password", "-s", "Claude Code-credentials", "-w").Output(); err == nil {
		if json.Unmarshal(out, &creds) == nil && creds.ClaudeAiOauth.AccessToken != "" {
			return creds.ClaudeAiOauth.AccessToken
		}
	}
	home, _ := os.UserHomeDir()
	if readJSON(filepath.Join(home, ".claude", ".credentials.json"), &creds) == nil {
		return creds.ClaudeAiOauth.AccessToken
	}
	return ""
}

// codexUsage reads the weekly window from the newest rollout: every
// token_count event carries it, so no call is needed.
func codexUsage() *Usage {
	home, _ := os.UserHomeDir()
	files, _ := filepath.Glob(filepath.Join(home, ".codex", "sessions", "*", "*", "*", "*.jsonl"))
	if len(files) == 0 {
		return nil
	}
	sort.Slice(files, func(i, j int) bool { return modTime(files[i]).After(modTime(files[j])) })
	for _, path := range files[:min(3, len(files))] {
		if u := lastRateLimit(path); u != nil {
			if time.Now().After(u.ResetsAt) {
				u.UsedPct = 0
			}
			return u
		}
	}
	return nil
}

func modTime(path string) time.Time {
	st, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return st.ModTime()
}

func lastRateLimit(path string) *Usage {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var last *Usage
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if !strings.Contains(string(line), `"rate_limits"`) {
			continue
		}
		var e struct {
			Payload struct {
				RateLimits *struct {
					Primary *struct {
						UsedPercent float64 `json:"used_percent"`
						Window      int     `json:"window_minutes"`
						ResetsAt    int64   `json:"resets_at"`
					} `json:"primary"`
				} `json:"rate_limits"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &e) != nil || e.Payload.RateLimits == nil || e.Payload.RateLimits.Primary == nil {
			continue
		}
		p := e.Payload.RateLimits.Primary
		last = &Usage{UsedPct: p.UsedPercent, ResetsAt: time.Unix(p.ResetsAt, 0)}
	}
	return last
}
