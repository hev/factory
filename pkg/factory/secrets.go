package factory

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// A factory's credentials live in 1Password, and the login keychain is the
// fallback for a machine provisioned before that was true.
//
//	1Password  vault $FACTORY_OP_VAULT (default "layer-factory"),
//	           item "<NAME>_<INSTANCE>" then "<NAME>", field "credential"
//	keychain   service hev-factory, account "<instance>/<NAME>"
//
// The reason for the order is remote provisioning, and it is not a preference.
// The login keychain refuses non-interactive access over ssh — `security`
// answers "User interaction is not allowed" — so a factory whose home_host is
// another machine could not be given its webhook without physically sitting at
// that machine. `op` with a service-account token has no such limit. 1Password
// is also already the house store (~/shell/sync-secrets.sh reads the same
// vault), so a credential rotates in one place instead of two.
//
// The shell side of this is scripts/lib/secrets.sh, which consults the same
// places in the same order and adds ~/.factory/secrets beneath them.
const keychainService = "hev-factory"

// opLookupTimeout bounds every `op` call. It is normally a second or two, but a
// cold or wedged agent has hung for minutes on these machines, and this code
// runs on the beat — a stalled loop is a worse failure than a missing webhook.
const opLookupTimeout = 10 * time.Second

// opVault is the vault to look in. Setting FACTORY_OP_VAULT to the empty string
// disables 1Password entirely and leaves the keychain in charge, which is the
// escape hatch for a machine with no `op`.
func opVault() string {
	if v, ok := os.LookupEnv("FACTORY_OP_VAULT"); ok {
		return v
	}
	return "layer-factory"
}

// SecretTitle is the 1Password item title a secret is filed under:
// "SLACK_WEBHOOK_URL" + instance "path" -> "SLACK_WEBHOOK_URL_PATH". It is the
// same shape ~/.factory/secrets uses and the same shape sync-secrets.sh
// exports, so one credential answers to one name wherever it is stored.
func SecretTitle(instance, name string) string {
	if instance == "" {
		return name
	}
	return name + "_" + strings.ToUpper(strings.NewReplacer("-", "_").Replace(instance))
}

// SecretAccount is the keychain account name a secret is filed under. Exported
// because error messages quote it — somebody who has to set one by hand should
// be able to copy the account out of the message.
func SecretAccount(instance, name string) string {
	if instance == "" {
		return name
	}
	return instance + "/" + name
}

// opRead returns one op:// reference's value, or "" for any failure at all —
// op absent, not signed in, item missing, or too slow. Never an error: the
// caller's next stop is the keychain.
func opRead(ref string) string {
	ctx, cancel := context.WithTimeout(context.Background(), opLookupTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "op", "read", ref).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// SecretGet returns a secret, or "" if it is nowhere. Never an error: an absent
// secret is a factory with no Slack, which is a normal factory.
func SecretGet(instance, name string) string {
	if vault := opVault(); vault != "" {
		if instance != "" {
			if v := opRead(fmt.Sprintf("op://%s/%s/credential", vault, SecretTitle(instance, name))); v != "" {
				return v
			}
		}
		if v := opRead(fmt.Sprintf("op://%s/%s/credential", vault, name)); v != "" {
			return v
		}
	}

	out, err := exec.Command("security", "find-generic-password",
		"-s", keychainService, "-a", SecretAccount(instance, name), "-w").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// SecretStore names where SecretSet actually put a credential, so the caller
// can say so rather than guessing. A factory that reports the wrong store sends
// the next person looking in the wrong place during a rotation.
type SecretStore string

const (
	StoreOnePassword SecretStore = "1Password"
	StoreKeychain    SecretStore = "login keychain"
)

// SecretSet writes one, replacing whatever was there. This is what keeps
// `factory init --slack-webhook` from putting a credential in a config file —
// gitignoring factories/*.toml stops it being committed and nothing else, and a
// URL that grants posting rights to a channel deserves better than a file
// somebody screen-shares.
//
// It tries 1Password first for the same reason SecretGet does: that is the copy
// every machine can read, and the only one reachable over ssh.
func SecretSet(instance, name, value string) (SecretStore, error) {
	if vault := opVault(); vault != "" {
		title := SecretTitle(instance, name)
		field := "credential=" + value
		// Edit an existing item before creating one, so re-running init on a
		// factory that already has a webhook updates it instead of leaving two
		// items with the same title for `op read` to choose between.
		ctx, cancel := context.WithTimeout(context.Background(), opLookupTimeout)
		err := exec.CommandContext(ctx, "op", "item", "edit", title, "--vault", vault, field).Run()
		cancel()
		if err == nil {
			return StoreOnePassword, nil
		}
		ctx, cancel = context.WithTimeout(context.Background(), opLookupTimeout)
		err = exec.CommandContext(ctx, "op", "item", "create",
			"--category", "API Credential", "--title", title, "--vault", vault, field).Run()
		cancel()
		if err == nil {
			return StoreOnePassword, nil
		}
	}

	cmd := exec.Command("security", "add-generic-password", "-U",
		"-s", keychainService, "-a", SecretAccount(instance, name), "-w", value)
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("could not write %s to 1Password or the keychain: %s",
			SecretAccount(instance, name), msg)
	}
	return StoreKeychain, nil
}
