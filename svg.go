package main

// Animated SVGs, the format people put in a README instead of a GIF.
//
// svg-term and termtosvg both make a film strip: every frame of the
// session laid out in one long picture, and a CSS animation sliding it
// past a window one frame at a time. Every frame is in the file, and every
// line of every frame is text, so the password that was on screen for a
// tenth of a second is as selectable, copyable and searchable as the
// command at the end. termtosvg advertises that as a feature.
//
// The frames are read back into screens: rows of text, in order, with the
// time each one was shown. From there the same reading as a recording
// applies.

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

func looksLikeSVG(body []byte) bool {
	head := body[:min(len(body), 2048)]
	return bytes.Contains(head, []byte("<svg")) && bytes.Contains(body, []byte("<text"))
}

type node struct {
	name  string
	attr  map[string]string
	text  string
	kids  []*node
	under *node
}

func parseXML(body []byte) (*node, error) {
	d := xml.NewDecoder(bytes.NewReader(body))
	d.Strict = false
	root := &node{name: "#root", attr: map[string]string{}}
	at := root
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			return root, nil
		}
		if err != nil {
			return root, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &node{name: t.Name.Local, attr: map[string]string{}, under: at}
			for _, a := range t.Attr {
				n.attr[a.Name.Local] = a.Value
			}
			at.kids = append(at.kids, n)
			at = n
		case xml.EndElement:
			if at.under != nil {
				at = at.under
			}
		case xml.CharData:
			at.text += string(t)
		}
	}
}

func (n *node) all(name string, into *[]*node) {
	if n.name == name {
		*into = append(*into, n)
	}
	for _, k := range n.kids {
		k.all(name, into)
	}
}

func (n *node) num(key string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSuffix(n.attr[key], "px"), 64)
	return v
}

// textOf is everything written inside a text element, tspans included.
func (n *node) textOf() string {
	var b strings.Builder
	b.WriteString(n.text)
	for _, k := range n.kids {
		b.WriteString(k.textOf())
	}
	return b.String()
}

type piece struct {
	x, y float64
	text string
}

var (
	keyframeStep = regexp.MustCompile(`([\d.]+)%\s*\{\s*transform:\s*translate([XY])\((-?[\d.]+)(?:px)?\)`)
	cssDuration  = regexp.MustCompile(`animation-duration:\s*([\d.]+)(m?s)`)
	varDuration  = regexp.MustCompile(`--animation-duration:\s*([\d.]+)ms`)
)

func readSVG(path string, body []byte) (*Recording, error) {
	root, err := parseXML(body)
	rec := &Recording{Path: path, Format: "animated SVG"}
	if err != nil {
		rec.Gaps = append(rec.Gaps, fmt.Sprintf("%s: %v, so only what came before it was read", path, err))
	}

	var texts []*node
	root.all("text", &texts)
	width := charWidth(texts)

	// A definition is a line drawn once and placed in many frames. Both
	// generators keep their lines in defs and point at them with use.
	lines := map[string][]piece{}
	var defs []*node
	root.all("defs", &defs)
	for _, d := range defs {
		for _, k := range d.kids {
			if id := k.attr["id"]; id != "" {
				var ts []*node
				k.all("text", &ts)
				for _, t := range ts {
					lines[id] = append(lines[id], piece{x: t.num("x"), y: t.num("y"), text: t.textOf()})
				}
			}
		}
	}

	strip := filmStrip(root)
	if strip == nil {
		// Not a film strip: a still picture of a terminal. Its text is
		// still its text.
		rec.Frames = []Frame{{Rows: rowsOf(collect(root, lines), width)}}
		return rec, nil
	}

	times := frameTimes(body, len(strip.kids))
	vertical := isVertical(body)
	for i, f := range strip.kids {
		ps := collect(f, lines)
		if vertical && len(ps) > 0 {
			// termtosvg stacks its frames down the page, so each frame's
			// rows sit at its own offset and have to be brought back to
			// the top.
			top := math.Inf(1)
			for _, p := range ps {
				top = math.Min(top, p.y)
			}
			step := frameStride(body)
			if step > 0 {
				base := math.Floor(top/step) * step
				for j := range ps {
					ps[j].y -= base
				}
			}
		}
		rec.Frames = append(rec.Frames, Frame{At: times[i], Rows: rowsOf(ps, width)})
	}
	return rec, nil
}

// filmStrip is the element whose children are the frames: the one with the
// most children that each draw text.
func filmStrip(n *node) *node {
	var best *node
	most := 1
	var walk func(*node)
	walk = func(n *node) {
		frames := 0
		for _, k := range n.kids {
			var t, u []*node
			k.all("text", &t)
			k.all("use", &u)
			if len(t)+len(u) > 0 {
				frames++
			}
		}
		if frames > most && n.name != "defs" {
			best, most = n, frames
		}
		for _, k := range n.kids {
			if k.name != "defs" {
				walk(k)
			}
		}
	}
	walk(n)
	return best
}

// collect lists every piece of text a frame draws, its own and the lines
// it borrows from defs, at the place it puts them.
func collect(n *node, lines map[string][]piece) []piece {
	var out []piece
	var walk func(*node, float64, float64)
	walk = func(n *node, dx, dy float64) {
		switch n.name {
		case "defs", "style", "symbol":
			return
		case "text":
			out = append(out, piece{x: dx + n.num("x"), y: dy + n.num("y"), text: n.textOf()})
			return
		case "use":
			id := strings.TrimPrefix(n.attr["href"], "#")
			for _, p := range lines[id] {
				out = append(out, piece{x: dx + n.num("x") + p.x, y: dy + n.num("y") + p.y, text: p.text})
			}
			return
		}
		for _, k := range n.kids {
			walk(k, dx, dy)
		}
	}
	walk(n, 0, 0)
	return out
}

// charWidth works out how wide one character is in the picture's units,
// from text that says how long it is, or failing that from two pieces of
// text that sit next to each other.
func charWidth(texts []*node) float64 {
	for _, t := range texts {
		if l := t.num("textLength"); l > 0 {
			if n := len([]rune(t.textOf())); n > 0 {
				return l / float64(n)
			}
		}
	}
	// Neighbours are only neighbours within one line. svg-term draws every
	// line as its own symbol at the same y, so grouping by y alone laid a
	// hundred lines over each other and measured the gaps between pieces
	// of different lines, which came out as anything at all.
	//
	// The commonest width wins rather than the smallest or the middle one.
	// A space is never drawn, so the gap after a word is wider than the
	// word, and the middle came out too wide and ran words together; the
	// smallest is thrown by the odd piece that carries a trailing space.
	votes := map[float64]int{}
	type line struct {
		under *node
		y     float64
	}
	byLine := map[line][]*node{}
	for _, t := range texts {
		k := line{t.under, t.num("y")}
		byLine[k] = append(byLine[k], t)
	}
	for _, row := range byLine {
		sort.Slice(row, func(i, j int) bool { return row[i].num("x") < row[j].num("x") })
		for i := 1; i < len(row); i++ {
			n := len([]rune(row[i-1].textOf()))
			if n == 0 {
				continue
			}
			if w := (row[i].num("x") - row[i-1].num("x")) / float64(n); w > 0.3 {
				votes[math.Round(w*1000)/1000]++
			}
		}
	}
	best, most := 1.0, 0
	for w, n := range votes {
		if n > most || n == most && w < best {
			best, most = w, n
		}
	}
	return best
}

// rowsOf lays pieces out as rows of a screen, putting each piece at the
// column its position says rather than gluing them together: "$" and "cd"
// are two pieces with a space between them that is not in either.
func rowsOf(ps []piece, width float64) []Row {
	byY := map[float64][]piece{}
	var ys []float64
	for _, p := range ps {
		y := math.Round(p.y*100) / 100
		if _, ok := byY[y]; !ok {
			ys = append(ys, y)
		}
		byY[y] = append(byY[y], p)
	}
	sort.Float64s(ys)
	rows := make([]Row, 0, len(ys))
	for _, y := range ys {
		row := byY[y]
		sort.Slice(row, func(i, j int) bool { return row[i].x < row[j].x })
		var line []rune
		for _, p := range row {
			col := int(math.Round(p.x / width))
			for len(line) < col {
				line = append(line, ' ')
			}
			for i, r := range []rune(p.text) {
				if col+i < len(line) {
					line[col+i] = r
				} else {
					line = append(line, r)
				}
			}
		}
		rows = append(rows, Row{Y: y, Text: strings.TrimRight(string(line), " ")})
	}
	return rows
}

// frameTimes reads when each frame is shown from the animation: a
// percentage of the whole for every step, and the length of the whole.
func frameTimes(body []byte, n int) []float64 {
	times := make([]float64, n)
	total := 0.0
	if m := cssDuration.FindSubmatch(body); m != nil {
		total, _ = strconv.ParseFloat(string(m[1]), 64)
		if string(m[2]) == "ms" {
			total /= 1000
		}
	} else if m := varDuration.FindSubmatch(body); m != nil {
		total, _ = strconv.ParseFloat(string(m[1]), 64)
		total /= 1000
	}
	steps := keyframeStep.FindAllSubmatch(body, -1)
	if total == 0 || len(steps) == 0 {
		for i := range times {
			times[i] = float64(i)
		}
		return times
	}
	// The step's distance says which frame it brings into view, which is
	// not always the next one: identical frames are drawn once and held.
	stride := 0.0
	for _, s := range steps {
		d := math.Abs(atof(string(s[3])))
		if d > 0 && (stride == 0 || d < stride) {
			stride = d
		}
	}
	for _, s := range steps {
		frame := int(math.Round(math.Abs(atof(string(s[3]))) / stride))
		if frame < n {
			times[frame] = atof(string(s[1])) / 100 * total
		}
	}
	for i := 1; i < n; i++ {
		if times[i] < times[i-1] {
			times[i] = times[i-1]
		}
	}
	return times
}

func isVertical(body []byte) bool {
	m := keyframeStep.FindSubmatch(body)
	return m != nil && string(m[2]) == "Y"
}

func frameStride(body []byte) float64 {
	stride := 0.0
	for _, s := range keyframeStep.FindAllSubmatch(body, -1) {
		d := math.Abs(atof(string(s[3])))
		if d > 0 && (stride == 0 || d < stride) {
			stride = d
		}
	}
	return stride
}

func atof(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}
