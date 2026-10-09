package main

// Turning what was read into what is worth saying, and the words for it.

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type Study struct {
	Sources  []*Source
	Readings [][]*Reading
	reveal   bool
}

func study(sources []*Source, reveal bool) *Study {
	s := &Study{Sources: sources, reveal: reveal}
	for _, src := range sources {
		var rds []*Reading
		for _, rec := range src.Recordings {
			rds = append(rds, read(rec))
		}
		s.Readings = append(s.Readings, rds)
	}
	return s
}

func (s *Study) recordings() int {
	n := 0
	for _, rds := range s.Readings {
		n += len(rds)
	}
	return n
}

func (s *Study) writeUp() *Report {
	var names []string
	for _, src := range s.Sources {
		names = append(names, src.Name)
	}
	r := &Report{Title: "Outtakes: " + strings.Join(names, ", ")}
	for i, src := range s.Sources {
		r.Summary = append(r.Summary, s.summary(i)...)
		r.Gaps = append(r.Gaps, src.Gaps...)
		for _, rd := range s.Readings[i] {
			r.Gaps = append(r.Gaps, rd.Rec.Gaps...)
			s.recording(r, src, rd)
		}
	}
	if len(s.Sources) > 1 {
		s.compare(r)
	} else if len(s.Sources) == 1 {
		s.liveTyping(r, 0)
	}
	return r
}

func (s *Study) summary(i int) []string {
	formats := map[string]int{}
	keyboard := 0
	for _, rd := range s.Readings[i] {
		format, _, _ := strings.Cut(rd.Rec.Format, ",")
		if strings.HasPrefix(format, "asciicast") {
			format = "asciicast"
		}
		formats[format]++
		if rd.Rec.hasInput() {
			keyboard++
		}
	}
	if len(formats) == 0 {
		return []string{fmt.Sprintf("%s: no recordings found.", s.Sources[i].Name)}
	}
	names := make([]string, 0, len(formats))
	for f := range formats {
		names = append(names, f)
	}
	sort.Slice(names, func(a, b int) bool {
		if formats[names[a]] != formats[names[b]] {
			return formats[names[a]] > formats[names[b]]
		}
		return names[a] < names[b]
	})
	kinds := make([]string, 0, len(names))
	for _, f := range names {
		kinds = append(kinds, fmt.Sprintf("%d %s", formats[f], f))
	}
	line := fmt.Sprintf("%s: %s read (%s).", s.Sources[i].Name,
		plural(len(s.Readings[i]), "recording", "recordings"), strings.Join(kinds, ", "))
	lines := wrapText(line, wrap+12)
	if keyboard > 0 {
		lines = append(lines, wrapText(fmt.Sprintf("%s recorded the keyboard as well as the screen.",
			plural(keyboard, "of them", "of them")), wrap+12)...)
	}
	return lines
}

// label is how a recording is named in the report: its path inside the
// folder that was given, never the whole path, which would put the account
// name of whoever ran this into a report meant for pasting.
func label(src *Source, rec *Recording) string {
	if src.Root == "" {
		return filepath.Base(rec.Path)
	}
	if rel, err := filepath.Rel(src.Root, rec.Path); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.Base(rec.Path)
}

func (s *Study) scrub(text string) string {
	text = coverAll(text)
	if s.reveal {
		return text
	}
	return maskTellsIn(text)
}

func (s *Study) recording(r *Report, src *Source, rd *Reading) {
	name := label(src, rd.Rec)
	file := []string{name}
	reported := map[string]bool{}

	if len(rd.Hidden) > 0 {
		var lines []string
		for _, h := range rd.Hidden {
			reported[h.Text] = true
			prompt := "a prompt that showed nothing"
			if h.Prompt != "" {
				prompt = `"` + clip(s.scrub(h.Prompt), 40) + `"`
			}
			lines = append(lines, fmt.Sprintf("%-7s typed at %s", seconds(h.At), prompt))
		}
		r.add(Finding{
			Tag: "HIDDEN ", Weight: Exposes, Files: file,
			Title: "%s kept " + some(len(rd.Hidden), "a password", "passwords") + " the screen never showed",
			Lines: lines,
			Advice: "The prompt turned echo off, so nothing appeared and the playback looks clean. " +
				"The recording was saving the keyboard as well, which asciinema does with --stdin " +
				"(--capture-input in version 3, or session.capture_input in its config) and script " +
				"does with -I or -B, and every key is in the file as it was typed. Record without " +
				"keyboard capture, and if this file has been shared or published, change the password.",
		})
	}

	s.outtakes(r, rd, file, reported)
	s.secretsOnScreen(r, rd, file, reported)
	s.unseen(r, rd, file)
	s.tells(r, rd, file)
	s.clock(r, rd, file)
}

func (s *Study) outtakes(r *Report, rd *Reading, file []string, reported map[string]bool) {
	if len(rd.Outtakes) == 0 {
		return
	}
	var entries [][]string
	withSecret, withTells := 0, 0
	for _, o := range rd.Outtakes {
		found := secretsIn(o.Text)
		if len(tellsIn(o.Text)) > 0 {
			withTells++
		}
		for _, sec := range found {
			reported[sec.value] = true
		}
		when := seconds(o.At)
		if rd.Rec.Tape != nil {
			when = fmt.Sprintf("line %d", int(o.At))
		}
		head := fmt.Sprintf("%-7s %s", when, o.How)
		if len(found) > 0 {
			withSecret++
			head += " (" + detectors[found[0].Rule].Name + ")"
		}
		entries = append(entries, []string{head, "        " + clip(s.scrub(o.Text), wrap-8)})
	}
	// A line with a secret or a name in it is what this section is for. A
	// line with neither is reported and does not fail anything: half a
	// command somebody thought better of is the commonest outtake there is,
	// and the least likely to matter.
	f := Finding{Tag: "TYPED  ", Weight: Carries, Files: file, Lines: atMost(entries, 12)}
	if withTells > 0 {
		f.Weight = Keeps
	}
	f.Title = "%s kept " + some(len(rd.Outtakes), "a line", "lines") + " typed and taken back"
	if withSecret > 0 {
		f.Weight = Exposes
		f.Title += ", " + some(withSecret, "one with a secret in it", "with secrets in them")
	}
	switch {
	case rd.Rec.Tape != nil:
		f.Advice = "Hide keeps commands out of the GIF. The tape that made the GIF still has them " +
			"in plain text, and the tape usually sits in the repository right beside it."
	case len(rd.Rec.Frames) > 0:
		f.Advice = "Every frame of an animated SVG is in the file as text, including the ones that " +
			"were on the screen for a tenth of a second, and anybody who opens the file or " +
			"searches it finds them."
	default:
		f.Advice = "Rubbing a line out before pressing enter keeps it out of the shell's history. " +
			"It does not keep it out of a recording, which saved every key as it was typed and " +
			"every one as it was taken back: anybody can scrub the playback to the moment before."
	}
	r.add(f)
}

func (s *Study) secretsOnScreen(r *Report, rd *Reading, file []string, reported map[string]bool) {
	type place struct {
		secret Secret
		at     float64
		where  string
	}
	var found []place
	look := func(text, where string, at float64) {
		for _, sec := range secretsIn(text) {
			if alreadySaid(reported, sec.value) {
				continue
			}
			reported[sec.value] = true
			found = append(found, place{sec, at, where})
		}
	}
	for _, l := range screenText(rd) {
		look(l.text, "on the screen", l.at)
	}
	for _, u := range rd.Unseen {
		at := u.At
		if u.Header {
			at = -1
		}
		look(u.Value, "in the "+u.What+", which no player shows", at)
	}
	if rd.Rec.Tape != nil {
		for _, l := range rd.Rec.Tape.Typed {
			look(l.Text, fmt.Sprintf("typed by line %d of the tape", l.Line), -1)
		}
	}
	if len(found) == 0 {
		return
	}
	var entries [][]string
	for _, p := range found {
		head := detectors[p.secret.Rule].Name + ", " + p.where
		if p.at >= 0 && rd.Rec.Tape == nil {
			head = fmt.Sprintf("%-7s %s", seconds(p.at), head)
		}
		entries = append(entries, []string{head, "        " + clip(s.scrub(p.secret.Line), wrap-8)})
	}
	r.add(Finding{
		Tag:    "SECRET ",
		Weight: Exposes,
		Files:  file,
		Title:  "%s showed " + some(len(found), "a secret", "secrets"),
		Lines:  atMost(entries, 10),
		Advice: secretAdvice(len(found)),
	})
}

// screenText is what was on the screen, line by line. A line the terminal
// wrapped is joined back to the one after it, because a token that ran past
// the right edge is two halves that neither look like a token.
func screenText(rd *Reading) []seen {
	if rd.screen != nil {
		return rd.screen
	}
	cols := rd.Rec.Cols
	out := []seen{}
	for i := 0; i < len(rd.Lines); i++ {
		l := rd.Lines[i]
		for cols > 0 && runes(l.text) == cols && i+1 < len(rd.Lines) {
			i++
			l.text += rd.Lines[i].text
		}
		out = append(out, l)
	}
	rd.screen = out
	return out
}

// worthSaying picks the header fields that carry something. The command
// and the TERM are in nearly every recording and say nothing about anyone;
// a terminal's version string, a colour scheme and a log path do.
func worthSaying(u Unseen) bool {
	switch {
	case !u.Header || strings.HasPrefix(u.What, "window") || strings.HasPrefix(u.What, "icon") ||
		strings.HasPrefix(u.What, "directory") || strings.HasPrefix(u.What, "iTerm2") ||
		strings.Contains(u.What, "VS Code") || strings.Contains(u.What, "clipboard") ||
		strings.Contains(u.What, "notification"):
		return true
	case u.What == "terminal version", u.What == "colour scheme", u.What == "logged to":
		return true
	case strings.HasPrefix(u.What, "environment "):
		name := strings.TrimPrefix(u.What, "environment ")
		return name != "SHELL" && name != "TERM" && name != "LANG" && name != "COLORTERM"
	}
	return len(tellsIn(u.Value)) > 0
}

func (s *Study) unseen(r *Report, rd *Reading, file []string) {
	order := []string{}
	values := map[string][]string{}
	for _, u := range rd.Unseen {
		if !worthSaying(u) {
			continue
		}
		if _, ok := values[u.What]; !ok {
			order = append(order, u.What)
		}
		v := s.scrub(u.Value)
		switch {
		case u.What == "colour scheme":
			v = u.Value
		case !s.reveal && (strings.HasPrefix(u.What, "environment ") || strings.Contains(u.What, "clipboard")):
			// A variable or a clipboard can hold anything at all, so the
			// whole value is masked rather than only the tells in it.
			v = maskText(coverAll(u.Value))
		}
		v = clip(v, wrap-4)
		values[u.What] = append(values[u.What], v)
	}
	if len(order) == 0 {
		return
	}
	var lines []string
	for _, what := range order {
		vs := values[what]
		head := what
		if len(vs) > 1 {
			head += fmt.Sprintf(", %d values", len(vs))
		}
		lines = append(lines, head)
		for i, v := range vs {
			if i == 4 {
				lines = append(lines, fmt.Sprintf("    ... and %d more", len(vs)-4))
				break
			}
			lines = append(lines, "    "+v)
		}
	}
	advice := "No player draws any of this, so nobody who watched the recording saw it, and it is " +
		"in the file for anybody who opens it."
	for _, what := range order {
		if strings.HasPrefix(what, "window") || strings.HasPrefix(what, "directory") {
			advice += " Shells set the window title and report the folder before every prompt, " +
				"usually as account@host and the full path, and recorders keep it with everything else."
			break
		}
	}
	if _, ok := values["terminal version"]; ok {
		advice += " A terminal's exact version, or a colour scheme somebody tuned by hand, is the " +
			"same in every recording they make, which is what makes it worth knowing about."
	} else if _, ok := values["colour scheme"]; ok {
		advice += " A colour scheme somebody tuned by hand is the same in every recording they " +
			"make, which is what makes it worth knowing about."
	}
	r.add(Finding{
		Tag: "UNSEEN ", Weight: Carries, Files: file,
		Title: "Things no player shows are in %s", Lines: lines, Advice: advice,
	})
}

func (s *Study) tells(r *Report, rd *Reading, file []string) {
	var all []Tell
	for _, l := range screenText(rd) {
		all = append(all, tellsIn(l.text)...)
	}
	ts := distinctTells(all)
	if len(ts) == 0 {
		return
	}
	var entries [][]string
	for _, t := range ts {
		v := t.Value
		if !s.reveal {
			v = maskTell(t)
		}
		entries = append(entries, []string{fmt.Sprintf("%-20s %s", t.Kind, v)})
	}
	r.add(Finding{
		Tag: "TELLS  ", Weight: Says, Files: file,
		Title: "Whose machine it was, according to the screen in %s",
		Lines: atMost(entries, 10),
		Advice: "None of this is secret, and all of it was on the screen. For a project " +
			"published under your own name it is nothing. For one published under another, any " +
			"of it ties the two together.",
	})
}

func (s *Study) clock(r *Report, rd *Reading, file []string) {
	c := readClock(rd)
	if c.Zone == nil && c.How == "" {
		return
	}
	var lines []string
	if !c.Started.IsZero() {
		started := "to the second, in UTC"
		if s.reveal {
			started = c.Started.Format("2006-01-02 15:04:05") + " UTC"
		}
		lines = append(lines, "started     "+started)
	}
	zone := "named on the screen"
	if c.Zone != nil {
		zone = "UTC**:**"
		if s.reveal {
			zone = zoneText(*c.Zone)
			if c.Twelve {
				other := *c.Zone + 720
				if other > 840 {
					other = *c.Zone - 720
				}
				zone += ", or " + zoneText(other) + " if that clock is a twelve hour one"
			}
		}
	}
	lines = append(lines, "time zone   "+zone)
	lines = append(lines, "because     "+c.How)
	r.add(Finding{
		Tag: "CLOCK  ", Weight: Says, Files: file,
		Title: "%s says where in the world it was made",
		Lines: lines,
		Advice: "A zone narrows down where somebody lives to a band of the map, and with the " +
			"start time it ties the recording to anything else done that minute, a commit or a " +
			"post. --reveal prints both.",
	})
}

// liveTyping says, once for a source, whether the typing in it is somebody
// typing. It is not a problem and the report does not call it one; it is
// the thing that makes two recordings comparable, and the reason to run
// this with two folders.
func (s *Study) liveTyping(r *Report, i int) {
	h := handsOf(s.Readings[i])
	switch {
	case h.keys >= enoughKeys && h.scripted:
		r.add(Finding{
			Tag: "HANDS  ", Weight: Note,
			Title: fmt.Sprintf("The typing in %s is machine paced", s.Sources[i].Name),
			Advice: "Every key comes at nearly the same interval, which is what VHS and the " +
				"typing simulators do. That carries no rhythm at all, so there is nothing in it to " +
				"tie these recordings to the hands of anybody.",
		})
	case h.keys >= enoughKeys:
		r.add(Finding{
			Tag: "HANDS  ", Weight: Note,
			Title: fmt.Sprintf("The typing in %s is live: %s, with the rhythm of whoever typed them",
				s.Sources[i].Name, plural(h.keys, "key", "keys")),
			Advice: "The gaps between keys are as personal as handwriting, and every recording " +
				"typed live carries them. That matters only if two recordings must never be tied " +
				"to the same person: give outtakes both, one folder each, and it says whether the " +
				"rhythm would do it.",
		})
	}
}

// alreadySaid says whether a secret is one already reported, seen again
// half typed. A password typed a key at a time is on the screen in every
// length on its way to the end, and zsh glues its clock to whatever reaches
// the right margin, so the lab session showed one password three times
// over, each a little different.
func alreadySaid(reported map[string]bool, value string) bool {
	if reported[value] {
		return true
	}
	for had := range reported {
		common := 0
		for common < len(had) && common < len(value) && had[common] == value[common] {
			common++
		}
		if common >= 6 && common*5 >= min(len(had), len(value))*4 {
			return true
		}
	}
	return false
}

func secretAdvice(n int) string {
	if n == 1 {
		return "Covered here, and nowhere else: the recording plays it for anybody who presses " +
			"play, and it is in the file as text. Revoke it rather than re-recording, since every " +
			"copy already downloaded still has it."
	}
	return "Covered here, and nowhere else: the recording plays them for anybody who presses " +
		"play, and they are in the file as text. Revoke them rather than re-recording, since " +
		"every copy already downloaded still has them."
}
