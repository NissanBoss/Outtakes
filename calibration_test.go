package main

// The calibration, against public recordings from asciinema.org rather than
// the lab sessions in testdata.
//
// Those recordings are other people's, so they are not in the repository,
// and these tests only ever count. Nothing about any one recording is
// printed: not its id, not its author, not what was found in it. Point the
// tests at a folder with one subfolder per author:
//
//	OUTTAKES_CAL=folder go test -run Calibration -v -timeout 30m
//
// The first asks what turns up in recordings people chose to publish. The
// second asks whether the rhythm comparison tells one author from another,
// which is where the bands in links.go come from.

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type authorCasts struct {
	name     string
	readings []*Reading
}

// calibrated is the set, read once for all the tests that use it: it is
// hundreds of megabytes, and reading it three times took half an hour.
var calibrated []authorCasts

func calibrationSet(t *testing.T) []authorCasts {
	root := os.Getenv("OUTTAKES_CAL")
	if root == "" {
		t.Skip("OUTTAKES_CAL is not set")
	}
	if calibrated != nil {
		return calibrated
	}
	dirs, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var out []authorCasts
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		s := readSource(filepath.Join(root, d.Name()))
		a := authorCasts{name: d.Name()}
		for _, rec := range s.Recordings {
			a.readings = append(a.readings, read(rec))
		}
		out = append(out, a)
	}
	calibrated = out
	return out
}

func TestCalibrationWhatPeoplePublish(t *testing.T) {
	set := calibrationSet(t)
	counts := map[string]int{}
	total := 0
	var order []string
	count := func(what string, yes bool) {
		if _, ok := counts[what]; !ok {
			order = append(order, what)
			counts[what] = 0
		}
		if yes {
			counts[what]++
		}
	}
	for _, a := range set {
		for _, rd := range a.readings {
			total++
			rec := rd.Rec
			count("had a gap in reading", len(rec.Gaps) > 0)
			count("recorded the keyboard", rec.hasInput())
			count("kept a password typed at a hidden prompt", len(rd.Hidden) > 0)
			deleted, abandoned := false, false
			for _, o := range rd.Outtakes {
				if strings.Contains(o.How, "ctrl-c") {
					abandoned = true
				} else {
					deleted = true
				}
			}
			count("kept a line typed and deleted", deleted)
			count("kept a line abandoned with ctrl-c", abandoned)
			secret := false
			for _, l := range screenText(rd) {
				if len(secretsIn(l.text)) > 0 {
					secret = true
					break
				}
			}
			count("showed something a secret rule matches", secret)
			kinds := map[string]bool{}
			for _, u := range rd.Unseen {
				if worthSaying(u) {
					kinds[u.What] = true
				}
			}
			count("carried a window title", kinds["window title"] || kinds["icon title"])
			count("carried the directory, reported to the terminal", kinds["directory reported to the terminal"] || kinds["iTerm2 CurrentDir"])
			count("carried the terminal's name and version", kinds["terminal version"])
			count("carried the colour scheme", kinds["colour scheme"])
			var tells []Tell
			for _, l := range screenText(rd) {
				tells = append(tells, tellsIn(l.text)...)
			}
			has := map[string]bool{}
			for _, tl := range tells {
				has[tl.Kind] = true
			}
			count("showed an account@host", has["account and host"])
			count("showed a home folder", has["home folder"])
			count("showed an internal host name", has["internal host name"])
			count("showed an email address", has["email address"])
			c := readClock(rd)
			count("gave away its time zone", c.Zone != nil)
			count("started at a time it states", !rec.Started.IsZero())
			h := handsOf([]*Reading{rd})
			count("had live typing, enough to compare", h.keys >= enoughKeys && !h.scripted)
			count("had machine paced typing", h.keys >= 50 && h.scripted)
		}
	}
	t.Logf("%d recordings from %d authors", total, len(set))
	for _, what := range order {
		t.Logf("  %-50s %4d  %5.1f%%", what, counts[what], 100*float64(counts[what])/float64(max(total, 1)))
	}
	if total > 0 && float64(counts["had a gap in reading"])/float64(total) > 0.05 {
		t.Errorf("more than one recording in twenty could not be read whole")
	}
}

// halvesOf splits an author's live recordings into two sittings each, the
// way somebody using this would have them: some recordings under one name
// and some under another.
func halvesOf(rds []*Reading) ([]*Reading, []*Reading) {
	var live []*Reading
	for _, rd := range rds {
		if h := handsOf([]*Reading{rd}); h.keys >= 30 && !h.scripted {
			live = append(live, rd)
		}
	}
	var a, b []*Reading
	for i, rd := range live {
		if i%2 == 0 {
			a = append(a, rd)
		} else {
			b = append(b, rd)
		}
	}
	return a, b
}

func TestCalibrationHands(t *testing.T) {
	set := calibrationSet(t)
	type half struct {
		author string
		hands  Hands
	}
	var firsts, seconds []half
	for _, a := range set {
		x, y := halvesOf(a.readings)
		hx, hy := handsOf(x), handsOf(y)
		if hx.keys >= enoughKeys && hy.keys >= enoughKeys {
			firsts = append(firsts, half{a.name, hx})
			seconds = append(seconds, half{a.name, hy})
		}
	}
	var same, different []float64
	for i := range firsts {
		if l := compareHands(firsts[i].hands, seconds[i].hands); l.Ready {
			same = append(same, l.Ratio)
		}
		for j := range seconds {
			if i == j {
				continue
			}
			if l := compareHands(firsts[i].hands, seconds[j].hands); l.Ready {
				different = append(different, l.Ratio)
			}
		}
	}
	t.Logf("%d authors with enough live typing in two halves", len(firsts))
	t.Logf("same author:      %s", spreadOf(same))
	t.Logf("different author: %s", spreadOf(different))
	t.Logf("separation (area under the curve): %.3f", separation(same, different))
	for _, cut := range []float64{1.0, 1.05, 1.1, 1.15, 1.2, 1.25, 1.3, 1.4, 1.5} {
		t.Logf("  linked at %.2f or less: %5.1f%% of same authors, %5.1f%% of different ones",
			cut, 100*share(same, cut), 100*share(different, cut))
	}
}

func spreadOf(xs []float64) string {
	if len(xs) == 0 {
		return "none"
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	q := func(p float64) float64 { return s[int(p*float64(len(s)-1))] }
	return fmt.Sprintf("n=%d  min %.2f  10%% %.2f  median %.2f  90%% %.2f  max %.2f",
		len(s), s[0], q(0.1), q(0.5), q(0.9), s[len(s)-1])
}

func share(xs []float64, cut float64) float64 {
	n := 0
	for _, x := range xs {
		if x <= cut {
			n++
		}
	}
	return float64(n) / math.Max(1, float64(len(xs)))
}

// separation is the chance that a same-author pair sits closer than a
// different-author pair: 0.5 is a coin, 1 is perfect.
func separation(same, different []float64) float64 {
	if len(same) == 0 || len(different) == 0 {
		return math.NaN()
	}
	wins := 0.0
	for _, s := range same {
		for _, d := range different {
			switch {
			case s < d:
				wins++
			case s == d:
				wins += 0.5
			}
		}
	}
	return wins / float64(len(same)*len(different))
}

// TestCalibrationHandsMeasures is how the distance in hands.go was chosen:
// the same pairs, measured every way that was tried, side by side.
func TestCalibrationHandsMeasures(t *testing.T) {
	set := calibrationSet(t)
	type pairOf struct{ a, b Hands }
	var firsts, seconds []Hands
	for _, a := range set {
		x, y := halvesOf(a.readings)
		hx, hy := handsOf(x), handsOf(y)
		if hx.keys >= enoughKeys && hy.keys >= enoughKeys {
			firsts = append(firsts, hx)
			seconds = append(seconds, hy)
		}
	}
	measures := []struct {
		name string
		f    func(a, b profile) (float64, int)
	}{
		{"rank disorder plus share apart (Gunetti and Picardi)", distance},
		{"mean difference of logged gaps", meanGapDistance},
		{"rhythm with speed taken out", patternDistance},
		{"mean difference plus rank disorder", func(a, b profile) (float64, int) {
			d, n := meanGapDistance(a, b)
			r, _ := rankDistance(a, b)
			return d + r, n
		}},
	}
	for _, m := range measures {
		var same, different []float64
		for i := range firsts {
			if l := compareWith(firsts[i], seconds[i], m.f); l.Ready {
				same = append(same, l.Ratio)
			}
			for j := range seconds {
				if j != i {
					if l := compareWith(firsts[i], seconds[j], m.f); l.Ready {
						different = append(different, l.Ratio)
					}
				}
			}
		}
		t.Logf("%s", m.name)
		t.Logf("  same:      %s", spreadOf(same))
		t.Logf("  different: %s", spreadOf(different))
		t.Logf("  separation %.3f", separation(same, different))
	}
}

func sharedKeys(a, b profile) []string {
	var out []string
	for k := range a {
		if _, ok := b[k]; ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func meanGapDistance(a, b profile) (float64, int) {
	keys := sharedKeys(a, b)
	if len(keys) < 2 {
		return math.NaN(), len(keys)
	}
	total := 0.0
	for _, k := range keys {
		total += math.Abs(a[k] - b[k])
	}
	return total / float64(len(keys)), len(keys)
}

func patternDistance(a, b profile) (float64, int) {
	keys := sharedKeys(a, b)
	if len(keys) < 2 {
		return math.NaN(), len(keys)
	}
	mid := func(p profile) float64 {
		var v []float64
		for _, k := range keys {
			v = append(v, p[k])
		}
		sort.Float64s(v)
		return v[len(v)/2]
	}
	ma, mb := mid(a), mid(b)
	total := 0.0
	for _, k := range keys {
		total += math.Abs((a[k] - ma) - (b[k] - mb))
	}
	return total / float64(len(keys)), len(keys)
}

func rankDistance(a, b profile) (float64, int) {
	keys := sharedKeys(a, b)
	n := len(keys)
	if n < 2 {
		return math.NaN(), n
	}
	rank := func(p profile) map[string]int {
		order := append([]string(nil), keys...)
		sort.SliceStable(order, func(i, j int) bool { return p[order[i]] < p[order[j]] })
		out := map[string]int{}
		for i, k := range order {
			out[k] = i
		}
		return out
	}
	ra, rb := rank(a), rank(b)
	disorder := 0
	for _, k := range keys {
		d := ra[k] - rb[k]
		if d < 0 {
			d = -d
		}
		disorder += d
	}
	most := n * n / 2
	if n%2 == 1 {
		most = (n*n - 1) / 2
	}
	return float64(disorder) / float64(most), n
}
