package auth

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// probeTimeout bounds every live check. It is not a guess: `op whoami` on a
// headless Mac reached over ssh has been measured sitting for minutes without
// answering, and a screen that can be wedged by a credential check is worse
// than one that says it could not check.
const probeTimeout = 8 * time.Second

// Probe asks the services themselves, for the credentials whose files cannot
// answer. It is the opt-in pass behind ^r on the auth screen, never the
// refresh path: these are network calls, and one of them is a network call to
// something that can hang.
//
// It only ever *narrows* uncertainty. A probe that times out leaves the row
// exactly as the files described it, with a note saying the live check did not
// come back — the reading is never made worse by having asked.
func Probe(creds []Credential) []Credential {
	out := make([]Credential, len(creds))
	copy(out, creds)

	ghOK, ghDetail, ghAnswered := probeGH()
	opOK, opDetail, opAnswered := probeOnePassword()

	for i := range out {
		switch out[i].Name {
		case "gh":
			if !ghAnswered {
				out[i].Detail += " · live check did not answer"
				continue
			}
			if ghOK {
				out[i].State, out[i].Detail = StateOK, ghDetail
			} else {
				out[i].State, out[i].Detail = StateExpired, ghDetail
			}
		case "1password", "buffer":
			if !opAnswered {
				out[i].Detail += " · live check did not answer"
				continue
			}
			if opOK {
				// The vault answers, so the token is live. It still has no
				// readable expiry, so a pinned date is the only thing that
				// can make this row anything but "working right now".
				if out[i].State != StateExpiring && out[i].State != StateExpired {
					out[i].State = StateOK
				}
				out[i].Detail = opDetail
			} else {
				out[i].State, out[i].Detail = StateExpired, opDetail
			}
		}
	}
	return out
}

// probeGH asks GitHub whether the token still works. This is the check worth
// paying for: gh's own file cannot tell a live token from a revoked one, and a
// revoked one is how a worker runs for an hour and opens no pull request.
func probeGH() (ok bool, detail string, answered bool) {
	out, err, timedOut := runBounded("gh", "auth", "status")
	if timedOut {
		return false, "", false
	}
	if err != nil {
		return false, firstLine(out, "github.com rejected the token — run `gh auth login`"), true
	}
	// gh prints the account and the scopes; the account line is the useful half.
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Logged in to") {
			return true, squash(strings.TrimLeft(strings.TrimSpace(line), "✓ ")), true
		}
	}
	return true, "github.com accepted the token", true
}

// probeOnePassword asks the vault who it thinks is calling. Everything fetched
// at call time — the Buffer key among them — is only as alive as this answer.
func probeOnePassword() (ok bool, detail string, answered bool) {
	if _, err := exec.LookPath("op"); err != nil {
		return false, "", false
	}
	out, err, timedOut := runBounded("op", "whoami")
	if timedOut {
		return false, "", false
	}
	if err != nil {
		return false, firstLine(out, "1Password rejected the token"), true
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "URL:") || strings.Contains(line, "Service Account") {
			return true, "the vault answered — " + squash(line), true
		}
	}
	return true, "the vault answered", true
}

// squash collapses the column padding a CLI writes for a human reading a
// terminal, which becomes a run of spaces in the middle of a sentence once the
// line is quoted inside a row.
func squash(line string) string {
	return strings.Join(strings.Fields(line), " ")
}

// runBounded runs a command under the probe timeout and says whether it was
// the timeout that ended it, because "it said no" and "it never said anything"
// are different answers and only one of them is about the credential.
func runBounded(name string, args ...string) (out string, err error, timedOut bool) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	raw, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(raw), err, ctx.Err() == context.DeadlineExceeded
}

func firstLine(out, fallback string) string {
	for _, line := range strings.Split(out, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return fallback
}
