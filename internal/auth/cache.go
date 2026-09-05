package auth

import (
	"sync"
	"time"
)

// refreshInterval is how often the credentials are re-read in the background.
// A minute rather than the floor's two seconds: a token's expiry is a date,
// not a state that flickers, and the only thing that changes it inside a
// minute is somebody logging in — which they did at a shell, and will look at
// this screen to confirm.
const refreshInterval = time.Minute

var (
	mu      sync.RWMutex
	latest  []Credential
	started bool
)

// Start begins reading credentials in the background, so the floor's header
// can carry the count without ever waiting for a `security` call. Calling it
// again is a no-op.
func Start() {
	mu.Lock()
	defer mu.Unlock()
	if started {
		return
	}
	started = true
	go func() {
		for {
			set(Check())
			time.Sleep(refreshInterval)
		}
	}()
}

// Latest is the last reading, which is empty until the first one lands. An
// empty reading draws no header token, which is the right behaviour: a screen
// that has not looked yet should say nothing rather than say everything is
// fine.
func Latest() []Credential {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Credential, len(latest))
	copy(out, latest)
	return out
}

// Refresh re-reads now and returns the result, for ^r on the auth screen.
func Refresh() []Credential {
	creds := Check()
	set(creds)
	return creds
}

// Store replaces the cached reading, so a live probe's narrower answer is what
// the header counts until the next background pass.
func Store(creds []Credential) { set(creds) }

func set(creds []Credential) {
	mu.Lock()
	latest = creds
	mu.Unlock()
}
