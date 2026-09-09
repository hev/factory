package ciwatch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestChecksExitCodesAndMissingChecks(t *testing.T) {
	d := t.TempDir()
	path := filepath.Join(d, "gh")
	t.Setenv("PATH", d)
	for _, tc := range []struct {
		name, script, bucket string
		wantErr              bool
	}{
		{"pending", `printf '[{"name":"test","bucket":"pending"}]'; exit 8`, "pending", false},
		{"failed", `printf '[{"name":"test","bucket":"fail"}]'; exit 1`, "fail", false},
		{"empty", `echo "no checks reported on the 'branch' branch" >&2; exit 1`, "", false},
		{"auth", `echo 'authentication failed' >&2; exit 1`, "", true},
		{"malformed", `echo 'not json'`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte("#!/bin/sh\n"+tc.script+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			checks, err := (CLI{}).Checks("acme/api", 1)
			if (err != nil) != tc.wantErr {
				t.Fatalf("checks=%v error=%v", checks, err)
			}
			if tc.bucket != "" && (len(checks) != 1 || checks[0].Bucket != tc.bucket) {
				t.Fatal(checks)
			}
		})
	}
}
