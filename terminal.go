package main

// A terminal, played back.
//
// The question this program asks is what was on the screen, when, and what
// stopped being on it. The raw output cannot answer that: readline erases a
// typo with a backspace and a clear-to-end-of-line, kills a whole line by
// jumping back to the prompt and clearing from there, and zsh rewrites
// characters with the same characters as you type. Only a screen that
// follows the escape sequences knows that "hunter2" was there at 12.4
// seconds and gone at 13.1.
//
// It is a small VT100 with the xterm additions shells actually use, not a
// renderer: no colours, no fonts, just which character is in which cell,
// when it got there, and whether it got there because somebody typed it.

import (
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type cell struct {
	r     rune
	at    float64
	typed bool
	epoch int
}

type lossKind int

const (
	erasedKind lossKind = iota
	replacedKind
	clearedKind
)

// loss is a character that stopped being on the screen without scrolling
// off it.
type loss struct {
	row, col int
	r        rune
	written  float64
	at       float64
	typed    bool
	how      lossKind
	// unsent is true when the character went before the line it was on was
	// finished with: deleted while still being typed, not cleared later.
	unsent bool
	epoch  int
	// alt is set for what vim, less and the like drew on the alternate
	// screen, where deleting text is editing a file, not taking a command
	// back.
	alt bool
}

type osc struct {
	code    int
	payload string
	at      float64
}

// seen is a line of text as it stood on the screen, with when it first did.
type seen struct {
	text string
	at   float64
	row  int
}

type terminal struct {
	cols, rows int
	grid       [][]cell
	saved      [][]cell
	alternate  bool
	epochs     []int
	nextEpoch  int
	x, y       int
	saveX      int
	saveY      int
	wrapNext   bool
	autowrap   bool
	insert     bool
	top        int
	bottom     int

	now    float64
	typing bool
	dirty  map[int]bool
	shown  map[int]string

	losses    []loss
	oscs      []osc
	lines     []seen
	abandoned []seen

	state   int
	params  []byte
	payload []byte
	pending []byte
}

const (
	ground = iota
	escape
	escapeSkip
	csi
	oscString
	ignoredString
	ignoredEscape
	oscEscape
)

func newTerminal(cols, rows int) *terminal {
	t := &terminal{autowrap: true, dirty: map[int]bool{}, shown: map[int]string{}}
	t.resize(cols, rows)
	return t
}

func (t *terminal) resize(cols, rows int) {
	if cols <= 0 || rows <= 0 || cols > 2000 || rows > 1000 {
		return
	}
	epochs := make([]int, rows)
	for y := range epochs {
		if y < len(t.epochs) {
			epochs[y] = t.epochs[y]
		} else {
			epochs[y] = t.fresh()
		}
	}
	// The main screen put away under vim is resized with the rest. A window
	// resized while vim was open left it at the old size, and coming back
	// from vim indexed rows that were no longer there.
	if t.grid == nil {
		t.grid = [][]cell{}
	}
	t.grid, t.saved = fit(t.grid, cols, rows), fit(t.saved, cols, rows)
	t.epochs, t.cols, t.rows = epochs, cols, rows
	t.top, t.bottom = 0, rows-1
	t.x, t.y = min(t.x, cols-1), min(t.y, rows-1)
}

func fit(grid [][]cell, cols, rows int) [][]cell {
	if grid == nil {
		return nil
	}
	out := make([][]cell, rows)
	for y := range out {
		out[y] = make([]cell, cols)
		if y < len(grid) {
			copy(out[y], grid[y])
		}
	}
	return out
}

func (t *terminal) fresh() int {
	t.nextEpoch++
	return t.nextEpoch
}

// feed plays one burst of output. typed says whether it is the echo of a
// keystroke, which the caller decides from the size of the burst and from
// the keyboard events around it.
func (t *terminal) feed(at float64, sent string, typed bool) {
	t.now, t.typing = at, typed
	if strings.HasPrefix(sent, "^C") {
		t.abandon()
	}
	b := append(t.pending, sent...)
	t.pending = nil
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		if r == utf8.RuneError && size == 1 && !utf8.FullRune(b) {
			// Half a character at the end of a burst: the rest comes in
			// the next one. Raw recordings split UTF-8 wherever the read
			// happened to end.
			t.pending = append([]byte(nil), b...)
			break
		}
		t.step(r)
		b = b[size:]
	}
	t.notice()
}

// abandon records what was typed on the current line when ctrl-c was
// pressed. The line stays on the screen with ^C after it, which reads as
// harmless, and it was never run, which is exactly why it was abandoned.
func (t *terminal) abandon() {
	var cols []rune
	row := t.grid[t.y]
	first := -1
	for x, c := range row {
		if c.typed && c.epoch == t.epochs[t.y] && c.r != 0 {
			if first < 0 {
				first = x
			}
			for len(cols) < x-first {
				cols = append(cols, ' ')
			}
			cols = append(cols, c.r)
		}
	}
	text := strings.TrimSpace(string(cols))
	if text == "" {
		return
	}
	// bash echoes the ^C and the keyboard, when it is recorded, has the
	// key itself: the same moment seen twice.
	if n := len(t.abandoned); n > 0 && t.abandoned[n-1].text == text && t.abandoned[n-1].row == t.y {
		return
	}
	t.abandoned = append(t.abandoned, seen{text: text, at: t.now, row: t.y})
}

func (t *terminal) step(r rune) {
	switch t.state {
	case escape:
		t.escape(r)
		return
	case escapeSkip:
		t.state = ground
		return
	case csi:
		if r >= 0x40 && r <= 0x7e {
			t.state = ground
			t.control(r, t.params)
			t.params = t.params[:0]
			return
		}
		if len(t.params) < 64 {
			t.params = append(t.params, byte(r))
		}
		return
	case oscString:
		switch r {
		case 0x07:
			t.endOSC()
		case 0x1b:
			t.state = oscEscape
		default:
			if len(t.payload) < 1<<20 {
				t.payload = utf8.AppendRune(t.payload, r)
			}
		}
		return
	case oscEscape:
		t.endOSC()
		if r != '\\' {
			t.step(r)
		}
		return
	case ignoredString:
		if r == 0x07 {
			t.state = ground
		} else if r == 0x1b {
			t.state = ignoredEscape
		}
		return
	case ignoredEscape:
		t.state = ground
		return
	}

	switch r {
	case 0x1b:
		t.state = escape
	case '\r':
		t.x, t.wrapNext = 0, false
	case '\n', 0x0b, 0x0c:
		t.linefeed()
	case '\b':
		if t.x > 0 {
			t.x--
		}
		t.wrapNext = false
	case '\t':
		t.x = min(t.cols-1, (t.x/8+1)*8)
	case 0x07, 0x00, 0x0e, 0x0f, 0x7f:
	default:
		if r < 0x20 || (r >= 0x80 && r < 0xa0) {
			return
		}
		t.put(r)
	}
}

func (t *terminal) escape(r rune) {
	t.state = ground
	switch r {
	case '[':
		t.state = csi
		t.params = t.params[:0]
	case ']':
		t.state = oscString
		t.payload = t.payload[:0]
	case 'P', '_', '^', 'X':
		t.state = ignoredString
	case '(', ')', '*', '+', '#', '%':
		t.state = escapeSkip
	case '7':
		t.saveX, t.saveY = t.x, t.y
	case '8':
		t.x, t.y = min(t.saveX, t.cols-1), min(t.saveY, t.rows-1)
	case 'D':
		t.linefeed()
	case 'E':
		t.x = 0
		t.linefeed()
	case 'M':
		if t.y == t.top {
			t.scrollDown(1)
		} else if t.y > 0 {
			t.y--
		}
	case 'c':
		t.clear(0, t.rows-1)
		t.x, t.y = 0, 0
	}
}

func (t *terminal) endOSC() {
	t.state = ground
	text := string(t.payload)
	num, rest, _ := strings.Cut(text, ";")
	code, err := strconv.Atoi(num)
	if err != nil {
		return
	}
	t.oscs = append(t.oscs, osc{code: code, payload: rest, at: t.now})
}

func (t *terminal) put(r rune) {
	if unicode.Is(unicode.Mn, r) {
		return
	}
	width := runeWidth(r)
	if t.wrapNext && t.autowrap {
		t.x = 0
		t.linefeed()
	}
	t.wrapNext = false
	if t.x+width > t.cols {
		if !t.autowrap {
			t.x = t.cols - width
		} else {
			t.x = 0
			t.linefeed()
		}
	}
	row := t.grid[t.y]
	if t.insert {
		t.shiftRight(t.x, width)
	}
	how := replacedKind
	if r == ' ' {
		// A space written over a character is how zsh deletes one.
		how = erasedKind
	}
	for i := 0; i < width; i++ {
		old := row[t.x+i]
		if i == 0 && old.r == r {
			// The same character written over itself is a redraw, not a
			// change. zsh does it on every keystroke.
			row[t.x].at, row[t.x].typed = old.at, old.typed || t.typing
			continue
		}
		t.lose(t.y, t.x+i, old, how)
		c := cell{r: r, at: t.now, typed: t.typing, epoch: t.epochs[t.y]}
		if i > 0 {
			c.r = 0
		}
		row[t.x+i] = c
	}
	t.dirty[t.y] = true
	t.x += width
	if t.x >= t.cols {
		t.x = t.cols - 1
		t.wrapNext = true
	}
}

// lose records a visible character leaving the screen.
func (t *terminal) lose(y, x int, old cell, how lossKind) {
	if old.r == 0 || old.r == ' ' {
		return
	}
	t.losses = append(t.losses, loss{
		row: y, col: x, r: old.r, written: old.at, at: t.now, typed: old.typed,
		how: how, unsent: old.epoch == t.epochs[y], epoch: old.epoch, alt: t.alternate,
	})
}

func (t *terminal) linefeed() {
	t.epochs[t.y] = t.fresh()
	if t.y == t.bottom {
		t.scrollUp(1)
		return
	}
	if t.y < t.rows-1 {
		t.y++
	}
}

// scrollUp moves the region up. What goes off the top is not lost: it is
// scrollback, which every player keeps showing.
func (t *terminal) scrollUp(n int) {
	for i := 0; i < n; i++ {
		t.capture(t.top)
		copy(t.grid[t.top:t.bottom], t.grid[t.top+1:t.bottom+1])
		copy(t.epochs[t.top:t.bottom], t.epochs[t.top+1:t.bottom+1])
		t.grid[t.bottom] = make([]cell, t.cols)
		t.epochs[t.bottom] = t.fresh()
	}
	t.everythingDirty()
}

func (t *terminal) scrollDown(n int) {
	for i := 0; i < n; i++ {
		for x := range t.grid[t.bottom] {
			t.lose(t.bottom, x, t.grid[t.bottom][x], erasedKind)
		}
		copy(t.grid[t.top+1:t.bottom+1], t.grid[t.top:t.bottom])
		copy(t.epochs[t.top+1:t.bottom+1], t.epochs[t.top:t.bottom])
		t.grid[t.top] = make([]cell, t.cols)
		t.epochs[t.top] = t.fresh()
	}
	t.everythingDirty()
}

func (t *terminal) everythingDirty() {
	for y := 0; y < t.rows; y++ {
		t.dirty[y] = true
	}
}

func (t *terminal) clear(from, to int) {
	for y := from; y <= to && y < t.rows; y++ {
		t.eraseCells(y, 0, t.cols, clearedKind)
		t.epochs[y] = t.fresh()
	}
}

func (t *terminal) eraseCells(y, from, to int, how lossKind) {
	if y < 0 || y >= t.rows {
		return
	}
	t.capture(y)
	for x := max(0, from); x < to && x < t.cols; x++ {
		t.lose(y, x, t.grid[y][x], how)
		t.grid[y][x] = cell{r: ' ', at: t.now, epoch: t.epochs[y]}
	}
	t.dirty[y] = true
}

func (t *terminal) shiftRight(from, n int) {
	row := t.grid[t.y]
	for x := t.cols - n; x < t.cols; x++ {
		t.lose(t.y, x, row[x], erasedKind)
	}
	copy(row[from+n:], row[from:t.cols-n])
	for x := from; x < from+n && x < t.cols; x++ {
		row[x] = cell{r: ' ', at: t.now, epoch: t.epochs[t.y]}
	}
}

func (t *terminal) control(final rune, raw []byte) {
	private := len(raw) > 0 && (raw[0] == '?' || raw[0] == '>' || raw[0] == '=')
	if private {
		raw = raw[1:]
	}
	args := params(string(raw))
	arg := func(i, def int) int {
		if i < len(args) && args[i] > 0 {
			return args[i]
		}
		return def
	}
	switch final {
	case 'A':
		t.y = max(t.top, t.y-arg(0, 1))
	case 'B', 'e':
		t.y = min(t.bottom, t.y+arg(0, 1))
	case 'C', 'a':
		t.x = min(t.cols-1, t.x+arg(0, 1))
	case 'D':
		t.x = max(0, t.x-arg(0, 1))
	case 'E':
		t.y, t.x = min(t.bottom, t.y+arg(0, 1)), 0
	case 'F':
		t.y, t.x = max(t.top, t.y-arg(0, 1)), 0
	case 'G', '`':
		t.x = min(t.cols-1, arg(0, 1)-1)
	case 'd':
		t.y = min(t.rows-1, arg(0, 1)-1)
	case 'H', 'f':
		t.y, t.x = min(t.rows-1, arg(0, 1)-1), min(t.cols-1, arg(1, 1)-1)
	case 'J':
		switch arg(0, 0) {
		case 0:
			t.eraseCells(t.y, t.x, t.cols, erasedKind)
			t.clear(t.y+1, t.rows-1)
		case 1:
			t.clear(0, t.y-1)
			t.eraseCells(t.y, 0, t.x+1, erasedKind)
		default:
			t.clear(0, t.rows-1)
		}
	case 'K':
		switch arg(0, 0) {
		case 0:
			t.eraseCells(t.y, t.x, t.cols, erasedKind)
		case 1:
			t.eraseCells(t.y, 0, t.x+1, erasedKind)
		default:
			t.eraseCells(t.y, 0, t.cols, erasedKind)
		}
	case 'X':
		t.eraseCells(t.y, t.x, t.x+arg(0, 1), erasedKind)
	case 'P':
		n := min(arg(0, 1), t.cols-t.x)
		row := t.grid[t.y]
		for x := t.x; x < t.x+n; x++ {
			t.lose(t.y, x, row[x], erasedKind)
		}
		copy(row[t.x:], row[t.x+n:])
		for x := t.cols - n; x < t.cols; x++ {
			row[x] = cell{r: ' ', at: t.now, epoch: t.epochs[t.y]}
		}
		t.dirty[t.y] = true
	case '@':
		t.shiftRight(t.x, min(arg(0, 1), t.cols-t.x))
		t.dirty[t.y] = true
	case 'L', 'M':
		if t.y < t.top || t.y > t.bottom {
			return
		}
		top := t.top
		t.top = t.y
		if final == 'L' {
			t.scrollDown(arg(0, 1))
		} else {
			for i := 0; i < arg(0, 1); i++ {
				t.eraseCells(t.y, 0, t.cols, erasedKind)
				t.scrollUp(1)
			}
		}
		t.top = top
	case 'S':
		t.scrollUp(arg(0, 1))
	case 'T':
		t.scrollDown(arg(0, 1))
	case 'r':
		top, bottom := arg(0, 1)-1, arg(1, t.rows)-1
		if top < bottom && bottom < t.rows {
			t.top, t.bottom = top, bottom
			t.x, t.y = 0, 0
		}
	case 's':
		t.saveX, t.saveY = t.x, t.y
	case 'u':
		t.x, t.y = min(t.saveX, t.cols-1), min(t.saveY, t.rows-1)
	case 'h', 'l':
		on := final == 'h'
		for _, a := range args {
			switch {
			case private && (a == 1049 || a == 1047 || a == 47):
				t.switchScreen(on)
			case private && a == 7:
				t.autowrap = on
			case !private && a == 4:
				t.insert = on
			}
		}
	}
}

// switchScreen moves to or from the alternate screen, which is where vim,
// less and htop draw. What they draw there is shown and then put away
// rather than erased, and the main screen comes back as it was.
func (t *terminal) switchScreen(on bool) {
	if on == t.alternate {
		return
	}
	t.everythingDirty()
	for y := 0; y < t.rows; y++ {
		t.capture(y)
	}
	if on {
		t.saved = t.grid
		t.grid = make([][]cell, t.rows)
		for y := range t.grid {
			t.grid[y] = make([]cell, t.cols)
			t.epochs[y] = t.fresh()
		}
	} else if t.saved != nil {
		t.grid = t.saved
		t.saved = nil
	}
	t.alternate = on
}

func params(s string) []int {
	if s == "" {
		return nil
	}
	var out []int
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ';' || r == ':' }) {
		n, _ := strconv.Atoi(p)
		out = append(out, n)
	}
	return out
}

func (t *terminal) rowText(y int) string {
	var b strings.Builder
	for _, c := range t.grid[y] {
		switch c.r {
		case 0:
			b.WriteByte(' ')
		default:
			b.WriteRune(c.r)
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// capture files a row's text as it stands, before something changes it.
func (t *terminal) capture(y int) {
	text := t.rowText(y)
	if text == "" || t.shown[y] == text {
		return
	}
	t.record(y, text)
}

// notice files the rows that changed during a burst. A line typed a
// character at a time is filed once, at its longest: each new state that
// only extends the last one replaces it.
func (t *terminal) notice() {
	rows := make([]int, 0, len(t.dirty))
	for y := range t.dirty {
		rows = append(rows, y)
	}
	sort.Ints(rows)
	for _, y := range rows {
		if y < t.rows {
			if text := t.rowText(y); text != "" && t.shown[y] != text {
				t.record(y, text)
			}
		}
		delete(t.dirty, y)
	}
}

func (t *terminal) record(y int, text string) {
	last := t.shown[y]
	t.shown[y] = text
	if last != "" && grew(last, text) {
		for i := len(t.lines) - 1; i >= 0 && i >= len(t.lines)-t.rows*2; i-- {
			if t.lines[i].row == y && t.lines[i].text == last {
				t.lines[i].text = text
				return
			}
		}
	}
	t.lines = append(t.lines, seen{text: text, at: t.now, row: y})
}

// grew says whether a line is an earlier one with something typed into it.
// Typing at the end is the easy case. A prompt with a clock on the right,
// which zsh draws with RPROMPT, is the other: each key goes in the middle
// and eats a space of the padding, so the line never gets longer and is
// never a prefix of the next. Compared with the padding squeezed out, the
// old line is the new one with characters inserted at one point.
func grew(old, new string) bool {
	a, b := strings.Join(strings.Fields(old), " "), strings.Join(strings.Fields(new), " ")
	if len(b) < len(a) {
		return false
	}
	prefix := 0
	for prefix < len(a) && a[prefix] == b[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(a)-prefix && a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}
	return prefix+suffix >= len(a)
}

// finish files whatever is still on the screen when the recording ends.
func (t *terminal) finish() {
	for y := 0; y < t.rows; y++ {
		t.dirty[y] = true
	}
	t.notice()
}

// runeWidth is how many cells a character takes. East Asian wide
// characters and most emoji take two; getting that wrong shifts every
// character after them on the line, and with them every erase.
func runeWidth(r rune) int {
	switch {
	case r >= 0x1100 && r <= 0x115f, r >= 0x2e80 && r <= 0xa4cf && r != 0x303f,
		r >= 0xac00 && r <= 0xd7a3, r >= 0xf900 && r <= 0xfaff, r >= 0xfe30 && r <= 0xfe4f,
		r >= 0xff00 && r <= 0xff60, r >= 0xffe0 && r <= 0xffe6, r >= 0x1f300 && r <= 0x1f64f,
		r >= 0x1f900 && r <= 0x1f9ff, r >= 0x20000 && r <= 0x3fffd:
		return 2
	}
	return 1
}
