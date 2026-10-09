package main

// When a recording was made, and where on the globe.
//
// Most formats stamp the start of the recording in UTC to the second, and
// say nothing about the zone. The zone leaks anyway, from the screen: a
// prompt with the time in it is a local clock printed next to a UTC one,
// and the difference between the two is the offset. One prompt is a guess;
// a prompt every few seconds agreeing to the second is not. script(1) does
// not leave it to inference and writes the offset on its first line.

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Clock struct {
	Started time.Time
	// Zone is minutes east of UTC, and How says where it came from.
	Zone     *int
	How      string
	Evidence string
	// Twelve is set when the clock read off the screen could be a twelve
	// hour one, which makes the zone one of two twelve hours apart.
	Twelve bool
}

var (
	clockOnScreen = regexp.MustCompile(`(?:^|[^\d:])([01]?\d|2[0-3]):([0-5]\d)(?::([0-5]\d))?(?:\s?([AaPp][Mm]))?(?:[^\d:]|$)`)
	datePrinted   = regexp.MustCompile(`\b(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun)\b[^\n]*?\b\d{1,2}:\d{2}:\d{2}\s+([A-Z]{2,5}|[+-]\d{2}(?::?\d{2})?)\s+\d{4}\b`)
)

// Abbreviations date(1) prints, for the zones people are most often in. An
// abbreviation not here is still reported, by name, without an offset.
var zoneNames = map[string]int{
	"UTC": 0, "GMT": 0, "WET": 0, "WEST": 60, "BST": 60, "IST": 330, "CET": 60,
	"CEST": 120, "EET": 120, "EEST": 180, "MSK": 180, "EST": -300, "EDT": -240,
	"CST": -360, "CDT": -300, "MST": -420, "MDT": -360, "PST": -480, "PDT": -420,
	"AKST": -540, "AKDT": -480, "HST": -600, "JST": 540, "KST": 540, "HKT": 480,
	"SGT": 480, "AEST": 600, "AEDT": 660, "ACST": 570, "AWST": 480, "NZST": 720,
	"NZDT": 780, "BRT": -180, "ART": -180, "SAST": 120, "WAT": 60, "EAT": 180,
}

func readClock(rd *Reading) Clock {
	c := clockOf(rd)
	// UTC is what servers and containers are set to, so a recording that
	// says UTC says where the machine was configured, not where anybody
	// was sitting. It is left out rather than reported as a place.
	if c.Zone != nil && *c.Zone == 0 {
		return Clock{Started: c.Started}
	}
	return c
}

func clockOf(rd *Reading) Clock {
	rec := rd.Rec
	c := Clock{Started: rec.Started}
	if rec.Zone != nil {
		c.Zone, c.How = rec.Zone, "script writes it on its first line"
		return c
	}
	for _, l := range rd.Lines {
		if !mentions(l.text, "Mon |Tue |Wed |Thu |Fri |Sat |Sun ") {
			continue
		}
		m := datePrinted.FindStringSubmatch(l.text)
		if m == nil {
			continue
		}
		c.Evidence = strings.TrimSpace(m[0])
		if minutes, ok := zoneNames[m[1]]; ok {
			c.Zone, c.How = &minutes, "date printed it, as "+m[1]
			return c
		}
		if minutes, ok := numericZone(m[1]); ok {
			c.Zone, c.How = &minutes, "date printed it"
			return c
		}
		c.How = "date printed the zone as " + m[1]
		return c
	}
	if !rec.Started.IsZero() {
		c.fromPrompts(rd.Lines)
	}
	return c
}

func numericZone(s string) (int, bool) {
	s = strings.Replace(s, ":", "", 1)
	if len(s) != 3 && len(s) != 5 {
		return 0, false
	}
	h, err := strconv.Atoi(s[1:3])
	if err != nil {
		return 0, false
	}
	m := 0
	if len(s) == 5 {
		m, _ = strconv.Atoi(s[3:])
	}
	minutes := h*60 + m
	if s[0] == '-' {
		minutes = -minutes
	}
	return minutes, true
}

// fromPrompts reads the zone off clocks on the screen. Every time shown is
// compared with the UTC time the recording says it was shown at, and the
// difference, if it is a whole quarter hour to within half a minute, is a
// vote for that offset. ls -l and git log print times too, but they print
// the times of files and commits, which vote all over the place; a prompt
// votes for the same offset every time it is drawn.
func (c *Clock) fromPrompts(lines []seen) {
	votes := map[int]int{}
	counted := map[string]bool{}
	settled := map[int]bool{}
	example := map[int]string{}
	for _, l := range lines {
		if !strings.Contains(l.text, ":") {
			continue
		}
		for _, m := range clockOnScreen.FindAllStringSubmatch(l.text, -1) {
			if m[3] == "" {
				// Without seconds a minute's slack becomes a quarter of
				// the agreement needed, and the votes stop meaning much.
				continue
			}
			h, _ := strconv.Atoi(m[1])
			mi, _ := strconv.Atoi(m[2])
			s, _ := strconv.Atoi(m[3])
			ampm := strings.ToLower(m[4])
			if ampm == "pm" && h < 12 {
				h += 12
			} else if ampm == "am" && h == 12 {
				h = 0
			}
			utc := c.Started.Add(time.Duration(l.at * float64(time.Second)))
			shown := float64(h*3600 + mi*60 + s)
			now := float64(utc.Hour()*3600 + utc.Minute()*60 + utc.Second())
			diff := math.Mod(shown-now+86400*1.5, 86400) - 43200
			quarter := math.Round(diff/900) * 900
			if math.Abs(diff-quarter) > 30 {
				continue
			}
			offset := int(quarter / 60)
			if !inUse[offset] {
				continue
			}
			// One prompt is filed again with every key typed after it, and
			// each copy carries the same clock. It gets one vote.
			key := fmt.Sprint(offset, m[1], m[2], m[3])
			if counted[key] {
				continue
			}
			counted[key] = true
			votes[offset]++
			if ampm != "" || h > 12 {
				settled[offset] = true
			}
			if example[offset] == "" {
				example[offset] = strings.TrimSpace(m[0])
			}
		}
	}
	best, most := 0, 0
	for offset, n := range votes {
		if n > most || n == most && offset < best {
			best, most = offset, n
		}
	}
	if most < 2 {
		return
	}
	c.Zone, c.How, c.Evidence = &best, "a clock on the screen agrees with it to the second", example[best]
	c.Twelve = !settled[best]
}

func zoneText(minutes int) string {
	sign := "+"
	if minutes < 0 {
		sign, minutes = "-", -minutes
	}
	return fmt.Sprintf("UTC%s%02d:%02d", sign, minutes/60, minutes%60)
}

// inUse is every offset from UTC some place on Earth keeps, in minutes. A
// clock that disagrees with the recording by anything else is not the time
// of day: it is a stopwatch, a countdown or a duration, and the first run
// over public recordings had a "0:00:00" timer voting for UTC-11:30.
var inUse = map[int]bool{
	-720: true, -660: true, -600: true, -570: true, -540: true, -480: true, -420: true,
	-360: true, -300: true, -240: true, -210: true, -180: true, -120: true, -60: true,
	0: true, 60: true, 120: true, 180: true, 210: true, 240: true, 270: true, 300: true,
	330: true, 345: true, 360: true, 390: true, 420: true, 480: true, 525: true, 540: true,
	570: true, 600: true, 630: true, 660: true, 720: true, 765: true, 780: true, 840: true,
}
