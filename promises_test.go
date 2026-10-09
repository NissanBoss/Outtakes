package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The README makes promises. Promises in a README rot; promises with a test
// behind them break the build first.

// It only reads. A recording somebody is about to publish is the one copy
// of the session they have, and a scanner that touched it would be a worse
// problem than anything it could find.
func TestNeverWrites(t *testing.T) {
	banned := []string{
		"os.WriteFile", "os.Create", "os.OpenFile", "os.Remove", "os.RemoveAll",
		"os.Rename", "os.Truncate", "os.Chmod", "os.Chown", "os.Mkdir",
		"os.MkdirAll", "os.Symlink", "os.Link", "os.Chtimes", "os.CreateTemp",
		"ioutil.WriteFile",
	}
	scanSource(t, banned, "this tool must not write anything")
}

// No network code at all: not a promise not to send anything, but no way
// to. The quote in front catches every package whose path starts with net,
// the harmless ones included, so nobody has to take anybody's word for
// which are harmless.
func TestNeverOpensTheNetwork(t *testing.T) {
	banned := []string{`"net`, "net.Dial", "http.Get", "http.Post", "syscall.Socket"}
	scanSource(t, banned, "this tool must have no way to send anything anywhere")
}

func TestNeverRunsAnotherProgram(t *testing.T) {
	scanSource(t, []string{`"os/exec"`, "syscall.Exec", "syscall.ForkExec", "StartProcess"},
		"this tool must not run another program")
}

// And there is no model in it. Nothing is downloaded, nothing is trained,
// and every comparison is arithmetic somebody can redo by hand.
func TestNoModelAnywhere(t *testing.T) {
	banned := []string{"onnx", "gguf", "tensor", "transformers", "go:embed"}
	scanSource(t, banned, "the method here is arithmetic and stays that way")
}

func scanSource(t *testing.T, banned []string, why string) {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	looked := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		looked++
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range banned {
			if bytes.Contains(src, []byte(bad)) {
				t.Errorf("%s contains %q: %s", name, bad, why)
			}
		}
	}
	if looked == 0 {
		t.Fatal("no source was read, so this test proved nothing")
	}
}

// The long dash is not used anywhere in this project, in code, comments,
// documentation or anything the program prints. Written as a number here
// so this file passes its own test. Binaries are skipped: a build left in
// the folder carries the standard library's strings, not ours.
func TestNoLongDash(t *testing.T) {
	dash := []byte(string(rune(0x2014)))
	looked := 0
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != "." && (strings.HasPrefix(d.Name(), ".") && d.Name() != ".github" || d.Name() == "dist") {
				return fs.SkipDir
			}
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.IndexByte(body, 0) >= 0 {
			return nil
		}
		looked++
		if bytes.Contains(body, dash) {
			t.Errorf("%s has a long dash in it", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if looked < 10 {
		t.Fatalf("only %d files were looked at", looked)
	}
}

// Every rule masks exactly one part of what it matches. A rule with no
// group would match the secret and cover nothing, which is worse than not
// having the rule.
func TestEveryRuleCoversSomething(t *testing.T) {
	for _, d := range detectors {
		if d.Match.NumSubexp() != 1 {
			t.Errorf("%q has %d groups; it needs exactly one, around the secret", d.Name, d.Match.NumSubexp())
		}
	}
}

// What the report must never contain, with or without --reveal: the
// password typed at a prompt that hid it, the one typed and rubbed out, and
// the ones on the screen. The report is meant to be pasted into an issue.
func TestSecretsNeverReachTheReport(t *testing.T) {
	never := []string{
		"Tr0ub4dor", "hunter2-correct-staple", "whsec-placeholder-for-tests",
		"correct-horse-battery-staple",
	}
	for _, reveal := range []bool{false, true} {
		out := reportOn(t, reveal, "testdata")
		for _, secret := range never {
			if strings.Contains(out, secret) {
				t.Errorf("reveal=%v: the report printed %q", reveal, secret)
			}
		}
	}
}

// Without --reveal, the report says what kind of thing was found and not
// what it was, so it can be pasted somewhere as it is.
func TestTheMaskedReportNamesNothing(t *testing.T) {
	out := reportOn(t, false, "testdata")
	for _, name := range []string{"demo@lab", "db01.acme", "build01", "/home/demo", "file://lab", "acme-prod"} {
		if strings.Contains(out, name) {
			t.Errorf("the masked report printed %q", name)
		}
	}
	revealed := reportOn(t, true, "testdata")
	for _, name := range []string{"demo@lab", "build01.corp.acme.internal", "UTC-05:00"} {
		if !strings.Contains(revealed, name) {
			t.Errorf("--reveal did not print %q", name)
		}
	}
}

func reportOn(t *testing.T, reveal bool, where ...string) string {
	t.Helper()
	var sources []*Source
	for _, w := range where {
		sources = append(sources, readSource(w))
	}
	var out bytes.Buffer
	study(sources, reveal).writeUp().render(&out)
	return out.String()
}

// The shaped rules, against tokens put together here from pieces so that
// nothing shaped like a live key exists in the repository for a scanner to
// trip on, this one included.
func TestShapedTokensAreFound(t *testing.T) {
	letters := "aB3dE5gH7jK9mN1pQ2sT4vW6yZ8bC0fG"
	cases := map[string]string{
		"a GitHub token":             "gh" + "p_" + letters + "Xy12",
		"a Stripe live key":          "sk" + "_live_" + letters[:24],
		"a Stripe webhook secret":    "wh" + "sec_" + letters,
		"an AWS access key id":       "AK" + "IA" + "QWERTYUIOPASDFGH",
		"a Slack token":              "xo" + "xb-" + "1234567890-" + letters[:12],
		"an OpenAI or Anthropic key": "sk" + "-proj-" + letters,
		"a Google API key":           "AI" + "za" + letters + "abc",
	}
	for name, token := range cases {
		found := false
		for _, s := range secretsIn("export THING=" + token + " # done") {
			if detectors[s.Rule].Name == name {
				found = true
				if strings.Contains(s.Line, token) {
					t.Errorf("%s was found and printed anyway: %s", name, s.Line)
				}
			}
		}
		if !found {
			t.Errorf("%s was not found", name)
		}
	}
}

// What a tutorial puts where a secret goes is not a secret, and saying it
// is would be telling somebody off for writing a good demo.
func TestPlaceholdersAreNotSecrets(t *testing.T) {
	for _, line := range []string{
		"export API_TOKEN=your-token-here",
		"export DB_PASSWORD=$DB_PASSWORD",
		"password: ********",
		"[sudo] password for demo:",
		"Password:",
		"docker login --password-stdin",
		"API_KEY=<api-key>",
		"token: ${{ secrets.GITHUB_TOKEN }}",
		"--password changeme",
		// From the public recordings: a status line, not a setting.
		"libsecret: gnome-keyring credential helper [installed]",
		"| register --username NAME --password ПАРОЛЬ |",
		"curl --anyauth --user username:password https://example.com",
	} {
		if found := secretsIn(line); len(found) > 0 {
			t.Errorf("%q was reported as %s", line, detectors[found[0].Rule].Name)
		}
	}
}

// And the settings that are secrets, in the shapes they turn up in on a
// screen: an env line, YAML, JSON, a table drawn with box characters.
func TestSettingsWithSecretsAreFound(t *testing.T) {
	for _, line := range []string{
		"DB_PASSWORD=hunter2-correct-staple && ls",
		"apiKey: abcd1234efgh",
		"│ apiKey: abcd1234efgh",
		`  "password": "s3cr3tvalue",`,
		"password=thunderbird42",
	} {
		if len(secretsIn(line)) == 0 {
			t.Errorf("%q was not found", line)
		}
	}
}
