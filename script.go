package main

// script(1) and ttyrec, the two formats that predate asciinema and are
// still what people reach for on a server.
//
// script writes the raw output to one file and, if asked, the timing to
// another: each timing line says how long to wait and how many bytes of
// the log to play. Its first line is the more interesting part. "Script
// started on 2026-10-09 14:18:11-05:00" is a local time with the offset
// written after it, which is where the recorder was sitting.

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var scriptHeader = regexp.MustCompile(`^Script started on (\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2})([+-]\d{2}:?\d{2})?(?: \[(.*)\])?`)

func looksLikeTypescript(body []byte) bool {
	return bytes.HasPrefix(body, []byte("Script started on "))
}

// looksLikeTiming says whether a file is a script(1) timing log, in either
// of its two formats: "0.713927 1" or "O 0.713927 1".
func looksLikeTiming(body []byte) bool {
	lines := bufio.NewScanner(bytes.NewReader(body[:min(len(body), 4096)]))
	n := 0
	for lines.Scan() && n < 20 {
		f := strings.Fields(lines.Text())
		switch {
		case len(f) == 2 && isFloat(f[0]) && isInt(f[1]):
		case len(f) >= 3 && len(f[0]) == 1 && strings.Contains("OISH", f[0]) && isFloat(f[1]):
		default:
			return false
		}
		n++
	}
	return n > 0
}

func readTypescript(path string, body, timing []byte) (*Recording, error) {
	rec := &Recording{Path: path, Format: "script", Cols: 80, Rows: 24}
	end := bytes.IndexByte(body, '\n')
	if end < 0 {
		return nil, fmt.Errorf("no header line")
	}
	scriptStarted(rec, string(body[:end]))
	log := body[end+1:]
	// The trailer "Script done on ..." is not part of the session.
	if i := bytes.LastIndex(log, []byte("\nScript done on ")); i >= 0 {
		log = log[:i]
	}

	if timing == nil {
		rec.Events = []Event{{Kind: 'o', Sent: string(log)}}
		rec.Gaps = append(rec.Gaps, path+": no timing file beside it, so the order is known and the timing is not")
		return rec, nil
	}

	at := 0.0
	pos := 0
	lines := bufio.NewScanner(bytes.NewReader(timing))
	lines.Buffer(make([]byte, 1<<16), 1<<20)
	for lines.Scan() {
		f := strings.Fields(lines.Text())
		if len(f) == 0 {
			continue
		}
		kind := byte('o')
		if len(f[0]) == 1 && strings.Contains("OISH", f[0]) {
			rec.Format = "script, advanced timing"
			kind = strings.ToLower(f[0])[0]
			f = f[1:]
		}
		if len(f) < 2 {
			continue
		}
		delay, _ := strconv.ParseFloat(f[0], 64)
		at += delay
		switch kind {
		case 'h':
			scriptInfo(rec, f[1], strings.Join(f[2:], " "))
			continue
		case 's':
			continue
		}
		n, err := strconv.Atoi(f[1])
		if err != nil || n < 0 {
			continue
		}
		if pos+n > len(log) {
			rec.Gaps = append(rec.Gaps, path+": the timing file asks for more than the log holds, so the end is missing")
			break
		}
		rec.Events = append(rec.Events, Event{At: at, Kind: kind, Sent: string(log[pos : pos+n])})
		pos += n
	}
	return rec, nil
}

func scriptStarted(rec *Recording, line string) {
	m := scriptHeader.FindStringSubmatch(line)
	if m == nil {
		return
	}
	stamp := strings.Replace(m[1], " ", "T", 1)
	if m[2] != "" {
		zone := strings.Replace(m[2], ":", "", 1)
		if t, err := time.Parse("2006-01-02T15:04:05-0700", stamp+zone); err == nil {
			rec.Started = t.UTC()
			_, off := t.Zone()
			minutes := off / 60
			rec.Zone = &minutes
		}
	}
	for _, kv := range quotedFields.FindAllStringSubmatch(m[3], -1) {
		scriptInfo(rec, kv[1], kv[2])
	}
}

var quotedFields = regexp.MustCompile(`(\w+)="([^"]*)"`)

// scriptInfo files one of the facts script records about the session. The
// log paths are the surprising ones: they are wherever the recording was
// written on the machine that made it, home directory included.
func scriptInfo(rec *Recording, name, value string) {
	switch name {
	case "START_TIME":
		scriptStarted(rec, "Script started on "+value)
	case "COLUMNS":
		rec.Cols, _ = strconv.Atoi(value)
	case "LINES":
		rec.Rows, _ = strconv.Atoi(value)
	case "COMMAND":
		rec.about("command", value)
	case "TERM":
		rec.about("terminal", value)
	case "TTY":
		rec.about("tty", value)
	case "SHELL":
		rec.about("shell", value)
	case "TIMING_LOG", "OUTPUT_LOG", "INPUT_LOG":
		rec.about("logged to", value)
	}
}

// readTtyrec reads ttyrec's records: seconds, microseconds and a length,
// each a little-endian 32 bit number, then that many bytes of output. The
// times are absolute, so the first one is when the recording started.
func readTtyrec(path string, body []byte) (*Recording, error) {
	rec := &Recording{Path: path, Format: "ttyrec", Cols: 80, Rows: 24}
	var first float64
	for pos := 0; pos+12 <= len(body); {
		sec := binary.LittleEndian.Uint32(body[pos:])
		usec := binary.LittleEndian.Uint32(body[pos+4:])
		n := int(binary.LittleEndian.Uint32(body[pos+8:]))
		pos += 12
		if n < 0 || pos+n > len(body) || usec >= 1000000 {
			rec.Gaps = append(rec.Gaps, path+": a record runs past the end of the file, so the end is missing")
			break
		}
		at := float64(sec) + float64(usec)/1e6
		if len(rec.Events) == 0 {
			first = at
			rec.Started = time.Unix(int64(sec), int64(usec)*1000).UTC()
		}
		rec.Events = append(rec.Events, Event{At: at - first, Kind: 'o', Sent: string(body[pos : pos+n])})
		pos += n
	}
	if len(rec.Events) == 0 {
		return nil, fmt.Errorf("no records")
	}
	return rec, nil
}

// looksLikeTtyrec checks the first two records hang together: a plausible
// time, a microsecond count under a million, a length that fits.
func looksLikeTtyrec(body []byte) bool {
	pos := 0
	for i := 0; i < 2; i++ {
		if pos+12 > len(body) {
			return i > 0
		}
		sec := binary.LittleEndian.Uint32(body[pos:])
		usec := binary.LittleEndian.Uint32(body[pos+4:])
		n := int(binary.LittleEndian.Uint32(body[pos+8:]))
		if sec < 315532800 || usec >= 1000000 || n < 0 || pos+12+n > len(body) {
			return false
		}
		pos += 12 + n
	}
	return true
}

func isFloat(s string) bool {
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

func isInt(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}
