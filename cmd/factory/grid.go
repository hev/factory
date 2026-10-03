package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"unicode/utf8"
	"unsafe"
)

// grid is a table that fits the terminal. Rows are written tab-separated, the
// header first. When the aligned table fits it prints as one; when it would
// wrap (a phone over ssh) each row prints as a short block instead: the first
// cell, the short cells packed after it, and the last cell, the free text, on
// its own lines underneath.
type grid struct {
	buf    bytes.Buffer
	out    io.Writer
	cols   int
	labels bool // name each cell in a block, for values that mean nothing without their header
}

func table() *grid { return &grid{out: os.Stdout, cols: termCols()} }

func (g *grid) Write(p []byte) (int, error) { return g.buf.Write(p) }

func (g *grid) Flush() error {
	var wide bytes.Buffer
	tw := tabwriter.NewWriter(&wide, 0, 0, 2, ' ', 0)
	tw.Write(g.buf.Bytes())
	tw.Flush()
	if g.cols <= 0 || widest(wide.String()) <= g.cols {
		_, err := g.out.Write(wide.Bytes())
		return err
	}
	lines := strings.Split(strings.TrimRight(g.buf.String(), "\n"), "\n")
	head := strings.Split(lines[0], "\t")
	var b strings.Builder
	for _, line := range lines[1:] {
		cells := strings.Split(line, "\t")
		short, text := cells[1:], ""
		if !g.labels && len(cells) > 2 {
			short, text = cells[1:len(cells)-1], cells[len(cells)-1]
		}
		cur := cells[0]
		for i, c := range short {
			if c == "-" || c == "" {
				continue
			}
			if g.labels && i+1 < len(head) {
				c = strings.ToLower(head[i+1]) + " " + c
			}
			if width(cur)+2+width(c) > g.cols {
				fmt.Fprintln(&b, cur)
				cur = ""
			}
			cur += "  " + c
		}
		fmt.Fprintln(&b, cur)
		for _, l := range wrap(text, g.cols-2, 2) {
			fmt.Fprintln(&b, "  "+l)
		}
	}
	_, err := io.WriteString(g.out, b.String())
	return err
}

// wrap breaks s into at most max lines of n runes at spaces, clipping the last.
func wrap(s string, n, max int) []string {
	var out []string
	words := strings.Fields(s)
	for len(words) > 0 && len(out) < max {
		line := words[0]
		words = words[1:]
		for len(words) > 0 && width(line)+1+width(words[0]) <= n {
			line += " " + words[0]
			words = words[1:]
		}
		if len(out) == max-1 && len(words) > 0 {
			line = oneLine(line+" "+strings.Join(words, " "), n)
		}
		out = append(out, oneLine(line, n))
	}
	return out
}

func width(s string) int { return utf8.RuneCountInString(s) }

func widest(s string) int {
	n := 0
	for _, l := range strings.Split(s, "\n") {
		n = max(n, width(l))
	}
	return n
}

// termCols is stdout's terminal width, or 0 when stdout is not a terminal.
// COLUMNS, when exported, wins.
func termCols() int {
	if n, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil {
		return n
	}
	var ws struct{ Row, Col, X, Y uint16 }
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, os.Stdout.Fd(), syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&ws)))
	if e != 0 {
		return 0
	}
	return int(ws.Col)
}
