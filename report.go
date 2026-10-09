package main

// What a finding is, and how it reaches the screen.

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

type Severity int

const (
	Note Severity = iota
	// Says is what the recording says about where and when it was made:
	// true, on the screen or in the header, and not a secret.
	Says
	// Carries is in the file and not on the screen, which is worse than
	// being on it: nobody who watched the recording saw it, the author
	// included. A line taken back with nothing in it anybody would mind is
	// here too.
	Carries
	// Keeps is a line taken back with a name in it, which the recording
	// kept, and two folders that give away they were made by one person.
	// From here up the exit code says something was found.
	Keeps
	// Exposes is a secret, or a password the keyboard recorded.
	Exposes
)

type Finding struct {
	Tag    string
	Weight Severity
	// Title has %s where the recording goes, so a finding that turns up
	// the same in ten recordings from one machine is said once, about ten
	// recordings, rather than ten times.
	Title  string
	Files  []string
	Lines  []string
	Advice string
}

type Report struct {
	Title    string
	Summary  []string
	Findings []Finding
	// Gaps are what could not be read, printed first, because a clean
	// report about half a folder is half a report.
	Gaps []string
}

func (r *Report) add(f Finding) {
	for i, had := range r.Findings {
		if had.Tag == f.Tag && had.Title == f.Title && had.Advice == f.Advice &&
			strings.Join(had.Lines, "\n") == strings.Join(f.Lines, "\n") {
			r.Findings[i].Files = append(r.Findings[i].Files, f.Files...)
			return
		}
	}
	r.Findings = append(r.Findings, f)
}

func (r *Report) worst() Severity {
	worst := Note
	for _, f := range r.Findings {
		worst = max(worst, f.Weight)
	}
	return worst
}

const (
	indent = "              "
	wrap   = 62
)

func (r *Report) render(out io.Writer) {
	fmt.Fprintf(out, "\n  %s\n  %s\n\n", r.Title, strings.Repeat("=", min(runes(r.Title), wrap+14)))
	for _, line := range r.Summary {
		fmt.Fprintf(out, "  %s\n", line)
	}
	if len(r.Summary) > 0 {
		fmt.Fprintln(out)
	}
	for _, g := range r.Gaps {
		block(out, "UNREAD ", "Something here could not be read", nil, g)
	}
	// Loudest first, keeping the order within a level, so somebody who
	// reads the first screen and stops has read the part that mattered.
	sorted := make([]Finding, len(r.Findings))
	copy(sorted, r.Findings)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Weight > sorted[j].Weight })
	// Advice is said once. A folder of ten demos made the same way has the
	// same problem ten times, and the tenth paragraph about it is noise.
	said := map[string]bool{}
	for _, f := range sorted {
		advice := f.Advice
		if said[advice] {
			advice = ""
		}
		said[f.Advice] = true
		block(out, f.Tag, f.title(), f.lines(), advice)
	}
	fmt.Fprintln(out, "    Nothing was changed, and nothing was sent anywhere.")
	fmt.Fprintln(out)
}

func block(out io.Writer, tag, title string, lines []string, advice string) {
	head := wrapText(title, wrap+10)
	if len(head) == 0 {
		head = []string{""}
	}
	fmt.Fprintf(out, "    [%s] %s\n", tag, head[0])
	for _, line := range head[1:] {
		fmt.Fprintf(out, "%s%s\n", indent, line)
	}
	for _, line := range lines {
		if line = strings.TrimRight(line, " "); line == "" {
			fmt.Fprintln(out)
			continue
		}
		fmt.Fprintf(out, "%s%s\n", indent, line)
	}
	if advice != "" {
		if len(lines) > 0 {
			fmt.Fprintln(out)
		}
		for _, line := range wrapText(advice, wrap) {
			fmt.Fprintf(out, "%s%s\n", indent, line)
		}
	}
	fmt.Fprintln(out)
}

// wrapText counts letters rather than bytes, so a line with a mask or an
// accent in it wraps where it looks like it should.
func wrapText(s string, width int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	line := words[0]
	for _, w := range words[1:] {
		if runes(line)+1+runes(w) > width {
			lines = append(lines, line)
			line = w
			continue
		}
		line += " " + w
	}
	return append(lines, line)
}

func runes(s string) int { return len([]rune(s)) }

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%s %s", commas(n), many)
}

func commas(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// atMost keeps a list to a screen. It counts entries rather than lines:
// an entry that runs to two lines is still one thing found.
func atMost(entries [][]string, n int) []string {
	var lines []string
	for i, e := range entries {
		if i == n {
			lines = append(lines, fmt.Sprintf("... and %d more", len(entries)-n))
			break
		}
		lines = append(lines, e...)
	}
	return lines
}

// clip cuts a line down to something that fits, counting letters.
func clip(s string, width int) string {
	s = strings.Join(strings.Fields(s), " ")
	if runes(s) > width {
		s = string([]rune(s)[:width-3]) + "..."
	}
	return s
}

func seconds(at float64) string {
	switch {
	case at < 60:
		return fmt.Sprintf("%.1fs", at)
	case at < 3600:
		return fmt.Sprintf("%dm%02ds", int(at)/60, int(at)%60)
	}
	return fmt.Sprintf("%dh%02dm", int(at)/3600, int(at)%3600/60)
}

func (f Finding) title() string {
	if !strings.Contains(f.Title, "%s") {
		return f.Title
	}
	if len(f.Files) == 1 {
		return fmt.Sprintf(f.Title, f.Files[0])
	}
	return fmt.Sprintf(f.Title, plural(len(f.Files), "recording", "recordings"))
}

func (f Finding) lines() []string {
	if len(f.Files) < 2 {
		return f.Lines
	}
	var names [][]string
	for _, file := range f.Files {
		names = append(names, []string{"  " + file})
	}
	lines := append([]string{"the same in each of:"}, atMost(names, 6)...)
	return append(append(lines, ""), f.Lines...)
}

// some is plural for a title, where "kept a password" reads and "kept 1
// password" reads like a form.
func some(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return commas(n) + " " + many
}
