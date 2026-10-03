package main

import (
	"fmt"
	"strings"
	"testing"
)

func gridOut(cols int, labels bool, rows ...string) string {
	var b strings.Builder
	g := &grid{out: &b, cols: cols, labels: labels}
	for _, r := range rows {
		fmt.Fprintln(g, r)
	}
	g.Flush()
	return b.String()
}

func TestGridFitsAsTable(t *testing.T) {
	got := gridOut(80, false, "ID\tSTATUS\tTASK", "abc123\tdone\tfix it")
	want := "ID      STATUS  TASK\nabc123  done    fix it\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestGridNarrowBlocks(t *testing.T) {
	got := gridOut(30, false,
		"ID\tSTATUS\tAGE\tREPO\tPR\tTASK",
		"nwxzue\tdone\t3d\thev/pov-bcc\t#716 merged\tLYR-180 blocks vj5zun BCC acceptance: expired Function process",
		"i63szp\tdone\t3d\t-\t-\tgaffer for job vj5zun")
	want := "nwxzue  done  3d  hev/pov-bcc\n" +
		"  #716 merged\n" +
		"  LYR-180 blocks vj5zun BCC\n" +
		"  acceptance: expired Functio…\n" +
		"i63szp  done  3d\n" +
		"  gaffer for job vj5zun\n"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	for _, l := range strings.Split(strings.TrimRight(got, "\n"), "\n") {
		if width(l) > 30 {
			t.Errorf("line wider than 30: %q", l)
		}
	}
}

func TestGridNarrowLabels(t *testing.T) {
	got := gridOut(30, true, "HOST\tLIVE\tCLAUDE WEEK", "mini\t3/8\t40% (resets Mon 09:00)")
	want := "mini  live 3/8\n  claude week 40% (resets Mon 09:00)\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
