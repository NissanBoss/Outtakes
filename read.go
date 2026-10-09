package main

// Finding the recordings in whatever somebody points this at.
//
// A folder is walked for every format this reads, recognised by what the
// file holds rather than what it is called: a recording renamed to .txt
// carries the same password it always did. The one pairing that has to be
// guessed is script's, which writes the session and its timing to two
// files; the timing is looked for beside the log under the names script's
// own documentation uses.

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Source struct {
	Name string
	// Root is the folder that was given, so recordings can be named by
	// their place inside it. It is empty when a single file was given.
	Root       string
	Recordings []*Recording
	Gaps       []string
}

// tooBig is the largest file this reads. A day-long ttyrec is tens of
// megabytes; anything past this is not a demo.
const tooBig = 512 << 20

func readSource(where string) *Source {
	s := &Source{Name: filepath.Base(strings.TrimRight(where, `/\`))}
	stat, err := os.Stat(where)
	if err != nil {
		s.Gaps = append(s.Gaps, err.Error())
		return s
	}
	if !stat.IsDir() {
		if rec, err := readOne(where, nil); err != nil {
			s.Gaps = append(s.Gaps, fmt.Sprintf("%s: %v", where, err))
		} else if rec != nil {
			s.Recordings = append(s.Recordings, rec)
		} else {
			s.Gaps = append(s.Gaps, fmt.Sprintf("%s is not a recording this reads", where))
		}
		return s
	}

	s.Root = where
	var files []string
	err = filepath.WalkDir(where, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			s.Gaps = append(s.Gaps, err.Error())
			return nil
		}
		if d.IsDir() {
			if path != where && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules") {
				return fs.SkipDir
			}
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		s.Gaps = append(s.Gaps, err.Error())
	}
	sort.Strings(files)
	timings := map[string]bool{}
	for _, f := range files {
		if t := timingFor(f, files); t != "" {
			timings[t] = true
		}
	}
	for _, f := range files {
		if timings[f] {
			continue
		}
		rec, err := readOne(f, files)
		switch {
		case err != nil:
			s.Gaps = append(s.Gaps, fmt.Sprintf("%s: %v", f, err))
		case rec != nil:
			s.Recordings = append(s.Recordings, rec)
		}
	}
	return s
}

// readOne reads a file if it is a recording, and returns nothing at all if
// it is not, so a folder can hold a README and a Makefile beside its
// demos without either being reported.
func readOne(path string, siblings []string) (*Recording, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if stat.Size() > tooBig {
		return nil, fmt.Errorf("%d MB, more than this will read", stat.Size()>>20)
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".mp4", ".webm", ".mov", ".zip", ".gz", ".exe":
		return nil, nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	switch {
	case ext == ".tape" || looksLikeTape(body):
		return readTape(path, body)
	case ext == ".svg" || looksLikeSVG(body):
		return readSVG(path, body)
	case ext == ".cast" || looksLikeCast(body):
		rec, err := readCast(path, body)
		if err != nil && ext != ".cast" {
			// JSON that is not a recording, a package.json beside the
			// demos: not ours, and not worth a line in the report.
			return nil, nil
		}
		return rec, err
	case looksLikeTypescript(body):
		var timing []byte
		if t := timingFor(path, siblings); t != "" {
			timing, err = os.ReadFile(t)
			if err != nil {
				return nil, err
			}
		}
		return readTypescript(path, body, timing)
	case ext == ".ttyrec" || ext == ".tty" || looksLikeTtyrec(body):
		return readTtyrec(path, body)
	}
	return nil, nil
}

// timingFor finds the timing log that goes with a script log, if there is
// one beside it.
func timingFor(path string, siblings []string) string {
	if siblings == nil {
		dir := filepath.Dir(path)
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			siblings = append(siblings, filepath.Join(dir, e.Name()))
		}
	}
	base := strings.TrimSuffix(path, filepath.Ext(path))
	candidates := []string{path + ".timing", path + ".tm", path + ".time", base + ".timing", base + ".tm", base + ".time"}
	if filepath.Base(path) == "typescript" {
		candidates = append(candidates, filepath.Join(filepath.Dir(path), "timing"))
	}
	for _, c := range candidates {
		for _, s := range siblings {
			// Cleaned on both sides: on Windows a path given as a.b/c meets
			// a listing that says a.b\c, and the timing beside the log was
			// never found when only the log was named.
			if filepath.Clean(s) == filepath.Clean(c) && filepath.Clean(c) != filepath.Clean(path) {
				if body, err := os.ReadFile(c); err == nil && looksLikeTiming(body) {
					return c
				}
			}
		}
	}
	return ""
}
