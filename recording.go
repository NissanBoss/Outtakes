package main

// What a recording is, whichever program made it.
//
// Every format here comes down to the same thing: what the terminal was
// sent, when, and sometimes what the keyboard sent back. The rest of the
// program only ever sees that, so a ttyrec from 2003 and an asciicast from
// last week are read by the same code.

import "time"

type Event struct {
	// At is seconds from the start of the recording.
	At float64
	// Kind is 'o' for what the terminal was sent, 'i' for what the
	// keyboard sent, 'r' for a resize and 'm' for a marker, and Sent
	// is what it was: the bytes, the keys, the new size.
	Kind byte
	Sent string
}

type Recording struct {
	Path   string
	Format string
	Cols   int
	Rows   int
	// Started is when the recording began, in UTC, if the file says.
	Started time.Time
	// Zone is the recorder's offset from UTC in minutes, when the file
	// states it outright, which script(1) does on its first line.
	Zone *int
	// About is what the file says about itself, which no player shows:
	// the command, the shell, the terminal and its version, the
	// environment the recorder captured, the paths it logged to.
	About  []Field
	Events []Event
	// Frames is the screen as text, frame by frame, for formats that keep
	// pictures rather than a stream: an animated SVG.
	Frames []Frame
	// Tape is set for a VHS script, which is not a recording at all but
	// the instructions for making one.
	Tape *Tape
	Gaps []string
}

type Field struct{ Name, Value string }

type Frame struct {
	At   float64
	Rows []Row
}

// Row is one line of a frame and where it sits, so the same line can be
// followed from one frame to the next even when the rows above it are
// blank and drawn as nothing.
type Row struct {
	Y    float64
	Text string
}

// about files a fact once. script states the command and the terminal in
// its first line and again in the timing log, and both are the same fact.
func (r *Recording) about(name, value string) {
	if value == "" {
		return
	}
	for _, f := range r.About {
		if f.Name == name && f.Value == value {
			return
		}
	}
	r.About = append(r.About, Field{name, value})
}

func (r *Recording) hasInput() bool {
	for _, e := range r.Events {
		if e.Kind == 'i' {
			return true
		}
	}
	return false
}

func (r *Recording) duration() float64 {
	if n := len(r.Events); n > 0 {
		return r.Events[n-1].At
	}
	if n := len(r.Frames); n > 0 {
		return r.Frames[n-1].At
	}
	return 0
}
