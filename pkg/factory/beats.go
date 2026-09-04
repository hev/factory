package factory

import (
	"os"
	"path/filepath"
	"time"
)

// LastBeat is when a factory last finished an iteration, read from the beat
// log's mtime. The beat log is the honest record of a completed beat: the
// heartbeat file is also touched at boot, so it would say "beat" about a
// factory that has never finished one
// (docs/feedback/2026-08-21-first-factory.md).
//
// False means no beat has ever completed on this machine.
func LastBeat(instance string) (time.Time, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return time.Time{}, false
	}
	info, err := os.Stat(filepath.Join(home, ".factory", "beats", instance+".jsonl"))
	if err != nil {
		return time.Time{}, false
	}
	return info.ModTime(), true
}
