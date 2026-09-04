package auth

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// isolate points every home-relative read at a temp dir, so a check in a test
// reads the files the test wrote rather than the ones this machine happens to
// have. Every credential this package reads is under $HOME, which is what
// makes that enough.
func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, name := range []string{
		"ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN",
		"GH_TOKEN", "GITHUB_TOKEN", "GH_CONFIG_DIR",
		"CLOUDFLARE_API_TOKEN", "CF_API_TOKEN",
		"OP_SERVICE_ACCOUNT_TOKEN", "FACTORY_AUTH_EXPIRY",
	} {
		t.Setenv(name, "")
	}
	return home
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// jwt builds a token whose payload carries exp. Nothing here verifies a
// signature, so the signature is not part of the fixture.
func jwt(exp time.Time) string {
	payload, _ := json.Marshal(map[string]int64{"exp": exp.Unix()})
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

func TestGradeTurnsDatesIntoStates(t *testing.T) {
	isolate(t)
	now := time.Now()
	for _, tc := range []struct {
		name string
		cred Credential
		want State
	}{
		{"well in the future", Credential{Expires: now.Add(90 * 24 * time.Hour)}, StateOK},
		{"inside the warning", Credential{Expires: now.Add(3 * 24 * time.Hour)}, StateExpiring},
		{"gone", Credential{Expires: now.Add(-time.Hour)}, StateExpired},
		// A refreshable token's own expiry is not news in either direction:
		// it renews itself, and an amber row every few hours is how a screen
		// teaches people to stop reading it.
		{"refreshable and lapsed", Credential{Expires: now.Add(-time.Hour), Refreshable: true}, StateOK},
		{"refreshable and expiring", Credential{Expires: now.Add(time.Hour), Refreshable: true}, StateOK},
		// Whether a file exists outranks anything a date could say.
		{"never logged in", Credential{State: StateMissing}, StateMissing},
		// No date recorded is its own answer, and not a quiet "fine".
		{"no date at all", Credential{State: StateUnknown}, StateUnknown},
	} {
		if got := grade(tc.cred).State; got != tc.want {
			t.Errorf("%s: state = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A pinned date is the only way a credential with no machine-readable expiry
// reaches the screen — the 1Password service account being the case it exists
// for.
func TestPinnedExpiryReachesTheRow(t *testing.T) {
	isolate(t)
	t.Setenv("FACTORY_AUTH_EXPIRY", "1password=2099-11-03, codex = 2000-01-01")

	pinned := grade(Credential{Name: "1password", State: StateUnknown})
	if !pinned.Pinned || pinned.State != StateOK {
		t.Fatalf("pinned future date = %+v, want ok and pinned", pinned)
	}
	if pinned.Expires.Year() != 2099 {
		t.Errorf("pinned expiry = %v", pinned.Expires)
	}
	if got := grade(Credential{Name: "codex", State: StateUnknown}).State; got != StateExpired {
		t.Errorf("pinned past date = %v, want expired", got)
	}
	// A credential that was never logged in stays that way: a date somebody
	// wrote down cannot conjure a token onto the machine.
	if got := grade(Credential{Name: "1password", State: StateMissing}).State; got != StateMissing {
		t.Errorf("pinned over missing = %v, want missing", got)
	}
}

func TestCodexReadsTheAccessTokensExpiry(t *testing.T) {
	home := isolate(t)
	exp := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	write(t, filepath.Join(home, ".codex", "auth.json"), `{
	  "auth_mode": "chatgpt",
	  "OPENAI_API_KEY": null,
	  "tokens": {"access_token": "`+jwt(exp)+`", "refresh_token": "r"}
	}`)

	c := grade(checkCodex())
	if !c.Expires.Equal(exp) {
		t.Errorf("expiry = %v, want %v", c.Expires, exp)
	}
	if !c.Refreshable || c.State != StateOK {
		t.Errorf("credential = %+v, want refreshable and ok", c)
	}
}

func TestCodexWithNoAuthFileIsNotLoggedIn(t *testing.T) {
	isolate(t)
	if got := grade(checkCodex()).State; got != StateMissing {
		t.Errorf("state = %v, want missing", got)
	}
}

// gh keeps its token in the login keychain on macOS and in the hosts file
// where there is no keychain, so a hosts file with a user and no token is the
// ordinary shape rather than a broken one. Reading it as "not logged in" was
// the first thing this check got wrong.
func TestGHHostsFileWithATokenIsALogin(t *testing.T) {
	home := isolate(t)
	write(t, filepath.Join(home, ".config", "gh", "hosts.yml"),
		"github.com:\n    user: hev\n    oauth_token: gho_xxx\n")

	c := grade(checkGH())
	if c.State != StateUnknown {
		t.Fatalf("state = %v, want unknown — gh records no expiry", c.State)
	}
	for _, want := range []string{"hev", "github.com", "hosts file"} {
		if !strings.Contains(c.Detail, want) {
			t.Errorf("detail %q is missing %q", c.Detail, want)
		}
	}
}

func TestGHWithNoConfigIsNotLoggedIn(t *testing.T) {
	isolate(t)
	if got := grade(checkGH()).State; got != StateMissing {
		t.Errorf("state = %v, want missing", got)
	}
}

// A static token in the environment silently outranks every login on the
// machine, which is worth saying out loud on a screen somebody opened because
// a session keeps logging itself out.
func TestEnvironmentTokensAreReportedAsOverriding(t *testing.T) {
	isolate(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	c := grade(checkClaude())
	if c.State != StateOK || !strings.Contains(c.Detail, "overrides") {
		t.Fatalf("claude = %+v, want ok and an override note", c)
	}
	if c.Source != "$ANTHROPIC_API_KEY" {
		t.Errorf("source = %q", c.Source)
	}
}

func TestCloudflareReadsWranglersExpiry(t *testing.T) {
	home := isolate(t)
	write(t, filepath.Join(home, "Library", "Preferences", ".wrangler", "config", "default.toml"),
		`oauth_token = "t"
refresh_token = "r"
expiration_time = "2020-07-21T07:04:42.934Z"
`)

	c := grade(checkCloudflare())
	if c.Expires.Year() != 2020 {
		t.Fatalf("expiry = %v", c.Expires)
	}
	// Long gone, but a refresh token sits beside it, so the next wrangler
	// command fixes it and nobody needs to be told.
	if !c.Refreshable || c.State != StateOK {
		t.Errorf("credential = %+v, want refreshable and ok", c)
	}
}

func TestAttentionCountsOnlyWhatSomebodyWouldActOn(t *testing.T) {
	creds := []Credential{
		{State: StateOK}, {State: StateUnknown},
		{State: StateExpiring}, {State: StateExpired}, {State: StateMissing},
	}
	if got := Attention(creds); got != 3 {
		t.Errorf("attention = %d, want 3", got)
	}
}

func TestSoonestPicksTheNearestRecordedDate(t *testing.T) {
	now := time.Now()
	creds := []Credential{
		{Name: "far", Expires: now.Add(100 * time.Hour)},
		{Name: "near", Expires: now.Add(time.Hour)},
		{Name: "undated"},
	}
	c, ok := Soonest(creds)
	if !ok || c.Name != "near" {
		t.Fatalf("soonest = %+v %v, want near", c, ok)
	}
	if _, ok := Soonest([]Credential{{Name: "undated"}}); ok {
		t.Error("nothing with a date should report no soonest")
	}
}
