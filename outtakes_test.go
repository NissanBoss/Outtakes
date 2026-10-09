package main

import (
	"bytes"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fixtures in testdata were made by the real recorders, in a container,
// typing the same session into each: testdata/record has the recipe. The
// session cds into a project, fixes a typo, types a password into an export
// and kills the line with ctrl-u, answers a hidden prompt, cats a .env,
// clears the screen, and abandons an rm -rf with ctrl-c.
var leaky = []string{
	"bash-asciinema2-stdin.cast",
	"bash-asciinema3.cast",
	"zsh-asciinema3-input.cast",
	"bash-script-classic",
	"bash-script-advanced",
	"bash.ttyrec",
	"svg-term.svg",
	"termtosvg.svg",
}

// lab is where the recordings are read from: testdata, or a fresh set the
// CI makes with the recipe on every push, so the fixtures here cannot drift
// from what the recorders write today.
func lab() string {
	if dir := os.Getenv("OUTTAKES_LAB"); dir != "" {
		return dir
	}
	return "testdata"
}

func readingOf(t *testing.T, name string) *Reading {
	t.Helper()
	s := readSource(filepath.Join(lab(), name))
	if len(s.Recordings) != 1 {
		t.Fatalf("%s: read %d recordings, gaps %v", name, len(s.Recordings), s.Gaps)
	}
	return read(s.Recordings[0])
}

func TestEveryRecorderKeepsTheOuttakes(t *testing.T) {
	for _, name := range leaky {
		rd := readingOf(t, name)
		var texts []string
		for _, o := range rd.Outtakes {
			texts = append(texts, o.Text)
		}
		all := strings.Join(texts, "\n")
		if !strings.Contains(all, "export DB_PASSWORD=hunter2-correct-staple") {
			t.Errorf("%s: the line killed with ctrl-u was not found in %q", name, texts)
		}
		if !strings.Contains(all, "rm -rf /tmp/old-builds") {
			t.Errorf("%s: the line abandoned with ctrl-c was not found in %q", name, texts)
		}
		// The typo fixed with two backspaces is a typo, and every session
		// has dozens. Reporting it would bury the line above.
		for _, o := range rd.Outtakes {
			if strings.Contains(o.Text, "hc") && len(o.Text) < 4 {
				t.Errorf("%s: a typo was reported: %q", name, o.Text)
			}
		}
	}
}

func TestTheKeyboardGivesThePasswordAway(t *testing.T) {
	keyboard := map[string]bool{
		"bash-asciinema2-stdin.cast": true,
		"zsh-asciinema3-input.cast":  true,
		"bash-script-advanced":       true,
	}
	for _, name := range leaky {
		rd := readingOf(t, name)
		found := false
		for _, h := range rd.Hidden {
			if h.Text == "Tr0ub4dor&3" && h.Prompt == "Password:" {
				found = true
			} else {
				t.Errorf("%s: %q at %q was reported as hidden", name, h.Text, h.Prompt)
			}
		}
		if found != keyboard[name] {
			t.Errorf("%s: hidden password found=%v, keyboard recorded=%v", name, found, keyboard[name])
		}
	}
}

func TestWhatNoPlayerShows(t *testing.T) {
	has := func(rd *Reading, what, value string) bool {
		for _, u := range rd.Unseen {
			if u.What == what && u.Value == value {
				return true
			}
		}
		return false
	}
	bash := readingOf(t, "bash-asciinema2-stdin.cast")
	if !has(bash, "window title", "demo@lab: ~/work/acme-billing") {
		t.Errorf("bash's window title was missed: %v", bash.Unseen)
	}
	zsh := readingOf(t, "zsh-asciinema3-input.cast")
	for _, want := range [][2]string{
		{"directory reported to the terminal", "file://lab/home/demo/work/acme-billing"},
		{"iTerm2 RemoteHost", "demo@lab"},
		{"iTerm2 CurrentDir", "/home/demo/work/acme-billing"},
	} {
		if !has(zsh, want[0], want[1]) {
			t.Errorf("zsh: %s %q was missed", want[0], want[1])
		}
	}
	script := readingOf(t, "bash-script-advanced")
	if !has(script, "logged to", "/out/bash-script-advanced.timing") {
		t.Errorf("script's log paths were missed: %v", script.Unseen)
	}
}

func TestWhereInTheWorld(t *testing.T) {
	for name, how := range map[string]string{
		"bash-script-classic":       "script writes it on its first line",
		"bash-script-advanced":      "script writes it on its first line",
		"zsh-asciinema3-input.cast": "a clock on the screen agrees with it to the second",
	} {
		c := readClock(readingOf(t, name))
		if c.Zone == nil || *c.Zone != -300 || c.How != how {
			t.Errorf("%s: the zone should be UTC-05:00 because %s, got %v %q", name, how, c.Zone, c.How)
		}
	}
	// bash's prompt has no clock in it, and a recording says nothing about
	// its zone on its own.
	if c := readClock(readingOf(t, "bash-asciinema2-stdin.cast")); c.Zone != nil {
		t.Errorf("a zone was invented for a recording with no clock on it: %s", zoneText(*c.Zone))
	}
}

func TestATapeKeepsWhatItHid(t *testing.T) {
	rd := readingOf(t, "deploy.tape")
	want := []string{
		"export DEPLOY_TOKEN=correct-horse-battery-staple && cd ~/src/billing",
		"clear",
		"--force",
	}
	if len(rd.Outtakes) != len(want) {
		t.Fatalf("got %d outtakes, want %d: %v", len(rd.Outtakes), len(want), rd.Outtakes)
	}
	for i, w := range want {
		if strings.TrimSpace(rd.Outtakes[i].Text) != w {
			t.Errorf("outtake %d: got %q, want %q", i, rd.Outtakes[i].Text, w)
		}
	}
}

// Nothing may depend on the order a map came out in, or two runs over the
// same folder give two reports.
func TestTheAnswerDoesNotMove(t *testing.T) {
	first := reportOn(t, true, "testdata")
	for range 3 {
		if again := reportOn(t, true, "testdata"); again != first {
			t.Fatal("the report changed between two runs over the same recordings")
		}
	}
}

// session builds a recording out of events, for the cases the fixtures do
// not cover.
type session struct {
	at     float64
	events []Event
}

func (s *session) out(after float64, sent string) {
	s.at += after
	s.events = append(s.events, Event{At: s.at, Kind: 'o', Sent: sent})
}

func (s *session) key(after float64, sent string) {
	s.at += after
	s.events = append(s.events, Event{At: s.at, Kind: 'i', Sent: sent})
}

// typeLine types text a key at a time, each echoed after the given delay,
// or not echoed at all when echo is negative.
func (s *session) typeLine(text string, gap, echo float64) {
	for _, r := range text {
		s.key(gap, string(r))
		if echo >= 0 {
			s.out(echo, string(r))
			s.at -= echo
		}
	}
	s.key(gap, "\r")
	s.out(0.001, "\r\n")
}

func (s *session) recording() *Recording {
	return &Recording{Path: "made-up.cast", Format: "asciicast v2", Cols: 80, Rows: 24, Events: s.events}
}

// Over ssh every echo comes back a round trip late. A window fitted to a
// local shell reads every key typed over a slow link as never echoed, and
// calls every command a password.
func TestALateEchoIsStillAnEcho(t *testing.T) {
	var s session
	s.out(0, "$ ")
	s.typeLine("ls -la /var/log", 0.15, 0.25)
	s.out(0.3, "total 0\r\n$ ")
	s.typeLine("git status", 0.15, 0.25)
	s.out(0.3, "$ sudo true\r\n[sudo] password for someone: ")
	s.typeLine("letmein99", 0.15, -1)
	s.out(0.3, "\r\n$ ")
	rd := read(s.recording())
	if len(rd.Hidden) != 1 || rd.Hidden[0].Text != "letmein99" {
		t.Fatalf("over a slow link, got %+v", rd.Hidden)
	}
	if !strings.HasPrefix(rd.Hidden[0].Prompt, "[sudo] password for") {
		t.Errorf("the prompt was %q", rd.Hidden[0].Prompt)
	}
}

// vim never echoes a key, because in vim a key is a command. A full screen
// program's keys are not a password typed at a prompt.
func TestVimIsNotAPasswordPrompt(t *testing.T) {
	var s session
	s.out(0, "$ ")
	s.typeLine("vim notes.txt", 0.12, 0.001)
	s.out(0.05, "\x1b[?1049h\x1b[H\x1b[2Jnotes go here\r\n~\r\n~")
	for _, k := range []string{"j", "j", "d", "d", ":", "w", "q", "\r"} {
		s.key(0.15, k)
	}
	s.out(0.05, "\x1b[?1049l$ ")
	if rd := read(s.recording()); len(rd.Hidden) > 0 {
		t.Errorf("vim's keys were reported as hidden: %+v", rd.Hidden)
	}
}

// A screen that scrolls moves a command up a row and puts a new prompt
// where it was. Read row by row, that looks like the command being
// deleted back to the prompt.
func TestScrollingIsNotDeleting(t *testing.T) {
	var s session
	s.out(0, "$ ")
	for i := 0; i < 40; i++ {
		for _, r := range fmt.Sprintf("echo line number %d", i) {
			s.out(0.1, string(r))
		}
		s.out(0.1, fmt.Sprintf("\r\nline number %d\r\n$ ", i))
	}
	if rd := read(s.recording()); len(rd.Outtakes) > 0 {
		t.Errorf("scrolling was read as deleting: %+v", rd.Outtakes)
	}
}

// zsh's answer to ctrl-c is a new line and no ^C, so only the keyboard can
// tell an abandoned line from one that ran.
func TestCtrlCWithoutACaret(t *testing.T) {
	var s session
	s.out(0, "% ")
	for _, r := range "terraform destroy -auto-approve" {
		s.key(0.12, string(r))
		s.out(0.001, string(r))
	}
	s.key(0.3, "\x03")
	s.out(0.001, "\r\r\n% ")
	rd := read(s.recording())
	if len(rd.Outtakes) != 1 || rd.Outtakes[0].Text != "terraform destroy -auto-approve" {
		t.Errorf("got %+v", rd.Outtakes)
	}
}

func TestEveryCastVersionKeepsTime(t *testing.T) {
	v1 := `{"version": 1, "width": 80, "height": 24, "stdout": [[0.5, "a"], [0.25, "b"], [1.0, "c"]]}`
	v2 := "{\"version\": 2, \"width\": 80, \"height\": 24}\n[0.5, \"o\", \"a\"]\n[0.75, \"o\", \"b\"]\n[1.75, \"o\", \"c\"]\n"
	v3 := "{\"version\": 3, \"term\": {\"cols\": 80, \"rows\": 24}}\n# a comment\n[0.5, \"o\", \"a\"]\n[0.25, \"o\", \"b\"]\n[1.0, \"o\", \"c\"]\n"
	for name, body := range map[string]string{"v1": v1, "v2": v2, "v3": v3} {
		rec, err := readCast(name, []byte(body))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(rec.Events) != 3 {
			t.Fatalf("%s: %d events", name, len(rec.Events))
		}
		if at := rec.Events[2].At; math.Abs(at-1.75) > 1e-9 {
			t.Errorf("%s: the last event is at %.3f seconds, not 1.75", name, at)
		}
	}
}

// Given on its own, a script log still finds its timing file beside it.
// On Windows the path given and the folder listing disagree about which
// slash to use, and for a while the timing was never found that way.
func TestTheTimingBesideALogGivenAlone(t *testing.T) {
	s := readSource("testdata/bash-script-advanced")
	if len(s.Recordings) != 1 || s.Recordings[0].Format != "script, advanced timing" {
		t.Fatalf("got %d recordings, gaps %v", len(s.Recordings), s.Gaps)
	}
}

// A file that stops halfway says so. A gap is not a pass.
func TestABrokenFileIsReported(t *testing.T) {
	dir := t.TempDir()
	body, err := os.ReadFile("testdata/bash-asciinema3.cast")
	if err != nil {
		t.Fatal(err)
	}
	cut := append(body[:len(body)/2:len(body)/2], []byte("\n[1.0, \"o\", \"unfinish")...)
	if err := os.WriteFile(filepath.Join(dir, "cut.cast"), cut, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# not a recording\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	study([]*Source{readSource(dir)}, false).writeUp().render(&out)
	if !strings.Contains(out.String(), "UNREAD") {
		t.Errorf("a cast cut off halfway was not reported:\n%s", out.String())
	}
	if strings.Contains(out.String(), "README") {
		t.Error("a README was reported as a recording it could not read")
	}
}

// typist is a made up pair of hands: a time for every pair of keys, which
// is what makes hands tell apart, and day to day noise on top.
type typist struct {
	speed  float64
	pairs  map[string]float64
	spread float64
}

func newTypist(seed int64, speed float64) typist {
	random := rand.New(rand.NewSource(seed))
	tp := typist{speed: speed, pairs: map[string]float64{}, spread: 0.25}
	letters := "abcdefghijklmnopqrstuvwxyz -./"
	for _, a := range letters {
		for _, b := range letters {
			tp.pairs[string(a)+string(b)] = math.Exp(random.NormFloat64() * 0.35)
		}
	}
	return tp
}

var commands = []string{
	"git status", "ls -la", "cd src/project", "make test", "git diff --stat",
	"docker compose up -d", "kubectl get pods", "cat readme.md", "grep -rn todo .",
	"npm run build", "go test ./...", "python3 manage.py migrate", "less output.log",
	"git commit -am fix the parser", "ssh build-box", "tail -f server.log",
}

func (tp typist) session(seed int64, lines int) *Recording {
	random := rand.New(rand.NewSource(seed))
	var s session
	s.out(0, "$ ")
	for i := 0; i < lines; i++ {
		line := commands[random.Intn(len(commands))]
		prev := ' '
		for _, r := range line {
			gap := tp.speed * tp.pairs[string(prev)+string(r)] * math.Exp(random.NormFloat64()*tp.spread)
			s.out(gap, string(r))
			prev = r
		}
		s.out(0.6, "\r\nok\r\n$ ")
	}
	return s.recording()
}

func handsFrom(tp typist, seed int64, recordings int) Hands {
	var rds []*Reading
	for i := 0; i < recordings; i++ {
		rds = append(rds, read(tp.session(seed+int64(i), 30)))
	}
	return handsOf(rds)
}

// The rhythm comparison on hands made up here, which is a test of the
// arithmetic and not of how well it works on people: that is the
// calibration, and its numbers are in the README.
func TestTheSameHandsComeOutCloserThanDifferentOnes(t *testing.T) {
	ana := newTypist(1, 0.16)
	ben := newTypist(2, 0.16)
	same := compareHands(handsFrom(ana, 100, 6), handsFrom(ana, 200, 6))
	other := compareHands(handsFrom(ana, 300, 6), handsFrom(ben, 400, 6))
	if !same.Ready || !other.Ready {
		t.Fatalf("not enough to compare: %+v %+v", same, other)
	}
	if same.Ratio >= other.Ratio {
		t.Errorf("the same hands came out further apart (%.2f) than different ones (%.2f)", same.Ratio, other.Ratio)
	}
	if v, call := handsVerdict(same, handsFrom(ana, 100, 6), handsFrom(ana, 200, 6), "a", "b"); call != sameCall {
		t.Errorf("the same hands were not linked: %s", v)
	}
	if v, call := handsVerdict(other, handsFrom(ana, 300, 6), handsFrom(ben, 400, 6), "a", "b"); call != differentCall {
		t.Errorf("different hands were not told apart: %s", v)
	}
}

// VHS types every key the same number of milliseconds apart. That is no
// rhythm at all, and the report says so instead of comparing it.
func TestMachinePacedTypingIsRecognised(t *testing.T) {
	var s session
	s.out(0, "$ ")
	for i := 0; i < 20; i++ {
		for _, r := range commands[i%len(commands)] {
			s.out(0.05, string(r))
		}
		s.out(0.5, "\r\n$ ")
	}
	if h := handsOf([]*Reading{read(s.recording())}); !h.scripted {
		t.Error("typing at a constant 50ms was taken for a person")
	}
	if h := handsFrom(newTypist(3, 0.15), 500, 2); h.scripted {
		t.Error("a person was taken for a machine")
	}
}
