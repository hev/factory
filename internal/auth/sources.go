package auth

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// ── Claude Code ──────────────────────────────────────────────

// checkClaude reads whichever of the three places Claude Code's login can live
// actually holds it, in the order Claude Code itself prefers them.
//
// The environment goes first because it wins. A static token exported from a
// shell profile silently overrides the keychain, which is a good thing to know
// while looking at a keychain entry and wondering why a session keeps logging
// itself out.
func checkClaude() Credential {
	c := Credential{Name: "claude", What: "Claude Code login"}

	if name, _ := firstEnv("ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN"); name != "" {
		c.State, c.Source = StateOK, "$"+name
		c.Detail = "static token in the environment — it overrides any login below"
		return c
	}

	path := home(".claude", ".credentials.json")
	if raw, err := os.ReadFile(path); err == nil {
		var file struct {
			OAuth struct {
				ExpiresAt int64 `json:"expiresAt"`
			} `json:"claudeAiOauth"`
		}
		if json.Unmarshal(raw, &file) == nil && file.OAuth.ExpiresAt > 0 {
			c.State, c.Source = StateOK, tilde(path)
			c.Expires = millis(file.OAuth.ExpiresAt)
			c.Refreshable = true
			c.Detail = "subscription oauth, refreshed in place"
			return c
		}
	}

	// The keychain is probed for the item, never for the password. `security
	// -w` can raise a GUI prompt that a headless or ssh session waits on
	// forever, and the expiry is inside the encrypted blob anyway, so there
	// is nothing to be gained by paying that price.
	if account, ok := keychainItem("Claude Code-credentials"); ok {
		c.State, c.Source = StateUnknown, "login keychain"
		c.Detail = "present as " + account + " — the expiry is inside the item, which is not read from here"
		return c
	}

	c.State, c.Detail = StateMissing, "run `claude` once and log in"
	return c
}

// keychainItem reports whether a generic-password item exists, and the account
// it is filed under. Metadata only: no -w, so no prompt and no hang.
func keychainItem(service string) (string, bool) {
	out, err := exec.Command("security", "find-generic-password", "-s", service).CombinedOutput()
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if _, value, ok := strings.Cut(line, `"acct"<blob>="`); ok {
			return strings.TrimSuffix(value, `"`), true
		}
	}
	return "the current user", true
}

// ── Codex ────────────────────────────────────────────────────

// checkCodex reads ~/.codex/auth.json, whose access token is a JWT and says
// when it dies. A refresh token sits beside it, so an expired one is normal
// rather than news.
func checkCodex() Credential {
	c := Credential{Name: "codex", What: "Codex login"}

	path := home(".codex", "auth.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		c.State, c.Detail = StateMissing, "run `codex login`"
		return c
	}
	c.Source = tilde(path)

	var file struct {
		AuthMode string  `json:"auth_mode"`
		APIKey   *string `json:"OPENAI_API_KEY"`
		Tokens   struct {
			Access  string `json:"access_token"`
			Refresh string `json:"refresh_token"`
		} `json:"tokens"`
	}
	if json.Unmarshal(raw, &file) != nil {
		c.State, c.Detail = StateUnknown, "the auth file is there but could not be read"
		return c
	}

	if file.APIKey != nil && *file.APIKey != "" {
		c.State, c.Detail = StateOK, "api key — no expiry recorded"
		return c
	}
	if file.Tokens.Access == "" {
		c.State, c.Detail = StateMissing, "the auth file holds no token — run `codex login`"
		return c
	}

	c.State = StateOK
	c.Refreshable = file.Tokens.Refresh != ""
	c.Expires = jwtExpiry(file.Tokens.Access)
	mode := file.AuthMode
	if mode == "" {
		mode = "oauth"
	}
	c.Detail = mode
	if c.Refreshable {
		c.Detail += " — refreshes on the next run"
	}
	return c
}

// jwtExpiry pulls `exp` out of a JWT's payload without verifying anything.
// This is a reader, not a gate: nothing here is deciding whether to trust the
// token, only reporting the date it carries.
func jwtExpiry(token string) time.Time {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return time.Time{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Exp <= 0 {
		return time.Time{}
	}
	return time.Unix(claims.Exp, 0)
}

// ── GitHub ───────────────────────────────────────────────────

// checkGH reads gh's hosts file. It records no expiry — a gh oauth token does
// not carry one, and a classic PAT's date lives on github.com rather than on
// disk — so the honest answer is "logged in as X, expiry not recorded here",
// and the live probe is what turns that into a yes or a no.
//
// This is the credential a factory misses most quietly: a worker with a dead
// gh token still runs, still commits, and simply never opens the pull request
// the whole task was for.
func checkGH() Credential {
	c := Credential{Name: "gh", What: "GitHub CLI"}

	if name, _ := firstEnv("GH_TOKEN", "GITHUB_TOKEN"); name != "" {
		c.State, c.Source = StateOK, "$"+name
		c.Detail = "token in the environment — it overrides the hosts file"
		return c
	}

	path := ghHostsPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		c.State, c.Detail = StateMissing, "run `gh auth login`"
		return c
	}
	c.Source = tilde(path)

	// A deliberately small reader rather than a YAML dependency: the two
	// facts wanted here are which host and which user, both of which are one
	// unambiguous line each in a file gh writes itself.
	host, user, token := "", "", false
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
		case !strings.HasPrefix(line, " ") && strings.HasSuffix(trimmed, ":"):
			if host == "" {
				host = strings.TrimSuffix(trimmed, ":")
			}
		case strings.HasPrefix(trimmed, "user:"):
			if user == "" {
				user = strings.TrimSpace(strings.TrimPrefix(trimmed, "user:"))
			}
		case strings.HasPrefix(trimmed, "oauth_token:"):
			token = true
		}
	}
	// gh keeps the token in the login keychain wherever there is one and
	// falls back to the hosts file where there is not, so a hosts file with a
	// user and no token is the *normal* macOS shape rather than a broken one.
	// Reading it as "not logged in" was the first thing this check got wrong.
	store := "hosts file"
	if !token {
		if _, ok := keychainItem("gh:" + host); !ok {
			c.State, c.Detail = StateMissing, "no token in the hosts file or the keychain — run `gh auth login`"
			return c
		}
		store = "login keychain"
	}

	c.State = StateUnknown
	who := host
	if user != "" {
		who = user + " on " + host
	}
	c.Detail = who + " · " + store + " — gh records no expiry; ^r asks GitHub"
	return c
}

func ghHostsPath() string {
	if dir := os.Getenv("GH_CONFIG_DIR"); dir != "" {
		return dir + "/hosts.yml"
	}
	return home(".config", "gh", "hosts.yml")
}

// ── Cloudflare ───────────────────────────────────────────────

// checkCloudflare reads wrangler's OAuth config, which does record an expiry,
// and a refresh token beside it. Wrangler has moved the file twice, so all
// three homes are tried.
func checkCloudflare() Credential {
	c := Credential{Name: "cloudflare", What: "Cloudflare (wrangler)"}

	if name, _ := firstEnv("CLOUDFLARE_API_TOKEN", "CF_API_TOKEN"); name != "" {
		c.State, c.Source = StateOK, "$"+name
		c.Detail = "api token in the environment — no expiry recorded"
		return c
	}

	path := ""
	for _, candidate := range []string{
		home("Library", "Preferences", ".wrangler", "config", "default.toml"),
		home(".config", ".wrangler", "config", "default.toml"),
		home(".wrangler", "config", "default.toml"),
	} {
		if exists(candidate) {
			path = candidate
			break
		}
	}
	if path == "" {
		c.State, c.Detail = StateMissing, "run `wrangler login`"
		return c
	}
	c.Source = tilde(path)

	var file struct {
		OAuthToken   string `toml:"oauth_token"`
		RefreshToken string `toml:"refresh_token"`
		Expiration   string `toml:"expiration_time"`
	}
	if _, err := toml.DecodeFile(path, &file); err != nil {
		c.State, c.Detail = StateUnknown, "the wrangler config could not be read"
		return c
	}
	if file.OAuthToken == "" {
		c.State, c.Detail = StateMissing, "the wrangler config holds no token — run `wrangler login`"
		return c
	}

	c.State = StateOK
	c.Refreshable = file.RefreshToken != ""
	if t, err := time.Parse(time.RFC3339, file.Expiration); err == nil {
		c.Expires = t
	}
	c.Detail = "oauth"
	if c.Refreshable {
		c.Detail += " — refreshes on the next wrangler run"
	}
	return c
}

// ── Linear (over MCP) ────────────────────────────────────────

// checkLinear reads the MCP OAuth sessions Claude Code keeps, which is where a
// factory's Linear login actually lives: the gaffer reaches Linear through an
// MCP server, so the thing that can lapse is that server's session and not
// anything in the factory's own config.
//
// Sessions are keyed by server name, which is why a machine holding two Linear
// workspaces gives each factory its own server name (contracts) — and why this
// counts them rather than assuming one.
func checkLinear() Credential {
	c := Credential{Name: "linear", What: "Linear (MCP oauth)"}

	path := home(".claude", ".credentials.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return linearFromConfig(c)
	}
	c.Source = tilde(path)

	var file struct {
		MCP map[string]struct {
			ServerName   string `json:"serverName"`
			AccessToken  string `json:"accessToken"`
			RefreshToken string `json:"refreshToken"`
			ExpiresAt    int64  `json:"expiresAt"`
		} `json:"mcpOAuth"`
	}
	if json.Unmarshal(raw, &file) != nil {
		c.State, c.Detail = StateUnknown, "the credentials file could not be read"
		return c
	}

	// A session with an empty accessToken is not a session. Claude Code writes
	// the record as soon as the oauth flow *starts* — server url, client id,
	// discovery — and fills the token in only when the browser comes back. An
	// abandoned flow therefore leaves a complete-looking entry holding nothing,
	// which read as "authorised, no expiry recorded" on the mini for as long as
	// the gaffer had been failing to reach Linear at all.
	var servers, started []string
	var soonest time.Time
	renewable := true
	for _, session := range file.MCP {
		if !strings.Contains(strings.ToLower(session.ServerName), "linear") {
			continue
		}
		if session.AccessToken == "" {
			started = append(started, session.ServerName)
			continue
		}
		servers = append(servers, session.ServerName)
		// Linear hands out a one-day access token with a refresh token beside
		// it, so the expiry is a heartbeat rather than a deadline and grade()
		// wants to know that. Refreshable only holds while *every* live
		// session carries one: a single session that cannot renew makes the
		// soonest date real news again, and this row speaks for all of them.
		if session.RefreshToken == "" {
			renewable = false
		}
		if session.ExpiresAt > 0 {
			if at := millis(session.ExpiresAt); soonest.IsZero() || at.Before(soonest) {
				soonest = at
			}
		}
	}
	if len(servers) == 0 {
		if len(started) > 0 {
			sort.Strings(started)
			c.State = StateMissing
			c.Detail = strings.Join(started, ", ") + " — the oauth flow was started and never finished; /mcp in a session completes it"
			return c
		}
		return linearFromConfig(c)
	}
	sort.Strings(servers)

	c.Expires = soonest
	c.State = StateOK
	c.Refreshable = renewable
	c.Detail = strings.Join(servers, ", ")
	if renewable {
		c.Detail += " — refreshes on the next call"
	}
	if len(started) > 0 {
		sort.Strings(started)
		c.State = StateUnknown
		c.Detail += " — but " + strings.Join(started, ", ") + " holds no token"
	}
	if soonest.IsZero() {
		c.State = StateUnknown
		c.Detail += " — authorised, no expiry recorded"
	}
	return c
}

// linearFromConfig is the fallback for a machine that keeps its MCP oauth
// sessions in the login keychain rather than in a file — which is every macOS
// install of Claude Code, and therefore the common case rather than the odd
// one. The session itself cannot be read from here without prompting for a
// secret nothing needs, so what is reported is the honest half: the servers
// are configured, and whether their sessions are live is a question only a
// call can answer.
func linearFromConfig(c Credential) Credential {
	path := home(".claude.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		c.State, c.Detail = StateMissing, "no Linear MCP server configured — /mcp in a session adds one"
		return c
	}

	var file struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	if json.Unmarshal(raw, &file) != nil {
		c.State, c.Detail = StateUnknown, "the Claude Code config could not be read"
		return c
	}

	var servers []string
	for name := range file.Servers {
		if strings.Contains(strings.ToLower(name), "linear") {
			servers = append(servers, name)
		}
	}
	if len(servers) == 0 {
		c.State, c.Detail = StateMissing, "no Linear MCP server configured — /mcp in a session adds one"
		return c
	}
	sort.Strings(servers)

	c.State, c.Source = StateUnknown, tilde(path)
	c.Detail = strings.Join(servers, ", ") + " — the oauth session is in the login keychain, not read from here"
	return c
}

// ── Buffer ───────────────────────────────────────────────────

// checkBuffer is the shape every credential fetched from the vault has: there
// is nothing on this machine to read, because the point of keeping it in
// 1Password is that it is not on this machine. So what is checked is the two
// things that have to be true for `buffer` to work at all — the CLI, and a way
// to reach the vault — and the row says out loud that its real expiry is the
// vault's.
func checkBuffer() Credential {
	c := Credential{Name: "buffer", What: "Buffer (via 1Password)"}

	if _, err := exec.LookPath("buffer"); err != nil {
		c.State, c.Detail = StateMissing, "the buffer CLI is not on PATH"
		return c
	}
	if !hasOnePassword() {
		c.State, c.Source = StateMissing, "1Password"
		c.Detail = "the key is in the vault and there is no way to reach it from here"
		return c
	}

	c.State, c.Source = StateUnknown, "1Password"
	c.Detail = "key read from the vault at call time — it lapses when 1Password does"
	return c
}

// ── 1Password ────────────────────────────────────────────────

// checkOnePassword is the one everything else stands on, and the one nothing
// can date.
//
// A service-account token is an envelope of credentials rather than a JWT:
// there is no `exp` in it to read, and the expiry it does have was chosen in a
// web UI on the day it was minted. So the honest machine answer is "present,
// expiry unknowable", and FACTORY_AUTH_EXPIRY is how the date somebody wrote
// down gets onto the screen beside it.
func checkOnePassword() Credential {
	c := Credential{Name: "1password", What: "1Password service account"}

	if os.Getenv("OP_SERVICE_ACCOUNT_TOKEN") != "" {
		c.State, c.Source = StateUnknown, "$OP_SERVICE_ACCOUNT_TOKEN"
		c.Detail = "service account token in the environment — the token records no expiry"
		return c
	}
	if path := opTokenPath(); exists(path) {
		c.State, c.Source = StateUnknown, tilde(path)
		c.Detail = "service account token on disk, not exported to this shell"
		return c
	}
	if _, err := exec.LookPath("op"); err != nil {
		c.State, c.Detail = StateMissing, "the op CLI is not on PATH"
		return c
	}
	c.State, c.Detail = StateMissing, "no service account token — agents will hit Touch ID instead"
	return c
}

func hasOnePassword() bool {
	return os.Getenv("OP_SERVICE_ACCOUNT_TOKEN") != "" || exists(opTokenPath())
}

func opTokenPath() string { return home(".config", "op", "token") }

// millis turns a millisecond epoch into a time, which is the unit both Claude
// Code files use.
func millis(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.Unix(ms/1000, (ms%1000)*int64(time.Millisecond))
}
