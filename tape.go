package main

// VHS tapes.
//
// A tape is not a recording: it is the script VHS follows to make one, and
// it usually sits in the repository right beside the GIF it made. That is
// the leak. Everything typed between Hide and Show runs in the session and
// is kept out of the GIF, which is what Hide is for, and it is all in the
// tape, in plain text, for anybody who opens the folder.

import (
	"bufio"
	"bytes"
	"strconv"
	"strings"
)

type Tape struct {
	// Typed is every string the tape types, in order, with whether the
	// recording was hidden at the time.
	Typed  []TapeLine
	Env    []Field
	Copied []string
	Output []string
	// Erased counts the characters a Backspace took back, which the GIF
	// shows for a moment and the tape keeps for good.
	Erased []TapeLine
}

type TapeLine struct {
	Text   string
	Hidden bool
	Line   int
}

var tapeCommands = map[string]bool{
	"Output": true, "Require": true, "Set": true, "Type": true, "Sleep": true,
	"Backspace": true, "Enter": true, "Hide": true, "Show": true, "Env": true,
	"Ctrl": true, "Screenshot": true, "Copy": true, "Paste": true, "Source": true,
	"Wait": true, "Tab": true, "Up": true, "Down": true, "Left": true, "Right": true,
}

func looksLikeTape(body []byte) bool {
	lines := bufio.NewScanner(bytes.NewReader(body[:min(len(body), 4096)]))
	commands, other := 0, 0
	for lines.Scan() {
		line := strings.TrimSpace(lines.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		word, _, _ := strings.Cut(line, " ")
		word, _, _ = strings.Cut(word, "@")
		word, _, _ = strings.Cut(word, "+")
		if tapeCommands[word] {
			commands++
		} else {
			other++
		}
	}
	return commands >= 2 && other == 0
}

func readTape(path string, body []byte) (*Recording, error) {
	t := &Tape{}
	rec := &Recording{Path: path, Format: "VHS tape", Tape: t}
	hidden := false
	lines := bufio.NewScanner(bytes.NewReader(body))
	n := 0
	for lines.Scan() {
		n++
		line := strings.TrimSpace(lines.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		word, rest, _ := strings.Cut(line, " ")
		command, _, _ := strings.Cut(word, "@")
		switch command {
		case "Hide":
			hidden = true
		case "Show":
			hidden = false
		case "Type":
			t.Typed = append(t.Typed, TapeLine{Text: unquote(rest), Hidden: hidden, Line: n})
		case "Backspace":
			count := 1
			if c, err := strconv.Atoi(strings.TrimSpace(rest)); err == nil {
				count = c
			}
			if len(t.Typed) > 0 && !hidden {
				last := t.Typed[len(t.Typed)-1].Text
				if count > len(last) {
					count = len(last)
				}
				if count > 0 {
					t.Erased = append(t.Erased, TapeLine{Text: last[len(last)-count:], Line: n})
				}
			}
		case "Env":
			name, value, _ := strings.Cut(rest, " ")
			t.Env = append(t.Env, Field{name, unquote(value)})
		case "Copy":
			t.Copied = append(t.Copied, unquote(rest))
		case "Output":
			t.Output = append(t.Output, strings.TrimSpace(rest))
		case "Set":
			name, value, _ := strings.Cut(rest, " ")
			if name == "Shell" {
				rec.about("shell", unquote(value))
			}
		}
	}
	return rec, nil
}

// unquote takes the string out of VHS's quotes, which may be double,
// single or backticks, and are not escaped inside.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && strings.ContainsRune("\"'`", rune(s[0])) && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
