package main

// What a recording says about whose machine it was made on.
//
// None of this is a secret, and most of it was on the screen for anybody
// watching. It is here because people record a demo for a project and
// forget that the prompt says who they are and where they work: the
// account name, the host, the internal domain a command was pointed at.
// For somebody publishing under a name that is not theirs, any one of
// these is the whole game.

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

type Tell struct {
	Kind  string
	Value string
}

var (
	accountAtHost = regexp.MustCompile(`(?:^|[\s\[(])([a-z_][a-z0-9_-]{0,31})@([A-Za-z0-9][A-Za-z0-9-]{0,62})(?:[:\s\])]|$)`)
	homeFolder    = regexp.MustCompile(`(?:/home|/Users|/usr/home)/([A-Za-z0-9._-]+)`)
	windowsHome   = regexp.MustCompile(`(?i)\b[A-Z]:\\Users\\([^\\\s"']+)`)
	internalHost  = regexp.MustCompile(`(?i)\b((?:[a-z0-9-]+\.)+(?:internal|corp|lan|local|localdomain|intranet|intra|home\.arpa|priv|private))\b`)
	ipv4          = regexp.MustCompile(`(?:^|[^\w.])((?:\d{1,3}\.){3}\d{1,3})(?:[^\w.]|\.?$)`)
	emailAddress  = regexp.MustCompile(`\b([A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,})\b`)
	awsAccount    = regexp.MustCompile(`\barn:aws[a-z-]*:[a-z0-9-]*:[a-z0-9-]*:(\d{12}):|"Account":\s*"(\d{12})"`)
)

// Accounts that every machine of a kind has, which say nothing about who
// was at the keyboard. Reporting "ubuntu" on an EC2 box as a tell is the
// sort of thing that teaches people to skip the section.
var ordinaryAccounts = map[string]bool{
	"root": true, "user": true, "ubuntu": true, "ec2-user": true, "admin": true,
	"vagrant": true, "runner": true, "codespace": true, "vscode": true,
	"node": true, "app": true, "git": true, "pi": true, "debian": true,
	"centos": true, "fedora": true, "guest": true, "test": true, "Shared": true,
	"localhost": true, "Public": true, "Default": true, "kali": true, "parrot": true,
	"alpine": true, "osboxes": true, "jovyan": true, "gitpod": true, "coder": true,
	"azureuser": true, "opc": true, "cloud-user": true, "rocky": true, "core": true,
	"docker": true, "postgres": true, "www-data": true, "nobody": true,
}

// Internal names every machine with Docker, Kubernetes or a cloud metadata
// server can resolve. Two folders that both mention host.docker.internal
// have Docker in common, not an owner.
var ordinaryHosts = regexp.MustCompile(`(?i)^(?:localhost\.localdomain|(?:host|gateway|kubernetes)\.docker\.internal|metadata\.google\.internal|.*\.cluster\.local|.*\.svc|kubernetes\.default\..*)$`)

// Hosts that are not anybody's: the loopback, examples, the public
// resolvers people ping to show the network works.
var ordinaryAddresses = map[string]bool{
	"8.8.8.8": true, "8.8.4.4": true, "1.1.1.1": true, "1.0.0.1": true, "9.9.9.9": true,
}

func tellsIn(text string) []Tell {
	var out []Tell
	// Each pattern only runs on a line with the one thing it cannot match
	// without, for the same reason the secret rules carry hints.
	lower := strings.ToLower(text)
	find := func(re *regexp.Regexp, hint string) [][]string {
		if !mentions(lower, hint) {
			return nil
		}
		return re.FindAllStringSubmatch(text, -1)
	}
	findIf := func(ok bool, re *regexp.Regexp) [][]string {
		if !ok {
			return nil
		}
		return re.FindAllStringSubmatch(text, -1)
	}
	add := func(kind, value string) {
		if value != "" {
			out = append(out, Tell{kind, value})
		}
	}
	for _, m := range find(accountAtHost, "@") {
		if !ordinaryAccounts[m[1]] && m[2] != "localhost" && !drawing(m[1]+m[2]) {
			add("account and host", m[1]+"@"+m[2])
		}
	}
	for _, m := range find(homeFolder, "/home/|/users/") {
		if !ordinaryAccounts[m[1]] {
			add("home folder", m[1])
		}
	}
	for _, m := range find(windowsHome, `:\users\`) {
		if !ordinaryAccounts[m[1]] {
			add("home folder", m[1])
		}
	}
	for _, m := range find(internalHost, ".internal|.corp|.lan|.local|.intra|.home.arpa|.priv") {
		if !ordinaryHosts.MatchString(m[1]) {
			add("internal host name", strings.ToLower(m[1]))
		}
	}
	for _, m := range findIf(dotted(text), ipv4) {
		if kind := addressKind(m[1]); kind != "" && !ordinaryAddresses[m[1]] {
			add(kind, m[1])
		}
	}
	for _, m := range find(emailAddress, "@") {
		at := strings.LastIndexByte(m[1], '@')
		local, domain := strings.ToLower(m[1][:at]), strings.ToLower(m[1][at+1:])
		// git@github.com is the start of a remote, not somebody's address,
		// and noreply@ is nobody's.
		if ordinaryAccounts[local] || local == "noreply" || local == "no-reply" || drawing(local+domain) ||
			!knownTLD(domain[strings.LastIndexByte(domain, '.')+1:]) ||
			domain == "example.com" || domain == "example.org" || domain == "example.net" ||
			strings.HasSuffix(domain, ".example") || strings.HasSuffix(domain, ".test") {
			continue
		}
		add("email address", m[1])
	}
	for _, m := range find(awsAccount, `arn:aws|"account"`) {
		add("AWS account", m[1]+m[2])
	}
	return out
}

// addressKind says whether a dotted quad is an address worth mentioning,
// and which kind. Done by hand rather than with the standard library's
// address type, to keep every package whose path starts with net out of a
// program that promises it cannot touch the network.
func addressKind(s string) string {
	var o [4]int
	for i, part := range strings.Split(s, ".") {
		n, err := strconv.Atoi(part)
		if err != nil || n > 255 || (len(part) > 1 && part[0] == '0') {
			return ""
		}
		o[i] = n
	}
	switch {
	case o[0] == 0, o[0] == 127, o[0] >= 224, o[0] == 169 && o[1] == 254:
		return ""
	case o[0] == 10, o[0] == 172 && o[1] >= 16 && o[1] <= 31, o[0] == 192 && o[1] == 168,
		o[0] == 100 && o[1] >= 64 && o[1] <= 127:
		return "private address"
	}
	return "public address"
}

// distinctTells puts tells in a stable order with each one once. A tell
// that is the start of a longer one of the same kind goes: a host name
// typed a key at a time is on the screen half finished first, and
// build01.corp is not a second machine.
func distinctTells(ts []Tell) []Tell {
	seen := map[Tell]bool{}
	var out []Tell
	for _, t := range ts {
		if seen[t] {
			continue
		}
		partial := false
		for _, other := range ts {
			if t.Kind == "internal host name" && other.Kind == t.Kind && strings.HasPrefix(other.Value, t.Value+".") {
				partial = true
				break
			}
		}
		if !partial {
			seen[t] = true
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return tellOrder(out[i].Kind) < tellOrder(out[j].Kind)
		}
		return out[i].Value < out[j].Value
	})
	return out
}

func tellOrder(kind string) int {
	for i, k := range []string{"account and host", "home folder", "email address", "internal host name", "AWS account", "private address", "public address"} {
		if k == kind {
			return i
		}
	}
	return 99
}

var fileURLHost = regexp.MustCompile(`file://([^/\s]+)/`)

// maskTellsIn masks the tells inside a line and leaves the rest of it
// readable. A command with only its host name and account covered still
// says what it did, which is the part somebody needs in order to judge it.
func maskTellsIn(text string) string {
	spans := map[[2]int]string{}
	mark := func(re *regexp.Regexp, how func(m []string) []string) {
		for _, at := range re.FindAllStringSubmatchIndex(text, -1) {
			m := make([]string, len(at)/2)
			for g := range m {
				if at[2*g] >= 0 {
					m[g] = text[at[2*g]:at[2*g+1]]
				}
			}
			for g, masked := range how(m) {
				if g > 0 && masked != "" && at[2*g] >= 0 {
					spans[[2]int{at[2*g], at[2*g+1]}] = masked
				}
			}
		}
	}
	mark(accountAtHost, func(m []string) []string {
		out := []string{"", "", maskWord(m[2])}
		if !ordinaryAccounts[m[1]] {
			out[1] = maskWord(m[1])
		}
		return out
	})
	mark(homeFolder, func(m []string) []string {
		if ordinaryAccounts[m[1]] {
			return nil
		}
		return []string{"", maskWord(m[1])}
	})
	mark(windowsHome, func(m []string) []string {
		if ordinaryAccounts[m[1]] {
			return nil
		}
		return []string{"", maskWord(m[1])}
	})
	mark(internalHost, func(m []string) []string { return []string{"", maskTell(Tell{"internal host name", m[1]})} })
	mark(ipv4, func(m []string) []string {
		if addressKind(m[1]) == "" || ordinaryAddresses[m[1]] {
			return nil
		}
		return []string{"", maskTell(Tell{"public address", m[1]})}
	})
	mark(emailAddress, func(m []string) []string { return []string{"", maskTell(Tell{"email address", m[1]})} })
	mark(awsAccount, func(m []string) []string { return []string{"", "************", "************"} })
	mark(fileURLHost, func(m []string) []string { return []string{"", maskWord(m[1])} })

	starts := make([][2]int, 0, len(spans))
	for s := range spans {
		starts = append(starts, s)
	}
	sort.Slice(starts, func(i, j int) bool { return starts[i][0] < starts[j][0] })
	var b strings.Builder
	at := 0
	for _, s := range starts {
		if s[0] < at {
			continue
		}
		b.WriteString(text[at:s[0]])
		b.WriteString(spans[s])
		at = s[1]
	}
	b.WriteString(text[at:])
	return b.String()
}

// maskTell covers a tell so the report can be pasted somewhere without
// carrying it, keeping the part that explains why it is a tell: the
// .internal on the end of a host name, the shape of an address.
func maskTell(t Tell) string {
	switch t.Kind {
	case "internal host name":
		labels := strings.Split(t.Value, ".")
		for i := range labels[:len(labels)-1] {
			labels[i] = maskWord(labels[i])
		}
		return strings.Join(labels, ".")
	case "private address", "public address":
		parts := strings.Split(t.Value, ".")
		return parts[0] + ".*.*.*"
	case "email address", "account and host":
		name, host, _ := strings.Cut(t.Value, "@")
		return maskWord(name) + "@" + maskText(host)
	}
	return maskWord(t.Value)
}

// maskWord keeps the first and last letter, which is enough for the owner
// to recognise their own word and not enough for anybody else to read it.
func maskWord(s string) string {
	r := []rune(s)
	switch {
	case len(r) == 0:
		return ""
	case len(r) <= 2:
		return string(r[0]) + strings.Repeat("*", len(r)-1)
	}
	return string(r[0]) + strings.Repeat("*", len(r)-2) + string(r[len(r)-1])
}

// maskText masks every word in a line and leaves the punctuation, so a
// command keeps its shape: e***t D*_P******D=<taken-out>. Secrets were
// covered before this and stay covered.
func maskText(s string) string {
	parts := strings.Split(s, covered)
	for i, p := range parts {
		var b strings.Builder
		var word []rune
		flush := func() {
			if len(word) > 0 {
				b.WriteString(maskWord(string(word)))
				word = word[:0]
			}
		}
		for _, r := range p {
			if isWordRune(r) {
				word = append(word, r)
				continue
			}
			flush()
			b.WriteRune(r)
		}
		flush()
		parts[i] = b.String()
	}
	return strings.Join(parts, covered)
}

func isWordRune(r rune) bool {
	return r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r > 0x7f
}

// dotted says whether a line has a run of digits and dots with three dots in
// it, which an address cannot do without. A line with a version number or a
// percentage in it has one dot, and a long build log has thousands of those.
func dotted(s string) bool {
	dots, digits := 0, false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c >= '0' && c <= '9':
			digits = true
		case c == '.' && digits:
			if dots++; dots == 3 {
				return true
			}
		default:
			dots, digits = 0, false
		}
	}
	return false
}

// knownTLD says whether an address ends the way addresses do: in a country,
// which is two letters, or in one of the common names. Without it, systemd's
// getty@tty1.service and a file called somebody@yahoo.com.json were
// addresses.
func knownTLD(tld string) bool {
	return len(tld) == 2 || commonTLDs[tld]
}

var commonTLDs = map[string]bool{}

func init() {
	for _, t := range strings.Fields(`com org net edu gov mil int info biz name pro io dev app ai
		me xyz tech cloud page email online site host green club blog systems network space
		works zone live life world today digital agency studio design codes software run
		social team solutions services company group house one top fun art fyi wiki ninja
		rocks guru center academy foundation institute science energy earth family care
		law money finance capital media news press games chat link click store shop website
		moe pub inc ltd llc eu asia cat coop museum travel jobs mobi tel aero`) {
		commonTLDs[t] = true
	}
}

// drawing says whether something is made of too few different letters to
// be a name. Pictures drawn out of characters are full of @, and the first
// run over public recordings found "addresses" like mmm@mmmmtttt.t.ttttt in
// a dozen animations.
func drawing(s string) bool {
	letters := map[rune]bool{}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			letters[unicode.ToLower(r)] = true
		}
	}
	return len(letters) <= 4
}
