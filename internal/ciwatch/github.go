package ciwatch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type CLI struct{}

func gh(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	b, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		if len(args) > 1 && args[0] == "pr" && args[1] == "checks" && strings.HasPrefix(stderr.String(), "no checks reported on the ") {
			return []byte("[]"), nil
		}
		return b, fmt.Errorf("gh: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return b, nil
}
func (CLI) PullRequest(repo string, pr int) (PR, error) {
	var p PR
	b, err := gh("pr", "view", strconv.Itoa(pr), "--repo", repo, "--json", "headRefOid,state")
	if err != nil {
		return p, err
	}
	if err = json.Unmarshal(b, &p); err != nil {
		return p, err
	}
	if p.Head == "" || (p.State != "OPEN" && p.State != "CLOSED" && p.State != "MERGED") {
		return p, fmt.Errorf("invalid PR response")
	}
	return p, nil
}
func (CLI) Checks(repo string, pr int) ([]Check, error) {
	b, err := gh("pr", "checks", strconv.Itoa(pr), "--repo", repo, "--json", "name,bucket,link")
	// gh uses nonzero exits for pending/failed checks, with valid JSON.
	// Malformed output, authentication failures and transport errors stay errors.
	var checks []Check
	if json.Unmarshal(b, &checks) == nil && bytes.HasPrefix(bytes.TrimSpace(b), []byte("[")) {
		return checks, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("invalid checks response")
}
