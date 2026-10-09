package main

// Playing a recording back and noting what it shows.

import (
	"encoding/base64"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Outtake is something that was on the screen and did not stay there:
// typed and deleted, typed and abandoned with ctrl-c, or typed while a VHS
// tape had hidden the recording.
type Outtake struct {
	Text string
	At   float64
	How  string
}

// Hidden is something typed at a prompt that did not show it, which the
// recording has anyway because it was recording the keyboard.
type Hidden struct {
	Text   string
	At     float64
	Prompt string
}

// Unseen is something the file carries that no player draws: a window
// title, the directory the shell reported, a header field.
type Unseen struct {
	What  string
	Value string
	At    float64
	// Header is set for what the file says about itself rather than what
	// the session sent, which has no moment in the playback.
	Header bool
}

type stroke struct {
	at float64
	r  rune
}

type Reading struct {
	Rec      *Recording
	Outtakes []Outtake
	Hidden   []Hidden
	Unseen   []Unseen
	Lines    []seen
	Strokes  []stroke

	screen []seen
}

func read(rec *Recording) (rd *Reading) {
	rd = &Reading{Rec: rec}
	for _, f := range rec.About {
		rd.Unseen = append(rd.Unseen, Unseen{What: f.Name, Value: f.Value, Header: true})
	}
	// A recording that trips the player up is reported and the rest of the
	// folder is still read. The first run over public recordings found one
	// within seconds, and a crash would have hidden every other report
	// behind it.
	defer func() {
		if p := recover(); p != nil {
			rec.Gaps = append(rec.Gaps, fmt.Sprintf("%s: playing it back failed (%v), so only part of it was read", rec.Path, p))
		}
	}()
	switch {
	case rec.Tape != nil:
		rd.readTape(rec.Tape)
	case len(rec.Frames) > 0:
		rd.readFrames(rec.Frames)
		rd.dropWhatCameBack()
	default:
		rd.play(rec)
		rd.dropWhatCameBack()
	}
	return rd
}

// dropWhatCameBack lets go of a deleted line that is on the screen again
// afterwards, because it was not taken back. Either it was retyped and run,
// or it was typed into the input box of a program that empties the box on
// enter and prints what was sent above it, which is how coding agents and
// chat clients work. The first run over public recordings reported dozens
// of messages to a coding agent as deleted.
//
// The comparison ignores spaces altogether, because the program printing
// the message back wraps it at its own width, not the input box's.
//
// A line that comes back nearly the same goes too: that is a typo being
// fixed, "giglet" retyped as "figlet", and the line that ran is the one
// that matters. Nearly the same means most of its three letter pieces turn
// up in one later line.
func (rd *Reading) dropWhatCameBack() {
	squeeze := func(s string) string { return strings.Join(strings.Fields(s), "") }
	kept := rd.Outtakes[:0]
	for _, o := range rd.Outtakes {
		needle := squeeze(o.Text)
		pieces := trigrams(strings.ToLower(needle))
		var later strings.Builder
		back := false
		for _, l := range rd.Lines {
			if l.at < o.At {
				continue
			}
			line := squeeze(l.text)
			later.WriteString(line)
			if len(pieces) >= 3 && overlap(pieces, strings.ToLower(line)) >= 0.6 {
				back = true
				break
			}
		}
		if !back && !strings.Contains(later.String(), needle) {
			kept = append(kept, o)
		}
	}
	rd.Outtakes = kept
}

func trigrams(s string) []string {
	r := []rune(s)
	var out []string
	for i := 0; i+3 <= len(r); i++ {
		out = append(out, string(r[i:i+3]))
	}
	return out
}

func overlap(pieces []string, in string) float64 {
	n := 0
	for _, p := range pieces {
		if strings.Contains(in, p) {
			n++
		}
	}
	return float64(n) / float64(len(pieces))
}

var escapes = regexp.MustCompile("\x1b\\[[0-?]*[ -/]*[@-~]|\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)?|\x1b[P_^X][^\x1b]*(?:\x1b\\\\)?|\x1b[ -/]*[0-~]")

// visible is what a burst of output draws, escape sequences taken out.
func visible(s string) string {
	s = escapes.ReplaceAllString(s, "")
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// press is a key as the keyboard sent it, with the screen as it stood: the
// line the cursor was on, which for a password is the question being
// answered, and whether a full screen program like vim had the keyboard,
// where keys are commands and are never echoed.
type press struct {
	at     float64
	r      rune
	alt    bool
	prompt string
}

func (rd *Reading) play(rec *Recording) {
	t := newTerminal(rec.Cols, rec.Rows)
	keyboard := rec.hasInput()
	window := echoWindow(rec.Events)
	lastKey := -1.0
	var outputs []Event
	var presses []press
	for _, e := range rec.Events {
		switch e.Kind {
		case 'i':
			lastKey = e.At
			presses = append(presses, pressesOf(e, t.alternate, strings.TrimSpace(t.rowText(t.y)))...)
			if strings.ContainsRune(e.Sent, 0x03) && !t.alternate {
				// zsh answers ctrl-c with a new line and no ^C, so from
				// the screen alone an abandoned line looks like one that
				// ran. The keyboard, when it is recorded, says which.
				t.abandon()
			}
		case 'r':
			if c, r, ok := strings.Cut(e.Sent, "x"); ok {
				cols, _ := strconv.Atoi(c)
				rows, _ := strconv.Atoi(r)
				t.resize(cols, rows)
			}
		case 'o':
			outputs = append(outputs, e)
			shown := []rune(visible(e.Sent))
			// With the keyboard recorded, an echo is what comes back within
			// a moment of a key. Without it, an echo has to be recognised
			// by its shape.
			typed := looksTyped(e.Sent, shown)
			if keyboard {
				typed = lastKey >= 0 && e.At-lastKey <= window && len(shown) <= 4
			}
			t.feed(e.At, e.Sent, typed)
			if typed && !keyboard {
				rd.echo(e, shown)
			}
		}
	}
	t.finish()
	rd.Lines = t.lines
	rd.outtakes(t)
	rd.titles(t.oscs)
	if keyboard {
		rd.hidden(presses, outputs, window)
		for _, p := range presses {
			if !p.alt {
				rd.Strokes = append(rd.Strokes, stroke{p.at, p.r})
			}
		}
	}
}

// echoWindow is how long after a key its echo can take to arrive. On the
// machine itself it is a millisecond; over ssh it is the round trip, which
// can be a quarter of a second, and a fixed window short enough for the
// first reads every key typed over the second as never echoed.
func echoWindow(events []Event) float64 {
	var delays []float64
	for i, e := range events {
		if e.Kind != 'i' {
			continue
		}
		for _, next := range events[i+1:] {
			if next.At-e.At > 1.5 {
				break
			}
			if next.Kind == 'o' {
				delays = append(delays, next.At-e.At)
				break
			}
		}
	}
	if len(delays) == 0 {
		return 0.1
	}
	sort.Float64s(delays)
	return min(1.0, max(0.1, 4*delays[len(delays)/2]+0.05))
}

// pressesOf splits what the keyboard sent in one read into keys. A read of
// several printable characters at once is a paste, not typing, and is
// kept as one break in the rhythm rather than as keys pressed together.
func pressesOf(e Event, alt bool, prompt string) []press {
	at := func(r rune) press { return press{at: e.At, r: r, alt: alt, prompt: prompt} }
	if strings.HasPrefix(e.Sent, "\x1b") {
		return []press{at(0)}
	}
	printable := 0
	for _, r := range e.Sent {
		if unicode.IsPrint(r) {
			printable++
		}
	}
	if printable > 1 {
		return []press{at(0)}
	}
	var out []press
	for _, r := range e.Sent {
		switch {
		case r == 0x7f || r == '\b':
			out = append(out, at('\b'))
		case r == '\r' || r == '\n':
			out = append(out, at('\r'))
		case unicode.IsPrint(r):
			out = append(out, at(r))
		default:
			out = append(out, at(0))
		}
	}
	return out
}

// looksTyped says whether a burst of output is the echo of a key: one
// character, or two when zsh redraws the word it belongs to, written where
// the cursor already was. The last part is what tells typing from the
// spinners and counters a full screen program updates a character at a
// time: they move the cursor first. The first run over public recordings
// reported a coding agent's spinner as five hundred deleted lines.
func looksTyped(sent string, shown []rune) bool {
	if isErase(sent) {
		return true
	}
	if len(shown) == 0 || len(shown) > 2 {
		return false
	}
	rest := styling.ReplaceAllString(sent, "")
	if strings.ContainsAny(rest, "\x1b\r\n") {
		return false
	}
	if strings.Contains(rest, "\b") {
		return strings.HasPrefix(rest, "\b") && len(shown) == 2 && strings.Count(rest, "\b") == 1
	}
	return true
}

// styling is colour and the like, which a shell that highlights as you
// type wraps around each echo, and the clear to the end of the line some
// shells send after it. Neither moves the cursor.
var styling = regexp.MustCompile(`\x1b\[[0-9;:]*[mK]|\x1b\[\?25[hl]`)

// isErase says whether a burst is the shell rubbing a character out: a
// backspace and a clear, or a backspace, a space and a backspace.
func isErase(sent string) bool {
	return strings.HasPrefix(sent, "\b") && (strings.Contains(sent, "\x1b[K") || strings.Contains(sent, "\b \b") || sent == "\b")
}

// echo turns a typed burst into the keystroke that caused it.
func (rd *Reading) echo(e Event, shown []rune) {
	switch {
	case isErase(e.Sent):
		rd.Strokes = append(rd.Strokes, stroke{e.At, '\b'})
	case len(shown) > 0:
		rd.Strokes = append(rd.Strokes, stroke{e.At, shown[len(shown)-1]})
	}
}

// outtakes gathers the characters that were typed and then rubbed out
// before the line was ever run. A burst is the set of characters one act
// of deleting took: a ctrl-u takes the line at once, backspaces take it a
// character at a time, and a pause of a few seconds starts a new act.
func (rd *Reading) outtakes(t *terminal) {
	type key struct{ row, epoch int }
	groups := map[key][]loss{}
	var order []key
	for _, l := range t.losses {
		if !l.unsent || l.alt || l.how == clearedKind {
			continue
		}
		k := key{l.row, l.epoch}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], l)
	}
	for _, k := range order {
		ls := groups[k]
		sort.SliceStable(ls, func(i, j int) bool { return ls[i].at < ls[j].at })
		start := 0
		for i := 1; i <= len(ls); i++ {
			if i < len(ls) && ls[i].at-ls[i-1].at < 3 {
				continue
			}
			if o, ok := takenBack(ls[start:i]); ok {
				rd.Outtakes = append(rd.Outtakes, o)
			}
			start = i
		}
	}
	for _, a := range t.abandoned {
		if len([]rune(a.text)) >= minOuttake && wordy(a.text) {
			rd.Outtakes = append(rd.Outtakes, Outtake{Text: a.text, At: a.at, How: "typed and abandoned with ctrl-c"})
		}
	}
	sort.SliceStable(rd.Outtakes, func(i, j int) bool { return rd.Outtakes[i].At < rd.Outtakes[j].At })
}

// takenBack turns one act of deleting into the text it deleted. What lies
// between the first and the last typed character is the line: the prompt
// to the left of it was never typed, and the letters tab completion put in
// the middle were not typed either but were as much part of the command.
// Keeping only the typed ones turned "cat sample.txt" into "ca sam", which
// then never matched the same command printed again a moment later.
func takenBack(burst []loss) (Outtake, bool) {
	from, to := -1, -1
	gone := 0.0
	for _, l := range burst {
		if !l.typed {
			continue
		}
		// The moment it went is when the last typed character went. The
		// burst can start earlier, with zsh taking its right-hand prompt
		// away as the line grew into it.
		gone = math.Max(gone, l.at)
		if from < 0 || l.col < from {
			from = l.col
		}
		to = max(to, l.col)
	}
	if from < 0 {
		return Outtake{}, false
	}
	// The last thing lost from a column is what was in it when the line
	// went, which is the line as it stood, typos already fixed.
	cols := map[int]rune{}
	for _, l := range burst {
		if l.col >= from && l.col <= to {
			cols[l.col] = l.r
		}
	}
	var b strings.Builder
	for c := from; c <= to; c++ {
		if r, ok := cols[c]; ok {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	text := strings.Join(strings.Fields(b.String()), " ")
	if len([]rune(text)) < minOuttake || !wordy(text) {
		return Outtake{}, false
	}
	return Outtake{Text: text, At: gone, How: "typed and deleted before it was run"}, true
}

// wordy says whether something reads like words: three letters at least,
// and letters and digits for a good part of it. A row of progress blocks
// typed out one at a time and wiped, the asterisks a password prompt
// echoes, a game board redrawn, are not lines somebody took back.
func wordy(s string) bool {
	letters, alnum, all := 0, 0, 0
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			continue
		case unicode.IsLetter(r):
			letters++
			alnum++
		case unicode.IsDigit(r):
			alnum++
		}
		all++
	}
	return letters >= 3 && alnum*10 >= all*4
}

// minOuttake is the shortest deletion worth listing. Shorter is a typo or
// half a word being fixed, which everybody does all the time and nobody
// needs telling about: four letters let through hundreds of "icio" and
// "ocio" from somebody fixing their spelling. A secret shorter than this
// is not much of one.
const minOuttake = 6

// titles reads the operating system commands the shell sent, which set the
// window title, report the directory and host, put text on the clipboard,
// or tell the terminal what command is about to run. A player draws none
// of them, and shells send most of them before every prompt.
func (rd *Reading) titles(oscs []osc) {
	seenBefore := map[string]bool{}
	note := func(what, value string, at float64) {
		value = strings.TrimSpace(value)
		if value == "" || seenBefore[what+"\x00"+value] {
			return
		}
		seenBefore[what+"\x00"+value] = true
		rd.Unseen = append(rd.Unseen, Unseen{What: what, Value: value, At: at})
	}
	for _, o := range oscs {
		switch o.code {
		case 0, 2:
			note("window title", o.payload, o.at)
		case 1:
			note("icon title", o.payload, o.at)
		case 7:
			note("directory reported to the terminal", o.payload, o.at)
		case 1337:
			name, value, _ := strings.Cut(o.payload, "=")
			switch name {
			case "RemoteHost", "CurrentDir":
				note("iTerm2 "+name, value, o.at)
			case "SetUserVar":
				k, v, _ := strings.Cut(value, "=")
				if decoded, err := base64.StdEncoding.DecodeString(v); err == nil {
					note("iTerm2 variable "+k, string(decoded), o.at)
				}
			}
		case 633:
			kind, value, _ := strings.Cut(o.payload, ";")
			switch kind {
			case "E":
				line, _, _ := strings.Cut(value, ";")
				note("command reported to VS Code", unescape633(line), o.at)
			case "P":
				note("property reported to VS Code", value, o.at)
			}
		case 52:
			_, encoded, _ := strings.Cut(o.payload, ";")
			if decoded, err := base64.StdEncoding.DecodeString(encoded); err == nil && len(decoded) > 0 {
				note("copied to the clipboard", string(decoded), o.at)
			}
		case 9, 777:
			note("desktop notification", o.payload, o.at)
		}
	}
}

func unescape633(s string) string {
	return strings.NewReplacer(`\x3b`, ";", `\x5c`, `\`, `\x0a`, "\n").Replace(s)
}

// hidden finds keystrokes that never came back as output: typed at a
// prompt that switches echo off, which is every password prompt there is.
// Recording the keyboard records them anyway, and asciinema's own help
// says so in a sentence nobody reads twice.
func (rd *Reading) hidden(presses []press, outputs []Event, window float64) {
	next := 0
	echoed := func(at float64, r rune) bool {
		for next < len(outputs) && outputs[next].At < at-0.005 {
			next++
		}
		for i := next; i < len(outputs) && outputs[i].At <= at+window; i++ {
			if strings.ContainsRune(outputs[i].Sent, r) {
				return true
			}
		}
		return false
	}
	var line []rune
	quiet, start, prompt := 0, 0.0, ""
	for _, p := range presses {
		switch {
		case p.alt || p.r == 0:
			// vim's keys are commands and an arrow moves the cursor, so
			// neither is echoed, and neither is part of a line being typed.
			line, quiet = nil, 0
		case p.r == '\r':
			if quiet >= 3 && quiet*2 >= len(line) {
				rd.Hidden = append(rd.Hidden, Hidden{Text: string(line), At: start, Prompt: prompt})
			}
			line, quiet = nil, 0
		case p.r == '\b':
			if len(line) > 0 {
				line = line[:len(line)-1]
			}
		default:
			if len(line) == 0 {
				start, prompt = p.at, p.prompt
			}
			line = append(line, p.r)
			if !echoed(p.at, p.r) {
				quiet++
			}
		}
	}
}

// readFrames does for an animated SVG what play does for a recording: a
// row that is a shorter version of what the same row said a frame earlier
// lost its end, and a row that grew by a character between frames was
// being typed.
func (rd *Reading) readFrames(frames []Frame) {
	seenText := map[string]bool{}
	// A line typed a character a frame is filed once, at its longest, the
	// way the terminal files it, rather than once per frame with a longer
	// piece of the same password each time.
	lastAt := map[float64]int{}
	for i, f := range frames {
		for _, row := range f.Rows {
			if row.Text == "" || seenText[row.Text] {
				continue
			}
			seenText[row.Text] = true
			if line, ok := strings.CutSuffix(row.Text, "^C"); ok {
				// A picture has no keyboard to ask, so the ^C on the end is
				// the only sign, and the prompt is whatever ends in the last
				// $, % or # before the typing.
				if cut := strings.LastIndexAny(line, "$%#>"); cut >= 0 {
					line = line[cut+1:]
				}
				if line = strings.TrimSpace(line); len([]rune(line)) >= minOuttake {
					rd.Outtakes = append(rd.Outtakes, Outtake{Text: line, At: f.At, How: "typed and abandoned with ctrl-c"})
				}
			}
			if at, ok := lastAt[row.Y]; ok && grew(rd.Lines[at].text, row.Text) {
				rd.Lines[at].text = row.Text
				continue
			}
			lastAt[row.Y] = len(rd.Lines)
			rd.Lines = append(rd.Lines, seen{text: row.Text, at: f.At})
		}
		if i == 0 {
			continue
		}
		before := map[float64]string{}
		for _, row := range frames[i-1].Rows {
			before[row.Y] = row.Text
		}
		now := map[string]bool{}
		for _, row := range f.Rows {
			now[row.Text] = true
		}
		for _, row := range f.Rows {
			old, ok := before[row.Y]
			if !ok || old == row.Text {
				continue
			}
			switch {
			case now[old]:
				// The line is still on the screen, a row higher: the
				// screen scrolled, and a new prompt landed where a command
				// had been. That is not the command being deleted.
			case strings.HasPrefix(old, row.Text) && strings.TrimSpace(row.Text) != "":
				gone := strings.TrimSpace(old[len(row.Text):])
				if len([]rune(gone)) >= minOuttake {
					rd.Outtakes = append(rd.Outtakes, Outtake{Text: gone, At: f.At, How: "on screen in one frame and deleted in the next"})
				}
			case strings.HasPrefix(row.Text, old) && len([]rune(row.Text))-len([]rune(old)) <= 2:
				added := []rune(row.Text[len(old):])
				rd.Strokes = append(rd.Strokes, stroke{f.At, added[len(added)-1]})
			}
		}
	}
}

func (rd *Reading) readTape(t *Tape) {
	for _, l := range t.Typed {
		rd.Lines = append(rd.Lines, seen{text: l.Text, at: float64(l.Line)})
		if l.Hidden {
			rd.Outtakes = append(rd.Outtakes, Outtake{Text: l.Text, At: float64(l.Line),
				How: "typed between Hide and Show"})
		}
	}
	for _, l := range t.Erased {
		if len([]rune(l.Text)) >= minOuttake {
			rd.Outtakes = append(rd.Outtakes, Outtake{Text: l.Text, At: float64(l.Line),
				How: "typed and backspaced"})
		}
	}
	for _, e := range t.Env {
		rd.Unseen = append(rd.Unseen, Unseen{What: "environment " + e.Name, Value: e.Value, Header: true})
	}
	for _, c := range t.Copied {
		rd.Unseen = append(rd.Unseen, Unseen{What: "copied to the clipboard", Value: c, Header: true})
	}
	for _, o := range t.Output {
		rd.Unseen = append(rd.Unseen, Unseen{What: "renders to", Value: o, Header: true})
	}
}
