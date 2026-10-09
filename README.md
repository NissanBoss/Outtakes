# Outtakes

What a terminal recording kept that you did not mean to publish.

```
    [HIDDEN ] demo.cast kept a password the screen never showed
              16.7s   typed at "Password:"

    [TYPED  ] demo.cast kept 2 lines typed and taken back, one with a secret in it
              12.2s   typed and deleted before it was run (a setting with a secret in it)
                      export DB_PASSWORD=<taken-out>
              24.4s   typed and abandoned with ctrl-c
                      rm -rf /tmp/old-builds

    [SECRET ] demo.cast showed a secret
              19.8s   a setting with a secret in it, on the screen
                      STRIPE_WEBHOOK_SECRET=<taken-out>

    [UNSEEN ] Things no player shows are in demo.cast
              window title, 2 values
                  d**o@l*b: ~
                  d**o@l*b: ~/work/acme-billing

    [TELLS  ] Whose machine it was, according to the screen in demo.cast
              account and host     d**o@l*b
              internal host name   b*****1.c**p.a**e.internal
              internal host name   d**1.a**e.internal
```

That is a half minute demo, made for this README with asciinema 2 and its
`--stdin` flag. Played back, it shows a few commands and nothing anybody
would think twice about.

## The problem

A terminal recording looks like a video and is not one. asciinema, script
and ttyrec save the session as text: every byte the terminal was sent,
stamped to the millisecond, and if they were asked to, every key. A player
turns that back into something that looks like a screen, and the file keeps
everything the screen ever showed, in order, including what was only there
for a moment.

So the line typed and killed with ctrl-u before pressing enter is in the
file. It never reached the shell's history, which is why it felt safe to
type, and it is one step back in the playback for anybody. A password typed
at a prompt that showed nothing is in the file too, if the recorder was
saving the keyboard: asciinema does that with `--capture-input` (or
`--stdin` before version 3), or for every recording when
`session.capture_input` is set in its config, and script does it with `-I`
and `-B`. The playback looks clean because the screen was clean.

Then there is what the file carries and no player draws. Shells set the
window title before every prompt, usually to account@host and the folder,
and the recording keeps every one. asciinema 3 writes down the terminal's
name and exact version and its colour scheme. The start is stamped to the
second, and a clock in the prompt gives away the time zone.

And there are the gaps between keys. How long somebody takes from one key to
the next, pair by pair, is a biometric that keystroke dynamics has studied
for decades, and every recording typed live carries it: in the keys, when
the keyboard was saved, or in the echoes, which come back a millisecond after
each key whether it was saved or not. Two recordings published under two
names can be tied together by the hands that typed them.

The animated SVG that goes in a README instead of a GIF is the same file
again. svg-term and termtosvg lay out every frame of the session as text in
one long picture and slide it past a window, so everything that was on the
screen for a tenth of a second is in the SVG, as selectable as the last
frame.

None of this is rare. Of 858 public recordings on asciinema.org, from 110
accounts, half carried a window title nobody watching ever saw, nearly two
in five showed an account@host, more than four in five stamped the second
they started, one in seven gave its time zone away, and one in fifteen kept
a line typed and taken back. About one in six had enough live typing to be
set against the same person's other recordings.

## What already exists

asciinema's own [FAQ](https://docs.asciinema.org/faq/) answers "Does
asciinema record the passwords I type?": not by default, yes with input
capture on. It leaves you to remember which way a recording was made.
Nothing else in it covers what a recording keeps.

[cast-redact](https://www.skills.sh/paldom/terminaltor/cast-redact) is the
closest thing, and it is a fixer: it runs regular expressions over a cast's
joined output to scrub matches out. It has to be told what to look for, it
reads only asciicast, and it cannot know that a line was deleted, that a
title was never drawn, or that the keys of a password were saved.

Secret scanners like [gitleaks](https://github.com/gitleaks/gitleaks) and
[TruffleHog](https://github.com/trufflesecurity/trufflehog) read a file as
text, and a token typed into a recording is one character per line of the
file. gitleaks 8.28 finds a GitHub token printed into a cast in one piece,
and finds nothing when the same token is typed into it.

[asciinema-edit](https://github.com/cirocosta/asciinema-edit) and
[asciicut](https://pypi.org/project/asciicut/) cut and trim recordings, once
you know what to cut. [par-term](https://docs.rs/crate/par-term/latest)
masks passwords as it writes its own session logs. And keystroke dynamics is
an old field, [Gunetti and Picardi](https://doi.org/10.1145/1085126.1085129)
showed in 2005 that free text typed in different sessions identifies the
typist, but nothing I found points it at the recordings people publish.

Outtakes reads a recording the way a player does and says, before it is
published, what is in it.

## How it reads

**The formats terminal recordings are published in.** asciicast versions
1, 2 and 3 from asciinema and everything that writes them, script(1) with
its timing file in the classic and the advanced format, ttyrec, animated
SVGs from svg-term and termtosvg, and VHS tapes. A file is recognised by
what it holds, not by its name, and a folder is read whole.

**It plays the recording back.** The session goes through a small terminal
that keeps, for every cell of the screen, which character is in it, when it
got there, whether it got there because somebody typed it, and when and how
it stopped being there. That is the only way to know that a line was
deleted: readline rubs a character out with a backspace and an erase, kills
a line by jumping back to the prompt and clearing from there, and zsh
rewrites characters with the same characters as you type. A cell that was
typed and then lost before its line was finished is an outtake. A screen
that scrolled, a `clear`, or vim editing a file are not.

**Typing is told from output by the keyboard when it was saved, and by the
shape of the echo when it was not.** An echo is one character, or two when
zsh redraws the word, written where the cursor already was. A spinner or a
counter that a program updates a character at a time moves the cursor
first.

**A line that comes back is not an outtake.** Retyped and run, or sent from
the input box of a coding agent or a chat client that empties the box and
prints the message above, it is on the screen again a moment later, and
Outtakes lets it go. So does a typo fixed by retyping the word.

**What the report says:**

- **HIDDEN** is a password typed at a prompt that showed nothing, kept
  because the keyboard was being recorded. The keys are never printed.
- **SECRET** is a credential the screen showed: a token by its shape, or a
  password in a setting, an argument or a URL. Always covered, `--reveal` or
  not.
- **TYPED** is what was typed and taken back: deleted before enter,
  abandoned with ctrl-c, on the screen in one SVG frame and gone in the
  next, or typed between Hide and Show in a VHS tape. A line with a secret
  or a name in it is what this is for; half a command somebody thought
  better of is listed too, lower down.
- **UNSEEN** is what the file carries and no player draws: window titles,
  the folder the shell reported to the terminal, iTerm2's and VS Code's
  shell integration, text sent to the clipboard, the terminal's version and
  colour scheme, the paths script logged to.
- **TELLS** is whose machine it was, as the screen showed it: account and
  host, home folder, internal host names, addresses, email addresses, AWS
  account numbers.
- **CLOCK** is the time zone, when the recording gives it away.
- **MATCHES**, given two folders, is what ties one to the other.
- **HANDS** says whether the typing is live, and so comparable.
- **UNREAD** is what could not be read, printed first. A clean report about
  half a folder is half a report.

The report masks names, hosts and times unless you pass `--reveal`, so it
can be pasted into an issue as it is. Secrets are covered either way.

## Two names, one person

Given two folders, Outtakes says what ties the recordings in one to the
recordings in the other: the same account or host or internal domain on the
screen, the same terminal down to its build, the same colour scheme, the
same time zone, and the same hands.

The hands are compared the way Gunetti and Picardi did it. Take the pairs of
keys both sets typed, rank the pairs by how long each set took over them,
and measure how far the rankings disagree and how many pairs took about as
long in both. A distance on its own means little, because it grows as the
typing shrinks, so each folder is cut in two at random, thirty three times,
and the distance between the folders is set against the distance between
each folder's own halves. The same hands come out near one.

Measured on public recordings, below: 1.05 or under is the same hands, 1.10
or under probably the same, over 1.25 different hands, and in between the
report says it cannot call it, because different people land there and so
does one person on different days. It needs about 300 keys on each side
before it will say anything. Typing paced by a machine, the way VHS types,
is recognised as such and never compared: it carries no rhythm at all.

## The calibration

Everything above was measured, on two kinds of recording.

**The lab session.** `testdata/record` types one session a key at a time,
with the pauses a person leaves, into real bash and zsh under asciinema 2
and 3, script in both timing formats and ttyrec, and renders it with
termtosvg and svg-term. The session fixes a typo, kills a line with a
password in it, answers a hidden prompt, prints a `.env`, and abandons an
`rm -rf` with ctrl-c. Every recorder has to give the same answers about it,
and the CI makes the recordings again on every push and checks that they
still do.

**Public recordings.** 858 recordings from 110 asciinema.org accounts:
everything in the public and featured listings, and up to thirteen more
from each account with four or more. They were fetched one at a time with a
pause between, read on one machine, and are not in this repository. Only
counts were kept. Nothing about any one recording was printed, kept or
passed on.

| what turned up | recordings | share |
|---|---|---|
| the start, stamped to the second | 731 | 85.2% |
| a window title no player draws | 436 | 50.8% |
| an account@host on the screen | 325 | 37.9% |
| a home folder on the screen | 240 | 28.0% |
| the time zone | 122 | 14.2% |
| the folder, reported to the terminal | 105 | 12.2% |
| an email address | 77 | 9.0% |
| the colour scheme | 71 | 8.3% |
| a line typed and deleted | 58 | 6.8% |
| the terminal's name and exact version | 57 | 6.6% |
| an internal host name | 28 | 3.3% |
| something a secret rule matches | 16 | 1.9% |
| a line abandoned with ctrl-c | 2 | 0.2% |
| the keyboard, recorded | 3 | 0.3% |
| enough live typing to compare | 133 | 15.5% |
| typing paced by a machine | 16 | 1.9% |
| a file it could not read | 0 | 0% |

Three recorded the keyboard, and none of the three caught a password. That
case is rare because asciinema leaves input capture off unless told
otherwise, and it is the one nothing else looks for.

**The typing.** 38 of the accounts had enough live typing to split their
recordings into two halves of at least 300 keys each. Every account's two
halves were compared with each other, and every account's first half with
every other account's second:

| | pairs | lowest | median | highest |
|---|---|---|---|---|
| same account | 34 | 0.77 | 0.99 | 1.55 |
| different accounts | 949 | 1.02 | 1.38 | 2.03 |

| tied together at | same account | different accounts |
|---|---|---|
| 1.05 or under | 82% | 0.5% |
| 1.10 or under | 91% | 1.5% |
| 1.25 or under | 97% | 19% |

A pair from one account comes out closer than a pair from two accounts 97
times in a hundred. An account is not always one person, a project's
account can be several, and some recordings were typed by a tool, so the
same-account numbers are if anything worse than one person's own
recordings would be.

Three other ways of measuring were tried on the same pairs. The plain
average difference of the logged gaps separated as well, 0.978 against
0.971, which over 34 pairs is no difference at all. Taking the typing speed
out made it worse, 0.959, so speed is part of what gives somebody away.
Gunetti and Picardi's measure stays because it is published, and anybody
can check it against the paper.

**What the calibration changed.** The first run over public recordings
found the program wrong in ways the lab session never could have:

- A coding agent's spinner, updated a character at a time, came out as
  hundreds of deleted lines. Echoes are now recognised by their shape and not
  only their length: a spinner moves the cursor before it writes.
- Messages typed into a coding agent's input box came out as deleted,
  because the box empties when a message is sent. A line that is on the
  screen again afterwards is now let go.
- "cat sample.txt" came out as "ca sam", because the letters tab completion
  added had not been typed. A deleted line is now everything between the
  first and the last typed character.
- A "0:00:00" timer voted for UTC-11:30, an offset no place keeps. Only
  offsets in use count now, and UTC itself, which is what servers are set
  to, is not reported as a place.
- A recording resized while vim was open crashed the player. That is fixed,
  and a recording that trips the player up is now reported as unread while
  the rest of the folder is still read.

## Running it

```bash
outtakes demo.cast
```

```bash
outtakes docs/
```

```bash
outtakes demo.svg
```

```bash
outtakes demo.tape
```

```bash
outtakes typescript
```

script's timing file is found beside its log under the names script's own
manual uses. Two folders are compared:

```bash
outtakes my-name/ other-name/
```

`--reveal` prints account names, hosts and times. It never prints a secret.

Exit code is 1 when a recording shows a secret, kept a password, kept a
deleted line with a secret or a name in it, or when two folders can be tied
together; 3 when nothing could be read; 0 otherwise. So it can sit in CI in
front of the folder the demos are published from, and a typo fixed on camera
will not fail the build.

Windows marks anything that came from the internet, so the first run may
bring up a blue "Windows protected your PC" box. Right click the zip,
Properties, tick Unblock at the bottom, and then extract it.

## What it cannot do

**It cannot find what is not there.** A password typed at a prompt that
hides it is only in the file if the keyboard was recorded. When it was not,
the recording is clean of it and Outtakes says nothing, because there is
nothing to say.

**GIFs and videos are pictures.** Every frame of a GIF is kept too, and
reading them would take character recognition, which is a model, and this
has none. Read the cast the GIF was made from instead.

**zsh's ctrl-c leaves no mark on the screen.** bash prints `^C` after an
abandoned line; zsh moves to a new line, exactly as if the line had run.
Without the keyboard in the recording, an abandoned line in zsh looks like
one that ran, and is not reported.

**Shells that redraw the whole line on every key** (fish, or zsh with
autosuggestions) do not echo keys one at a time, so without the keyboard in
the recording their typing is harder to tell from output and some of it is
missed. With the keyboard recorded it is read in full.

**The rhythm comparison is evidence, not proof.** On public recordings it
tied the two halves of one account together nine times in ten, and two
different accounts about once in seventy. It says nothing below about 300
keys a side, which a demo of a few commands does not reach, and it cannot
say who typed something, only whether two sets of recordings were typed
alike.

**Secrets are found by name.** Every rule says what it finds: a token by its
shape, a password next to the word password. There is no "this looks
random" rule, because a report of maybes is a report nobody reads twice, so
a secret with no shape and no label is missed.

**It does not fix anything.** It reads. Cutting a recording is what
[asciinema-edit](https://github.com/cirocosta/asciinema-edit) is for, and
for a secret that has been published, the fix is revoking it.

**Formats it does not read:** tlog's JSON, termrec, ipbt, PowerShell
transcripts ([Hearsay](https://github.com/NissanBoss/Hearsay) reads those),
and asciinema's raw and txt outputs, which have no timing to play back.

## It only reads

Nothing is written, nothing is sent anywhere, no other program is run, and
there is no model in it. Tests read the source and fail the build if a call
that writes a file, an import of anything under `net`, an import of
`os/exec` or anything that looks like a model turns up.

The file you are about to publish is the one copy of that session you have,
and a scanner that wrote to it would be worse than anything it could find.
And a recording you are checking for secrets is the last thing that should
go to somebody else's server to be checked.

Secrets are covered with a fixed width marker, so not even their length
reaches the screen. A report about a password left in a recording should
not leave it in the scrollback of the terminal it was read in.

It does not scold anybody for doing the right thing. A typo fixed with two
backspaces is not reported, a placeholder like `your-token-here` is not a
secret, a machine set to UTC is not a place, and typing paced by a machine
is reported as what it is: no rhythm at all, which is the point.

## Building

Needs [Go](https://go.dev) and nothing else. No dependencies at all.

```bash
go build -o outtakes .
```

```bash
go test ./...
```

The recordings in `testdata` were made by the real recorders in a
container, typing a session a key at a time into real shells. The recipe is
in `testdata/record`, and the CI makes them again on every push and runs
the same checks on what comes out.

## Licence

MIT. Do what you like with it.
