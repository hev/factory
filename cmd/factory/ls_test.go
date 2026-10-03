package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/hev/factory/internal/fleet"
)

func TestRecentSessions(t *testing.T) {
	now := time.Now()
	s := func(id, status string, created, updated time.Duration) fleet.Session {
		var x fleet.Session
		x.ID, x.Status, x.CreatedAt, x.UpdatedAt = id, status, now.Add(-created), now.Add(-updated)
		return x
	}
	ss := []fleet.Session{
		s("old-live", fleet.Running, 200*time.Hour, time.Minute),
		s("new-live", fleet.Interactive, time.Hour, time.Minute),
		s("stale", fleet.Done, 100*time.Hour, 30*time.Hour),
	}
	for i := 0; i < 12; i++ {
		ss = append(ss, s(fmt.Sprintf("done%02d", i), fleet.Done, 5*time.Hour, time.Duration(i+1)*time.Minute))
	}
	rows, older := recentSessions(ss, false)
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	want := "[new-live old-live done00 done01 done02 done03 done04 done05 done06 done07 done08 done09]"
	if got := fmt.Sprint(ids); got != want || older != 3 {
		t.Fatalf("got %s, %d older; want %s, 3 older", got, older, want)
	}
	if rows, older := recentSessions(ss, true); len(rows) != len(ss) || older != 0 {
		t.Fatalf("--all: %d rows, %d older", len(rows), older)
	}
}
