package main

// The rhythm of the hands that typed a recording.
//
// Every recording of somebody typing live carries the gaps between their
// keys, either because it saved the keyboard or because each key came back
// as an echo a millisecond later. Those gaps are a biometric. How long a
// person takes from t to h, against how long from h to e, is about as
// stable as their handwriting and as hard to change on purpose, and unlike
// handwriting nobody thinks of it as something they are publishing.
//
// The comparison is Gunetti and Picardi's, from their 2005 work on free
// text: take the pairs of keys both samples typed, rank the pairs by how
// long each sample took over them, and measure how far the two rankings
// disagree, plus how many of the pairs took about as long in both. No
// model, nothing trained, arithmetic anybody can redo by hand.
//
// What a distance means depends on how much typing there is, so it is
// never read raw. Each source is cut in two at random, again and again,
// and the distance between the two sources is set against the distance
// between each source's own halves. The same hands come out near one.

import (
	"math"
	"math/rand"
	"sort"
	"unicode"
)

// enoughKeys is the least typing a comparison is attempted on. Below it the
// pairs two samples share are too few to rank, and an answer would be
// noise with a number on it.
const enoughKeys = 300

type Hands struct {
	keys     int
	scripted bool
	// runs are stretches of uninterrupted typing, the unit the halves are
	// cut from. Cutting mid-run would put one word in both halves.
	runs []run
}

type run struct {
	recording int
	pairs     []pair
}

type pair struct {
	keys string
	gap  float64
}

func handsOf(rds []*Reading) Hands {
	var h Hands
	var gaps []float64
	for i, rd := range rds {
		var current run
		current.recording = i
		flush := func() {
			if len(current.pairs) > 0 {
				h.runs = append(h.runs, current)
			}
			current = run{recording: i}
		}
		var last stroke
		for _, st := range rd.Strokes {
			if st.r == 0 || st.r == '\r' || st.r == '\b' || st.r == 0x1b {
				flush()
				last = stroke{}
				continue
			}
			h.keys++
			gap := st.at - last.at
			if last.r != 0 && gap > 0.015 && gap < 1.5 {
				current.pairs = append(current.pairs, pair{keys: fold(last.r) + fold(st.r), gap: gap})
				gaps = append(gaps, gap)
			} else if last.r != 0 {
				flush()
			}
			last = st
		}
		flush()
	}
	h.scripted = machinePaced(gaps)
	return h
}

func fold(r rune) string {
	return string(unicode.ToLower(r))
}

// machinePaced spots typing no person did. VHS types every key the same
// number of milliseconds apart, and so do most of the scripts that fake
// typing for a demo; people never manage a spread that narrow.
func machinePaced(gaps []float64) bool {
	if len(gaps) < 50 {
		return false
	}
	mean, sd := 0.0, 0.0
	for _, g := range gaps {
		mean += g
	}
	mean /= float64(len(gaps))
	same := map[int]int{}
	for _, g := range gaps {
		sd += (g - mean) * (g - mean)
		same[int(math.Round(g*1000))]++
	}
	sd = math.Sqrt(sd / float64(len(gaps)))
	most := 0
	for _, n := range same {
		most = max(most, n)
	}
	return sd/mean < 0.2 || most*2 > len(gaps)
}

// profile is the typing of one half: each pair of keys with the typical
// time over it. Typical means the median of the logged gaps, so one
// pause to think in the middle of a word does not move a pair's place in
// the ranking.
type profile map[string]float64

func profileOf(runs []run) profile {
	gaps := map[string][]float64{}
	for _, r := range runs {
		for _, p := range r.pairs {
			gaps[p.keys] = append(gaps[p.keys], math.Log(p.gap))
		}
	}
	out := profile{}
	for k, gs := range gaps {
		sort.Float64s(gs)
		out[k] = gs[len(gs)/2]
	}
	return out
}

// distance is Gunetti and Picardi's R plus their A. R is how scrambled one
// ranking of the shared pairs is against the other, as a share of the most
// scrambled two rankings can be. A is the share of shared pairs whose
// times differ by more than a quarter. Both go from 0 for the same hands
// on a good day to 1.
func distance(a, b profile) (float64, int) {
	var shared []string
	for k := range a {
		if _, ok := b[k]; ok {
			shared = append(shared, k)
		}
	}
	n := len(shared)
	if n < 2 {
		return math.NaN(), n
	}
	sort.Strings(shared)
	rank := func(p profile) map[string]int {
		order := append([]string(nil), shared...)
		sort.SliceStable(order, func(i, j int) bool { return p[order[i]] < p[order[j]] })
		out := map[string]int{}
		for i, k := range order {
			out[k] = i
		}
		return out
	}
	ra, rb := rank(a), rank(b)
	disorder := 0
	apart := 0
	for _, k := range shared {
		d := ra[k] - rb[k]
		if d < 0 {
			d = -d
		}
		disorder += d
		if math.Abs(a[k]-b[k]) > math.Log(1.25) {
			apart++
		}
	}
	most := n * n / 2
	if n%2 == 1 {
		most = (n*n - 1) / 2
	}
	return float64(disorder)/float64(most) + float64(apart)/float64(n), n
}

type Likeness struct {
	// Ratio is the distance between the two sources over the distance
	// within each. Near one, the halves of one source are as far from
	// each other as they are from the other source.
	Ratio  float64
	Shared int
	Ready  bool
}

// splits is how many random halvings the answer averages over. One split
// is a single draw from a noisy distribution; thirty three of them sit
// still from one run to the next.
const splits = 33

func compareHands(a, b Hands) Likeness {
	return compareWith(a, b, distance)
}

func compareWith(a, b Hands, measure func(a, b profile) (float64, int)) Likeness {
	if a.keys < enoughKeys || b.keys < enoughKeys || a.scripted || b.scripted ||
		len(a.runs) < 4 || len(b.runs) < 4 {
		return Likeness{}
	}
	random := rand.New(rand.NewSource(seedOf(a, b)))
	within, between := 0.0, 0.0
	shared := 0
	tries := 0
	for range splits {
		a1, a2 := halve(a.runs, random)
		b1, b2 := halve(b.runs, random)
		pa1, pa2, pb1, pb2 := profileOf(a1), profileOf(a2), profileOf(b1), profileOf(b2)
		w1, n1 := measure(pa1, pa2)
		w2, n2 := measure(pb1, pb2)
		x1, n3 := measure(pa1, pb1)
		x2, n4 := measure(pa2, pb2)
		x3, _ := measure(pa1, pb2)
		x4, _ := measure(pa2, pb1)
		if math.IsNaN(w1 + w2 + x1 + x2 + x3 + x4) {
			continue
		}
		within += (w1 + w2) / 2
		between += (x1 + x2 + x3 + x4) / 4
		shared += min(n1, n2, n3, n4)
		tries++
	}
	if tries == 0 || within == 0 {
		return Likeness{}
	}
	return Likeness{Ratio: between / within, Shared: shared / tries, Ready: shared/tries >= 20}
}

// halve deals a source's runs into two piles at random. When the runs come
// from several recordings, whole recordings go to each side, because the
// question being asked is whether two different sittings were the same
// hands, and a sitting is more like itself than like the person's other
// sittings.
func halve(runs []run, random *rand.Rand) ([]run, []run) {
	recordings := map[int]bool{}
	for _, r := range runs {
		recordings[r.recording] = true
	}
	var left, right []run
	if len(recordings) >= 4 {
		ids := make([]int, 0, len(recordings))
		for id := range recordings {
			ids = append(ids, id)
		}
		sort.Ints(ids)
		random.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
		side := map[int]bool{}
		for i, id := range ids {
			side[id] = i < len(ids)/2
		}
		for _, r := range runs {
			if side[r.recording] {
				left = append(left, r)
			} else {
				right = append(right, r)
			}
		}
		return left, right
	}
	order := random.Perm(len(runs))
	for i, which := range order {
		if i < len(order)/2 {
			left = append(left, runs[which])
		} else {
			right = append(right, runs[which])
		}
	}
	return left, right
}

// seedOf makes the shuffling depend on the typing and nothing else, so the
// same two folders give the same answer every time.
func seedOf(hs ...Hands) int64 {
	var seed int64 = 1469598103934665603
	for _, h := range hs {
		for _, r := range h.runs {
			for _, p := range r.pairs {
				seed = (seed ^ int64(math.Float64bits(p.gap)>>20)) * 1099511628211
				for i := 0; i < len(p.keys); i++ {
					seed = (seed ^ int64(p.keys[i])) * 1099511628211
				}
			}
		}
	}
	return seed
}
