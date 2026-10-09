package main

// Outtakes reads terminal recordings for what their authors did not mean
// to publish.
//
// A terminal recording looks like a video and is not one. It is the whole
// session as text: every key and every echo, timed to the millisecond, so
// the line typed and rubbed out before anybody pressed enter is in the
// file, and so is the password typed at a prompt that showed nothing, if
// the recorder was saving the keyboard. The window titles the shell set,
// which no player draws, are in it. So is the start time to the second,
// and from the clock in a prompt the time zone, and in the gaps between
// keys the rhythm of the hands that typed it.
//
// This plays the recording back through a small terminal and says what was
// on the screen and stopped being on it, what was in the file and never on
// the screen, and, given two sets of recordings, what ties one to the
// other. It only reads. Nothing is written, nothing is sent anywhere, and
// there is no model in it.

import (
	"flag"
	"fmt"
	"os"
)

var version = "unreleased"

const (
	exitClean  = 0
	exitFound  = 1 // something kept, exposed, or tying two names together
	exitBroken = 2
	exitUnsure = 3 // nothing was read, which is not the same as nothing found
)

func main() {
	reveal := flag.Bool("reveal", false, "print account names, hosts and times instead of masking them (secrets stay covered)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Usage = usage
	flag.Parse()

	if *showVersion {
		fmt.Println("Outtakes " + version)
		return
	}
	if flag.NArg() == 0 {
		usage()
		os.Exit(exitBroken)
	}

	var sources []*Source
	for _, where := range flag.Args() {
		sources = append(sources, readSource(where))
	}
	s := study(sources, *reveal)
	report := s.writeUp()
	report.render(os.Stdout)

	switch {
	case s.recordings() == 0:
		os.Exit(exitUnsure)
	case report.worst() >= Keeps:
		os.Exit(exitFound)
	}
	os.Exit(exitClean)
}

func usage() {
	fmt.Fprintln(os.Stderr, `Outtakes - what a terminal recording kept

  outtakes demo.cast                 an asciinema recording, any version
  outtakes typescript                script(1), with its timing file beside it
  outtakes session.ttyrec
  outtakes demo.svg                  an animated SVG from svg-term or termtosvg
  outtakes demo.tape                 a VHS tape
  outtakes docs/                     every recording in a folder
  outtakes mine/ other-name/         two sets of recordings: what ties them
  outtakes --reveal ...              print names, hosts and times unmasked
  outtakes --version

It plays each recording back and reports what was typed and taken back,
passwords the keyboard recorded at prompts that showed nothing, secrets on
the screen, window titles and other things no player draws, whose machine
it was, and the time zone. Given two folders, it says what ties one to the
other, the rhythm of the typing included.

Secrets are always covered. Names, hosts and times are masked unless you
pass --reveal, so the report can be pasted somewhere as it is.

It only reads. Nothing is written, nothing is sent anywhere, and there is
no model in it.`)
}
