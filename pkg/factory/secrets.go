package factory

import (
	"fmt"
	"os/exec"
	"strings"
)

// The login keychain is where a factory's credentials live. Service is a
// constant so `security dump-keychain | grep hev-factory` finds every one of
// them; the account carries the instance, because a webhook belongs to one
// factory the same way its repo scope does.
//
//	service  hev-factory
//	account  <instance>/SLACK_WEBHOOK_URL
//
// The shell side of this is scripts/lib/secrets.sh, which reads the same two
// fields and falls back to ~/.factory/secrets on a machine without a keychain.
const keychainService = "hev-factory"

// SecretAccount is the account name a secret is filed under. Exported because
// error messages quote it — somebody who has to set one by hand should be able
// to copy the account out of the message.
func SecretAccount(instance, name string) string {
	if instance == "" {
		return name
	}
	return instance + "/" + name
}

// SecretGet returns a secret from the login keychain, or "" if it is not there
// or the machine has no `security` at all. Never an error: an absent secret is
// a factory with no Slack, which is a normal factory.
func SecretGet(instance, name string) string {
	out, err := exec.Command("security", "find-generic-password",
		"-s", keychainService, "-a", SecretAccount(instance, name), "-w").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// SecretSet writes one, replacing whatever was there (-U). This is what keeps
// `factory init --slack-webhook` from putting a credential in a config file —
// gitignoring factories/*.toml stops it being committed and nothing else, and
// a URL that grants posting rights to a channel deserves better than a file
// somebody screen-shares.
func SecretSet(instance, name, value string) error {
	cmd := exec.Command("security", "add-generic-password", "-U",
		"-s", keychainService, "-a", SecretAccount(instance, name), "-w", value)
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("could not write %s to the keychain: %s", SecretAccount(instance, name), msg)
	}
	return nil
}
