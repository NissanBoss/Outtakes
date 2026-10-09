package main

// Credentials that were on the screen, typed or printed.
//
// The rules are Hearsay's, which were worked out against real shell
// histories, plus the ones a recording needs and a history does not: a .env
// printed with cat, a webhook secret, a token in a header that a curl -v
// echoed back. Every rule names what it finds. There is deliberately no
// "this string looks random" rule, because a report of maybes is a report
// nobody reads twice.
//
// The value is matched only so it can be covered. Nothing in this program
// prints it, with or without --reveal, and the cover is a fixed width so
// the length is not on the screen either.

import (
	"regexp"
	"sort"
	"strings"
)

type Detector struct {
	Name string
	// Shaped rules find tokens that say what they are by their prefix, and
	// are never a placeholder. The others find a value next to a word like
	// password, which is a placeholder half the time in a tutorial.
	Shaped bool
	// Hint is what a line has to contain, lower case, for the rule to be
	// worth running on it, with | between the choices. One long public
	// recording took half a minute to read because every rule ran over
	// every line, and almost no line contains any of these.
	Hint string
	// Match has exactly one capturing group, around the part that must not
	// reach the screen. A test checks every rule has one, because a rule
	// without it would mask nothing and print everything.
	Match *regexp.Regexp
}

const covered = "<taken-out>"

var detectors = []Detector{
	{Name: "a GitHub token", Shaped: true, Hint: "gh", Match: regexp.MustCompile(`\b(gh[pousr]_[A-Za-z0-9]{36,255})\b`)},
	{Name: "a GitHub fine grained token", Shaped: true, Hint: "github_pat_", Match: regexp.MustCompile(`\b(github_pat_[A-Za-z0-9_]{60,255})\b`)},
	{Name: "a GitLab token", Shaped: true, Hint: "glpat-", Match: regexp.MustCompile(`\b(glpat-[A-Za-z0-9_-]{20,})\b`)},
	{Name: "a Slack token", Shaped: true, Hint: "xox", Match: regexp.MustCompile(`\b(xox[baprse]-[A-Za-z0-9-]{10,})\b`)},
	{Name: "an OpenAI or Anthropic key", Shaped: true, Hint: "sk-", Match: regexp.MustCompile(`\b(sk-(?:ant-|proj-)?[A-Za-z0-9_-]{20,})\b`)},
	{Name: "a Stripe live key", Shaped: true, Hint: "_live_", Match: regexp.MustCompile(`\b((?:sk|rk)_live_[A-Za-z0-9]{16,})\b`)},
	{Name: "a Stripe webhook secret", Shaped: true, Hint: "whsec_", Match: regexp.MustCompile(`\b(whsec_[A-Za-z0-9]{24,})\b`)},
	{Name: "a Google API key", Shaped: true, Hint: "aiza", Match: regexp.MustCompile(`\b(AIza[0-9A-Za-z_-]{35})\b`)},
	{Name: "an npm token", Shaped: true, Hint: "npm_", Match: regexp.MustCompile(`\b(npm_[A-Za-z0-9]{36})\b`)},
	{Name: "a PyPI token", Shaped: true, Hint: "pypi-", Match: regexp.MustCompile(`\b(pypi-AgEIcHlwaS5vcmc[A-Za-z0-9_-]{50,})`)},
	{Name: "a Docker Hub token", Shaped: true, Hint: "dckr_pat_", Match: regexp.MustCompile(`\b(dckr_pat_[A-Za-z0-9_-]{20,})`)},
	{Name: "a Hugging Face token", Shaped: true, Hint: "hf_", Match: regexp.MustCompile(`\b(hf_[A-Za-z0-9]{30,})\b`)},
	{Name: "a DigitalOcean token", Shaped: true, Hint: "_v1_", Match: regexp.MustCompile(`\b(do[opr]_v1_[a-f0-9]{64})\b`)},
	{Name: "a HashiCorp Vault token", Shaped: true, Hint: "hvs.|hvb.|hvr.", Match: regexp.MustCompile(`\b(hv[sbr]\.[A-Za-z0-9_-]{24,})`)},
	{Name: "a SendGrid key", Shaped: true, Hint: "sg.", Match: regexp.MustCompile(`\b(SG\.[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,})`)},
	{Name: "a Shopify token", Shaped: true, Hint: "shp", Match: regexp.MustCompile(`\b(shp(?:at|ca|pa|ss)_[a-fA-F0-9]{32})\b`)},
	{Name: "a Telegram bot token", Shaped: true, Hint: ":aa", Match: regexp.MustCompile(`\b([0-9]{8,10}:AA[A-Za-z0-9_-]{33})\b`)},
	{Name: "an AWS access key id", Shaped: true, Hint: "akia|asia", Match: regexp.MustCompile(`\b((?:AKIA|ASIA)[0-9A-Z]{16})\b`)},
	{Name: "a signed token", Shaped: true, Hint: "eyj", Match: regexp.MustCompile(`\b(eyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,})`)},
	{Name: "a private key", Shaped: true, Hint: "private key", Match: regexp.MustCompile(`(-----BEGIN (?:RSA |DSA |EC |OPENSSH |PGP |ENCRYPTED )?PRIVATE KEY(?: BLOCK)?-----)`)},
	{Name: "an Azure storage key", Shaped: true, Hint: "accountkey=", Match: regexp.MustCompile(`AccountKey=([A-Za-z0-9+/]{80,}={0,2})`)},

	{Name: "an AWS secret key", Hint: "secret", Match: regexp.MustCompile(`(?i)aws_?secret_?access_?key\s*[=:]\s*["']?([A-Za-z0-9/+]{40})\b`)},
	{Name: "a mysql password on the command line", Hint: "mysql", Match: regexp.MustCompile(`(?i)\bmysql(?:dump|admin|show)?\b[^|;&\n]*?\s-p([^\s'"]\S*)`)},
	{Name: "a password given with --password", Hint: "--password", Match: regexp.MustCompile(`(?i)--password[= ]("[^"\n]*"|'[^'\n]*'|[^\s-]\S*)`)},
	{Name: "a password given to sshpass", Hint: "sshpass", Match: regexp.MustCompile(`(?i)\bsshpass\s+-p\s*("[^"\n]*"|'[^'\n]*'|\S+)`)},
	{Name: "a login given to curl with -u", Hint: "curl", Match: regexp.MustCompile(`(?i)\bcurl\b[^|;&\n]*?\s(?:-u|--user)\s+("[^"\n]*:[^"\n]*"|'[^'\n]*:[^'\n]*'|\S+:\S+)`)},
	{Name: "a token in a request header", Hint: "authorization|x-api-key|x-auth-token|private-token", Match: regexp.MustCompile(`(?i)(?:authorization|x-api-key|x-auth-token|private-token)\s*[:=]\s*"?(?:bearer\s+|basic\s+|token\s+)?([A-Za-z0-9._~+/=-]{12,})`)},
	{Name: "a password in a URL", Hint: "://", Match: regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^:/\s@"']+:([^@/\s"']{3,})@`)},
	{Name: "a passphrase given to openssl", Hint: "pass:", Match: regexp.MustCompile(`(?i)-pass(?:in|out)?\s+pass:(\S+)`)},
	{Name: "a passphrase given to gpg", Hint: "--passphrase", Match: regexp.MustCompile(`(?i)--passphrase[= ]("[^"\n]*"|'[^'\n]*'|\S+)`)},
	{Name: "a secret given as --token, --api-key or the like", Hint: "--", Match: regexp.MustCompile(`(?i)--(?:token|api[-_]?key|secret|client[-_]secret|auth[-_]token|access[-_]token)[= ]("[^"\n]*"|'[^'\n]*'|[A-Za-z0-9._~+/=-]{8,})`)},
	{Name: "a password in a CREATE USER or ALTER USER", Hint: "identified", Match: regexp.MustCompile(`(?i)identified\s+(?:with\s+\S+\s+)?by\s+("[^"\n]*"|'[^'\n]*')`)},
	// No word boundary in front of the name. It is nearly always
	// SOMETHING_PASSWORD rather than PASSWORD alone, and the underscore is a
	// word character, so a boundary there misses what turns up in a real
	// .env. The value has to start right after the sign: "password: " with
	// a space and nothing typed yet is a prompt, not a setting.
	{Name: "a setting with a secret in it", Hint: "password|passwd|secret|token|key", Match: regexp.MustCompile(`(?i)[A-Za-z0-9_.-]*(?:password|passwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key)"?\s*=\s?("[^"\n]+"|'[^'\n]+'|[^\s"',;]{6,})`)},
	// The same with a colon, the way YAML and JSON write it, but only when
	// the value ends the line. A colon is also how a status line is written,
	// and the first run over public recordings found "libsecret: ...
	// credential helper [installed]" in three of them: a sentence about a
	// library, not a setting.
	{Name: "a setting with a secret in it", Hint: "password|passwd|secret|token|key", Match: regexp.MustCompile(`(?i)[A-Za-z0-9_.-]*(?:password|passwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key)"?\s*:\s?("[^"\n]+"|'[^'\n]+'|[^\s"',;]{6,})\s*[,}\]]?\s*(?:#.*)?$`)},
}

// placeholder is what a tutorial puts where the secret goes. Recordings are
// mostly tutorials, so most values next to the word password are one of
// these, and reporting them would be telling somebody off for writing a
// good demo.
var placeholder = regexp.MustCompile(`(?i)^(` +
	`change[_-]?me|changeit|password|passwd|secret|token|your[\w.-]*|(?:my|the|some|an?)[-_.][\w.-]*|` +
	`example|examples|sample|test|testing|dummy|placeholder|redacted|hidden|masked|` +
	`xxx+|yyy+|zzz+|aaa+|1234\d*|abc123|foo|bar|baz|qwerty|admin|root|` +
	`none|null|nil|true|false|undefined|empty|unset|required|optional|string|todo|tbd|` +
	`username|user|pass|pwd|secret[-_]?key|api[-_]?key|access[-_]?token|key|value|` +
	// The word for password in the languages help text is most often
	// translated into. A help table in Russian, --password ПАРОЛЬ, came out
	// of the public recordings as a secret.
	`пароль|contraseña|contrasena|clave|passwort|kennwort|motdepasse|mot_de_passe|senha|wachtwoord|hasło|haslo|parola|密码|パスワード|` +
	`\$\{?\w*\}?|\$\(.*\)|<[^>]*>|\[[^\]]*\]|\{[^}]*\}|%\w+%|\*+|•+|\.\.\.|-+|_+|process\.env\.\w+` +
	`)$`)

// reference is a value that points somewhere rather than being the secret:
// a path, a variable, an option.
var reference = regexp.MustCompile(`^(?:/|\./|\.\./|~/|[A-Za-z]:\\|\$|%|@|-|file:)`)

type Secret struct {
	Rule int
	// Line is where it was, already covered, and the only form of it that
	// leaves this file.
	Line string
	// value is the secret itself, kept to tell one secret from another
	// and never printed.
	value string
}

// secretsIn finds the secrets in a piece of text and returns the line each
// was on, already covered.
func secretsIn(text string) []Secret {
	var found []Secret
	for _, line := range strings.Split(text, "\n") {
		hits := detect(line)
		if len(hits) == 0 {
			continue
		}
		covered := cover(line, hits)
		for _, h := range hits {
			found = append(found, Secret{Rule: h.Rule, Line: covered, value: line[h.From:h.To]})
		}
	}
	return found
}

type Hit struct {
	Rule     int
	From, To int
}

func detect(text string) []Hit {
	var hits []Hit
	lower := strings.ToLower(text)
	for i, d := range detectors {
		if !mentions(lower, d.Hint) {
			continue
		}
		for _, at := range d.Match.FindAllStringSubmatchIndex(text, -1) {
			if len(at) < 4 || at[2] < 0 {
				continue
			}
			if !d.Shaped && notReallyASecret(text[at[2]:at[3]]) {
				continue
			}
			hits = append(hits, Hit{Rule: i, From: at[2], To: at[3]})
		}
	}
	return hits
}

func notReallyASecret(value string) bool {
	trimmed := strings.Trim(strings.TrimSpace(value), "\"'`")
	if len([]rune(trimmed)) < 6 {
		return true
	}
	if placeholder.MatchString(trimmed) || reference.MatchString(trimmed) {
		return true
	}
	// user:password, the way curl -u is shown in every example of it.
	if name, pass, ok := strings.Cut(trimmed, ":"); ok && placeholder.MatchString(name) && placeholder.MatchString(pass) {
		return true
	}
	same := true
	for i := 1; i < len(trimmed); i++ {
		if trimmed[i] != trimmed[0] {
			same = false
			break
		}
	}
	return same
}

// cover takes the secrets out of a line. Overlapping matches are normal: a
// URL with a password in it inside a --token argument is caught twice, and
// covering one at a time would cut the line in the wrong place.
func cover(text string, hits []Hit) string {
	if len(hits) == 0 {
		return text
	}
	spans := make([]Hit, len(hits))
	copy(spans, hits)
	sort.Slice(spans, func(i, j int) bool { return spans[i].From < spans[j].From })
	var out []byte
	at := 0
	for _, s := range spans {
		if s.From < at {
			at = max(at, s.To)
			continue
		}
		out = append(out, text[at:s.From]...)
		out = append(out, covered...)
		at = s.To
	}
	return string(out) + text[at:]
}

// coverAll is cover for text whose hits are not already known.
func coverAll(text string) string {
	return cover(text, detect(text))
}

// mentions says whether a line contains one of the choices in a hint.
func mentions(lower, hint string) bool {
	if hint == "" {
		return true
	}
	for hint != "" {
		choice, rest, _ := strings.Cut(hint, "|")
		if strings.Contains(lower, choice) {
			return true
		}
		hint = rest
	}
	return false
}
