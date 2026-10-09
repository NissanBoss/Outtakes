package main

// What ties one set of recordings to another.
//
// This is the question somebody with two names asks: the projects under
// their own and the ones under the other, each with a demo or two. The
// recordings do not have to share a secret to be tied together. They can
// share a host name, a terminal down to its build, a colour scheme tuned by
// hand, a time zone, or the hands that typed them.

import (
	"fmt"
	"sort"
)

type mark struct {
	kind, value string
}

// strong marks are the ones that are close to unique to a person. The rest
// narrow things down without naming anyone: a time zone is shared by a few
// hundred million people, a terminal's exact build by everybody who updated
// that week, a colour scheme by everybody who never changed it. Three of
// them together are another matter, and are counted as one strong mark.
var strongMarks = map[string]bool{
	"account and host": true, "home folder": true, "email address": true,
	"internal host name": true, "AWS account": true,
}

const weakMarksThatName = 3

func (s *Study) marks(i int) map[mark]bool {
	out := map[mark]bool{}
	for _, rd := range s.Readings[i] {
		for _, l := range screenText(rd) {
			for _, t := range tellsIn(l.text) {
				out[mark{t.Kind, t.Value}] = true
			}
		}
		for _, u := range rd.Unseen {
			for _, t := range tellsIn(u.Value) {
				out[mark{t.Kind, t.Value}] = true
			}
			if u.What == "terminal version" || u.What == "colour scheme" {
				out[mark{u.What, u.Value}] = true
			}
		}
		if c := readClock(rd); c.Zone != nil {
			out[mark{"time zone", zoneText(*c.Zone)}] = true
		}
		if rec := rd.Rec; rec.Cols > 0 && !ordinarySize(rec.Cols, rec.Rows) {
			out[mark{"window size", fmt.Sprintf("%dx%d", rec.Cols, rec.Rows)}] = true
		}
	}
	return out
}

// ordinarySize is a window nobody chose: the default of every terminal
// since the VT100, and the sizes recorders force when asked to.
func ordinarySize(cols, rows int) bool {
	switch fmt.Sprintf("%dx%d", cols, rows) {
	case "80x24", "80x25", "80x30", "100x30", "120x30", "120x40":
		return true
	}
	return false
}

func (s *Study) compare(r *Report) {
	hands := make([]Hands, len(s.Sources))
	for i := range s.Sources {
		hands[i] = handsOf(s.Readings[i])
	}
	for i := 0; i < len(s.Sources); i++ {
		for j := i + 1; j < len(s.Sources); j++ {
			s.comparePair(r, i, j, hands[i], hands[j])
		}
	}
}

// The bands are where this part of the tool lives, so they sit here in
// plain sight. They were measured, not picked.
//
// 858 public recordings from 110 asciinema.org accounts; 38 of the accounts
// had enough live typing to split into two halves of their own recordings.
// The halves of one account landed at a median of 0.99, ninety per cent of
// them at 1.07 or under. Halves of two different accounts, 949 pairs, landed
// at a median of 1.38, and only one pair in two hundred under 1.05.
//
// Between 1.10 and 1.25 the two overlap, and an answer there would be a
// guess, so it says so. Above 1.25 sat four different pairs in five and one
// same pair in thirty four: that is where "different hands" is said, and
// not lower, because being told the typing does not tie two names together
// when it does is the expensive mistake.
const (
	sameHands     = 1.05
	probablySame  = 1.10
	tooClose      = 1.25
	notEnoughKeys = "not enough typing to say"
)

type handsCall int

const (
	noCall handsCall = iota
	sameCall
	unsureCall
	differentCall
)

func handsVerdict(l Likeness, a, b Hands, an, bn string) (string, handsCall) {
	switch {
	case a.scripted || b.scripted:
		return "Typing rhythm: one of them is machine paced, so the typing cannot tie them.", noCall
	case a.keys < enoughKeys || b.keys < enoughKeys:
		if a.keys == 0 && b.keys == 0 {
			return "", noCall
		}
		return fmt.Sprintf("Typing rhythm: %s (%s has %s and %s has %s; %d each are needed).",
			notEnoughKeys, an, plural(a.keys, "key", "keys"), bn, plural(b.keys, "key", "keys"), enoughKeys), noCall
	case !l.Ready:
		return "Typing rhythm: " + notEnoughKeys + ", because the two share too few pairs of keys to rank.", noCall
	case l.Ratio <= sameHands:
		return fmt.Sprintf("Typing rhythm: the same hands. The two are no further apart than each "+
			"is from itself (%.2f, over %d pairs of keys).", l.Ratio, l.Shared), sameCall
	case l.Ratio <= probablySame:
		return fmt.Sprintf("Typing rhythm: probably the same hands. The two sit close to how far "+
			"each is from itself (%.2f, over %d pairs of keys).", l.Ratio, l.Shared), sameCall
	case l.Ratio <= tooClose:
		return fmt.Sprintf("Typing rhythm: too close to call. Different people land here and so "+
			"do the same people on different days (%.2f, over %d pairs of keys).", l.Ratio, l.Shared), unsureCall
	}
	return fmt.Sprintf("Typing rhythm: different hands, as far as this can tell (%.2f, over %d "+
		"pairs of keys).", l.Ratio, l.Shared), differentCall
}

func (s *Study) comparePair(r *Report, i, j int, hi, hj Hands) {
	a, b := s.Sources[i].Name, s.Sources[j].Name
	mi, mj := s.marks(i), s.marks(j)
	var both []mark
	for m := range mi {
		if mj[m] {
			both = append(both, m)
		}
	}
	sort.Slice(both, func(x, y int) bool {
		sx, sy := strongMarks[both[x].kind], strongMarks[both[y].kind]
		if sx != sy {
			return sx
		}
		if both[x].kind != both[y].kind {
			return both[x].kind < both[y].kind
		}
		return both[x].value < both[y].value
	})

	strong, weak := 0, 0
	var lines []string
	for _, m := range both {
		if strongMarks[m.kind] {
			strong++
		} else {
			weak++
		}
		v := m.value
		if !s.reveal {
			switch m.kind {
			case "terminal version", "colour scheme", "window size":
			case "time zone":
				v = "UTC**:**"
			default:
				v = maskTell(Tell{m.kind, m.value})
			}
		}
		lines = append(lines, fmt.Sprintf("%-20s %s", m.kind, clip(v, wrap-21)))
	}

	verdict, call := handsVerdict(compareHands(hi, hj), hi, hj, a, b)
	if verdict != "" {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, wrapText(verdict, wrap)...)
	}

	f := Finding{Tag: "MATCHES", Lines: lines}
	switch {
	case strong > 0 || call == sameCall:
		f.Weight = Keeps
		f.Title = fmt.Sprintf("%s and %s can be tied to each other", a, b)
		f.Advice = "Each of these is in both, and one strong match is enough for somebody " +
			"comparing the two to stop looking. Re-record the demos for one of them on a " +
			"machine, account and terminal that belong to that name only."
	case weak >= weakMarksThatName:
		f.Weight = Keeps
		f.Title = fmt.Sprintf("%s and %s share enough to be tied to each other", a, b)
		f.Advice = "None of these names anybody on its own. Together they leave very few people " +
			"who fit both, and that is all somebody comparing the two needs."
	case len(both) > 0:
		f.Weight = Says
		f.Title = fmt.Sprintf("%s and %s share a little, and nothing that names anybody", a, b)
		f.Advice = "A time zone, a terminal or a window size is shared by a great many people. " +
			"On its own it ties nothing together; next to anything else, it narrows the field."
	default:
		f.Weight = Note
		f.Title = fmt.Sprintf("Nothing here ties %s to %s", a, b)
		f.Advice = "No account, host, terminal, colour scheme or time zone turns up in both."
		if call == differentCall {
			f.Advice += " The typing does not tie them either."
		}
	}
	r.add(f)
}
