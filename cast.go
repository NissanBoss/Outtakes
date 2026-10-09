package main

// asciicast, the format asciinema writes, in all three versions.
//
// Version 1 is one JSON object with the output as a list of [delay, text]
// pairs. Versions 2 and 3 are a header line and then one event per line;
// the difference that matters is the clock: version 2 stamps every event
// with the time since the start, version 3 with the time since the event
// before it. Reading a version 3 file as version 2 puts the whole session
// inside its first second, and nothing fails.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// looksLikeCast checks the start of the file only. asciinema 1 wrote its
// object with the keys sorted and indented, so "version" can be the last
// thing in a file of megabytes and "stdout" is what comes early; fourteen
// of the public recordings were skipped as not recordings at all before
// this looked for either.
func looksLikeCast(body []byte) bool {
	head := bytes.TrimSpace(body[:min(len(body), 4096)])
	return bytes.HasPrefix(head, []byte("{")) &&
		(bytes.Contains(head, []byte(`"version"`)) || bytes.Contains(head, []byte(`"stdout"`)))
}

func readCast(path string, body []byte) (*Recording, error) {
	rec := &Recording{Path: path}
	lines := bufio.NewScanner(bytes.NewReader(body))
	lines.Buffer(make([]byte, 1<<20), 1<<28)
	if !lines.Scan() {
		return nil, fmt.Errorf("empty")
	}
	first := lines.Bytes()

	var head struct {
		Version   int                  `json:"version"`
		Width     int                  `json:"width"`
		Height    int                  `json:"height"`
		Timestamp float64              `json:"timestamp"`
		Command   string               `json:"command"`
		Title     string               `json:"title"`
		Env       map[string]any       `json:"env"`
		IdleLimit float64              `json:"idle_time_limit"`
		Tags      []string             `json:"tags"`
		Stdout    [][2]json.RawMessage `json:"stdout"`
		Theme     *theme               `json:"theme"`
		Term      struct {
			Cols    int    `json:"cols"`
			Rows    int    `json:"rows"`
			Type    string `json:"type"`
			Version string `json:"version"`
			Theme   *theme `json:"theme"`
		} `json:"term"`
	}
	if err := json.Unmarshal(first, &head); err != nil {
		// Version 1 is a single object that may span many lines.
		if err := json.Unmarshal(body, &head); err != nil || head.Version != 1 {
			return nil, fmt.Errorf("not an asciicast: %v", err)
		}
	}

	rec.Format = fmt.Sprintf("asciicast v%d", head.Version)
	rec.Cols, rec.Rows = head.Width, head.Height
	if head.Version == 3 {
		rec.Cols, rec.Rows = head.Term.Cols, head.Term.Rows
		rec.about("terminal", head.Term.Type)
		rec.about("terminal version", head.Term.Version)
		head.Theme = head.Term.Theme
	}
	if head.Theme != nil {
		rec.about("colour scheme", head.Theme.String())
	}
	if head.Timestamp > 0 {
		rec.Started = time.Unix(int64(head.Timestamp), 0).UTC()
	}
	rec.about("command", head.Command)
	rec.about("title", head.Title)
	names := make([]string, 0, len(head.Env))
	for k := range head.Env {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if v, ok := head.Env[k].(string); ok {
			rec.about("environment "+k, v)
		}
	}
	if len(head.Tags) > 0 {
		rec.about("tags", strings.Join(head.Tags, ", "))
	}

	switch head.Version {
	case 1:
		at := 0.0
		for _, pair := range head.Stdout {
			var delay float64
			var text string
			if json.Unmarshal(pair[0], &delay) != nil || json.Unmarshal(pair[1], &text) != nil {
				continue
			}
			at += delay
			rec.Events = append(rec.Events, Event{At: at, Kind: 'o', Sent: text})
		}
	case 2, 3:
		at := 0.0
		bad := 0
		for lines.Scan() {
			line := bytes.TrimSpace(lines.Bytes())
			if len(line) == 0 || line[0] == '#' {
				continue
			}
			var ev [3]json.RawMessage
			var t float64
			var code, sent string
			if json.Unmarshal(line, &ev) != nil || json.Unmarshal(ev[0], &t) != nil ||
				json.Unmarshal(ev[1], &code) != nil || json.Unmarshal(ev[2], &sent) != nil || code == "" {
				bad++
				continue
			}
			if head.Version == 3 {
				at += t
			} else {
				at = t
			}
			rec.Events = append(rec.Events, Event{At: at, Kind: code[0], Sent: sent})
		}
		if bad > 0 {
			rec.Gaps = append(rec.Gaps, fmt.Sprintf("%s: %d lines were not events and were skipped", path, bad))
		}
	default:
		return nil, fmt.Errorf("asciicast version %d is not one this reads", head.Version)
	}
	if rec.Cols == 0 || rec.Rows == 0 {
		rec.Cols, rec.Rows = 80, 24
	}
	return rec, nil
}

// theme is the terminal's colours, which asciinema 3 asks the terminal for
// and writes into the header so the player can match them. A default
// palette is shared by millions. A palette somebody tuned by hand is
// shared by nobody else, and it is in every recording they make.
type theme struct {
	Fg      string `json:"fg"`
	Bg      string `json:"bg"`
	Palette string `json:"palette"`
}

func (t *theme) String() string {
	if t.Fg == "" && t.Bg == "" && t.Palette == "" {
		return ""
	}
	colours := len(strings.Split(t.Palette, ":"))
	if t.Palette == "" {
		colours = 0
	}
	return fmt.Sprintf("text %s on %s, %d colours %s", t.Fg, t.Bg, colours, t.Palette)
}
