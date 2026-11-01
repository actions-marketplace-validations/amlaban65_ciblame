//go:build ignore

// ansi2svg renders ANSI-colored terminal output as an SVG "screenshot" for
// the README. Usage:
//
//	go run ./cmd/ciblame --demo --until 2026-11-01 --color always | go run tools/ansi2svg/main.go > docs/demo.svg
package main

import (
	"bufio"
	"fmt"
	"html"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	fontSize  = 13
	charW     = 7.8
	lineH     = 19
	padX      = 22
	padTop    = 52
	padBottom = 22
)

var colors = map[int]string{
	31: "#ff7b72", 32: "#7ee787", 33: "#e3b341", 34: "#79c0ff", 35: "#d2a8ff", 36: "#56d4dd", 37: "#e6edf3",
}

type style struct {
	bold, dim bool
	fg        string
}

type seg struct {
	col  int
	text string
	st   style
}

var sgr = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

func main() {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var lines [][]seg
	maxCols := 0
	for sc.Scan() {
		line := sc.Text()
		var segs []seg
		st := style{}
		col := 0
		rest := line
		for rest != "" {
			loc := sgr.FindStringSubmatchIndex(rest)
			text := rest
			if loc != nil {
				text = rest[:loc[0]]
			}
			if text != "" {
				segs = append(segs, seg{col: col, text: text, st: st})
				col += utf8.RuneCountInString(text)
			}
			if loc == nil {
				break
			}
			for _, p := range strings.Split(rest[loc[2]:loc[3]], ";") {
				n, _ := strconv.Atoi(p)
				switch {
				case n == 0:
					st = style{}
				case n == 1:
					st.bold = true
				case n == 2:
					st.dim = true
				case colors[n] != "":
					st.fg = colors[n]
				}
			}
			rest = rest[loc[1]:]
		}
		if col > maxCols {
			maxCols = col
		}
		lines = append(lines, segs)
	}
	for len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}

	w := float64(padX*2) + float64(maxCols)*charW
	h := padTop + len(lines)*lineH + padBottom
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%.0f" height="%d" viewBox="0 0 %.0f %d" role="img" aria-label="ciblame terminal output">`+"\n", w, h, w, h)
	fmt.Fprintf(&b, `<rect width="100%%" height="100%%" rx="10" fill="#0d1117"/>`+"\n")
	b.WriteString(`<circle cx="22" cy="20" r="6" fill="#ff5f57"/><circle cx="42" cy="20" r="6" fill="#febc2e"/><circle cx="62" cy="20" r="6" fill="#28c840"/>` + "\n")
	fmt.Fprintf(&b, `<g font-family="ui-monospace,SFMono-Regular,Menlo,Consolas,'DejaVu Sans Mono',monospace" font-size="%d" fill="#c9d1d9">`+"\n", fontSize)
	for i, segs := range lines {
		y := padTop + i*lineH
		for _, s := range segs {
			attrs := ""
			if s.st.fg != "" {
				attrs += fmt.Sprintf(` fill="%s"`, s.st.fg)
			}
			if s.st.bold {
				attrs += ` font-weight="700" fill-opacity="1"`
				if s.st.fg == "" {
					attrs += ` fill="#f0f6fc"`
				}
			}
			if s.st.dim {
				attrs += ` fill-opacity="0.55"`
			}
			// Emit each word at its own grid column. Browsers collapse
			// whitespace inside <text> unless told otherwise, and xml:space on
			// a parent group isn't honoured everywhere, so spaces are never
			// sent; positions alone produce the alignment.
			for _, w := range words(s.text) {
				x := float64(padX) + float64(s.col+w.col)*charW
				// Draw runs of full blocks as one rectangle; glyph-by-glyph
				// they show hairline seams in most browsers.
				if trimmed := strings.TrimLeft(w.text, "█"); trimmed != w.text {
					blocks := utf8.RuneCountInString(w.text) - utf8.RuneCountInString(trimmed)
					fill := s.st.fg
					if fill == "" {
						fill = "#c9d1d9"
					}
					fmt.Fprintf(&b, `<rect x="%.1f" y="%d" width="%.1f" height="%d" fill="%s"/>`+"\n",
						x, y-13, float64(blocks)*charW, 16, fill)
					if trimmed == "" {
						continue
					}
					x += float64(blocks) * charW
					w.text = trimmed
				}
				n := utf8.RuneCountInString(w.text)
				// textLength keeps the word on the grid if the font's advance
				// differs slightly from charW.
				fmt.Fprintf(&b, `<text x="%.1f" y="%d" textLength="%.1f" lengthAdjust="spacingAndGlyphs"%s>%s</text>`+"\n",
					x, y, float64(n)*charW, attrs, html.EscapeString(w.text))
			}
		}
	}
	b.WriteString("</g>\n</svg>\n")
	os.Stdout.WriteString(b.String())
}

type word struct {
	col  int // rune offset within the segment
	text string
}

// words splits s on spaces, keeping each word's rune column.
func words(s string) []word {
	var out []word
	col, start := 0, -1
	var cur []rune
	for _, r := range s {
		if r == ' ' {
			if start >= 0 {
				out = append(out, word{start, string(cur)})
				start, cur = -1, cur[:0]
			}
		} else {
			if start < 0 {
				start = col
			}
			cur = append(cur, r)
		}
		col++
	}
	if start >= 0 {
		out = append(out, word{start, string(cur)})
	}
	return out
}
