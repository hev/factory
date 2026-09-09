package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/hev/factory/internal/ciwatch"
)

const ciUsage = "usage: factory ci wait INSTANCE WORKER OWNER/REPO PR | poll INSTANCE | list INSTANCE [--ready] | ack INSTANCE ID"

func runCI(root string, args []string) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Println(ciUsage)
		return nil
	}
	if len(args) < 2 {
		return fmt.Errorf("%s", ciUsage)
	}
	verb, instance := args[0], args[1]
	// Validate before constructing a config or state path.
	if instance == "" || strings.ContainsAny(instance, "/\\") || instance == "." || instance == ".." {
		return fmt.Errorf("invalid instance")
	}
	var cfg struct {
		Home  string   `toml:"home_host"`
		Repos []string `toml:"repo_scope"`
	}
	if _, err := toml.DecodeFile(filepath.Join(root, "factories", instance+".toml"), &cfg); err != nil {
		return err
	}
	host, err := os.Hostname()
	if err != nil {
		return err
	}
	host = strings.SplitN(host, ".", 2)[0]
	if override := os.Getenv("FACTORY_HOSTNAME_OVERRIDE"); override != "" {
		host = override
	}
	if cfg.Home != "" && !strings.EqualFold(host, cfg.Home) {
		return fmt.Errorf("CI watches live on home_host %s; this host is %s", cfg.Home, host)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	ledgerDir := os.Getenv("FACTORY_LEDGER_DIR")
	if ledgerDir == "" {
		ledgerDir = filepath.Join(home, ".factory", "children")
	}
	s := ciwatch.Store{LedgerDir: ledgerDir, Dir: filepath.Join(home, ".factory", "ci", instance), Instance: instance, Repos: cfg.Repos, GH: ciwatch.CLI{}}
	enc := json.NewEncoder(os.Stdout)
	switch verb {
	case "wait":
		if len(args) != 5 {
			return fmt.Errorf("%s", ciUsage)
		}
		pr, err := strconv.Atoi(args[4])
		if err != nil {
			return err
		}
		w, err := s.Wait(args[2], args[3], pr)
		if err != nil {
			return err
		}
		return enc.Encode(map[string]any{"id": w.ID, "state": w.State, "head": w.Head, "url": w.URL()})
	case "poll":
		if len(args) != 2 {
			return fmt.Errorf("%s", ciUsage)
		}
		return s.Poll()
	case "list":
		if len(args) > 3 || (len(args) == 3 && args[2] != "--ready") {
			return fmt.Errorf("%s", ciUsage)
		}
		all, err := s.List()
		if err != nil {
			return err
		}
		out := []ciwatch.Watch{}
		for _, w := range all {
			if len(args) == 2 || w.Ready() {
				out = append(out, w)
			}
		}
		return enc.Encode(out)
	case "ack":
		if len(args) != 3 {
			return fmt.Errorf("%s", ciUsage)
		}
		return s.Ack(args[2])
	default:
		return fmt.Errorf("%s", ciUsage)
	}
}
