#!/usr/bin/env python3
"""Make the recordings in testdata/ with the real recorders.

Runs inside the container described by the Dockerfile next to this file:

    docker build -t outtakes-lab testdata/record
    docker run --rm --hostname lab -e TZ=America/Bogota -v "$PWD/testdata:/out" \
        outtakes-lab python3 /usr/local/bin/drive.py

The zone is fixed because the tests read it back: script writes it into
its first line, and zsh's prompt clock gives it away.

Each session is typed into a real shell through a real pseudo-terminal, a
key at a time, with the pauses a person leaves. That is the whole point:
what bash prints when you press backspace, what readline does with ctrl-u,
what each recorder writes down, are things to measure, not to guess.
"""

import fcntl
import os
import pty
import random
import select
import struct
import subprocess
import sys
import termios
import time

OUT = "/out"
COLS, ROWS = 80, 24


class Session:
    def __init__(self, argv, env=None):
        pid, fd = pty.fork()
        if pid == 0:
            os.execvpe(argv[0], argv, env or os.environ)
        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
        self.pid, self.fd, self.seen = pid, fd, b""

    def read(self, wait):
        end = time.time() + wait
        while time.time() < end:
            ready, _, _ = select.select([self.fd], [], [], 0.05)
            if not ready:
                continue
            try:
                chunk = os.read(self.fd, 65536)
            except OSError:
                return
            if not chunk:
                return
            self.seen += chunk

    def prompt(self, marks=(b"$ ", b"% ", b"# "), wait=10):
        # A prompt is a prompt sign followed by the shell going quiet. zsh
        # draws the right-hand prompt and moves the cursor back after the
        # sign, so the output does not end with it, and waiting for silence
        # is the only test that works for both shells.
        start = len(self.seen)
        end = time.time() + wait
        while time.time() < end:
            before = len(self.seen)
            self.read(0.3)
            if len(self.seen) == before and any(m in self.seen[start:] for m in marks):
                return
        raise SystemExit("no prompt in %r" % self.seen[-200:])

    def send(self, text):
        os.write(self.fd, text.encode())

    def finish(self):
        self.read(1.0)
        _, status = os.waitpid(self.pid, 0)
        return status


class Typist:
    """A person at a keyboard, as far as timing goes.

    Intervals between keys are log-normal, which is what keystroke studies
    find, and a few letter pairs are quicker or slower than the rest, which
    is what makes one person's rhythm different from another's.
    """

    def __init__(self, name, median, spread, pairs, seed):
        self.name, self.median, self.spread, self.pairs = name, median, spread, pairs
        self.rand = random.Random(seed)

    def gap(self, prev, key):
        g = self.rand.lognormvariate(0, self.spread) * self.median
        return g * self.pairs.get(prev + key, 1.0)

    def type(self, s, text, enter=True):
        prev = " "
        for key in text:
            time.sleep(self.gap(prev, key))
            s.send(key)
            s.read(0)
            prev = key
        if enter:
            time.sleep(self.gap(prev, "\r") * 1.6)
            s.send("\r")

    def erase(self, s, n):
        for _ in range(n):
            time.sleep(self.gap("x", "\x7f") * 0.8)
            s.send("\x7f")


ANA = Typist("ana", 0.11, 0.35, {"th": 0.6, "er": 0.7, "in": 0.65, "st": 0.8, "ls": 0.5, "cd": 0.6}, 1)
BEN = Typist("ben", 0.21, 0.55, {"th": 1.3, "er": 0.9, "in": 1.2, "ex": 0.7, "it": 1.5, "ls": 1.4}, 2)


def setup():
    os.makedirs(os.path.expanduser("~/work/acme-billing"), exist_ok=True)
    with open(os.path.expanduser("~/work/acme-billing/.env"), "w") as f:
        f.write("STRIPE_WEBHOOK_SECRET=whsec-placeholder-for-tests\nDB_HOST=db01.acme.internal\n")


def leaky(s, who, read_secret="read -s -p 'Password: ' pw; echo"):
    """The demo everybody records: a few commands, a typo, a regret."""
    s.prompt()
    who.type(s, "cd ~/work/acme-billing")
    s.prompt()
    who.type(s, "ehc", enter=False)
    who.erase(s, 2)
    who.type(s, "cho hello world")
    s.prompt()
    who.type(s, "export DB_PASSWORD=hunter2-correct-staple", enter=False)
    time.sleep(0.8)
    s.send("\x15")  # ctrl-u: the line is gone from the screen, not from the file
    s.read(0.2)
    who.type(s, read_secret)
    s.read(0.5)
    who.type(s, "Tr0ub4dor&3")
    s.prompt()
    who.type(s, "cat .env")
    s.prompt()
    who.type(s, "clear")
    s.prompt()
    who.type(s, "rm -rf /tmp/old-builds", enter=False)
    time.sleep(0.5)
    s.send("\x03")
    s.prompt()
    who.type(s, "echo deploying to build01.corp.acme.internal")
    s.prompt()
    who.type(s, "exit")


def typing(s, who, rand):
    """A short session that is mostly typing, for comparing two hands."""
    lines = [
        "ls -la", "cd src", "git status", "git diff --stat", "cat main.go | head",
        "grep -rn TODO .", "make test", "go build ./...", "ls -lh dist", "cd ..",
        "git log --oneline -5", "echo done",
    ]
    rand.shuffle(lines)
    s.prompt()
    for line in lines[:8]:
        who.type(s, line)
        s.prompt()
    who.type(s, "exit")


def zsh_env():
    env = dict(os.environ)
    env["ZDOTDIR"] = "/tmp/zdot"
    os.makedirs("/tmp/zdot", exist_ok=True)
    with open("/tmp/zdot/.zshrc", "w") as f:
        # What Ubuntu's vte.sh and iTerm2's shell integration send before
        # every prompt. Neither is drawn by any player.
        f.write(
            "PROMPT='%n@%m %~ %# '\n"
            "RPROMPT='%*'\n"
            "precmd() {\n"
            "  printf '\\e]7;file://%s%s\\e\\\\' \"$HOST\" \"$PWD\"\n"
            "  printf '\\e]1337;RemoteHost=%s@%s\\a\\e]1337;CurrentDir=%s\\a' \"$USER\" \"$HOST\" \"$PWD\"\n"
            "  printf '\\e]0;%s@%s: %s\\a' \"$USER\" \"$HOST\" \"$PWD\"\n"
            "}\n"
        )
    return env


def record(name, argv, body, who, env=None, rand=None):
    path = os.path.join(OUT, name)
    for p in [path] + [path + ext for ext in (".timing", ".io")]:
        if os.path.exists(p):
            os.remove(p)
    s = Session(argv(path), env)
    if rand is None:
        body(s, who)
    else:
        body(s, who, rand)
    s.finish()
    print("  " + name)


def main():
    setup()
    os.chdir(os.path.expanduser("~"))
    bash = "bash -i"
    record("bash-asciinema2-stdin.cast", lambda p: ["/opt/asciinema2/bin/asciinema", "rec", "-q", "--stdin", "-c", bash, p], leaky, ANA)
    record("bash-asciinema3.cast", lambda p: ["asciinema3", "rec", "-q", "-c", bash, p], leaky, ANA)
    # zsh's read takes the prompt after a question mark; its -p reads from
    # a coprocess.
    zsh_leaky = lambda s, who: leaky(s, who, "read -s 'pw?Password: '; echo")
    record("zsh-asciinema3-input.cast", lambda p: ["asciinema3", "rec", "-q", "-I", "-c", "zsh -i", p], zsh_leaky, BEN, env=zsh_env())
    record("bash-script-classic", lambda p: ["script", "-q", "-T", p + ".timing", "-c", bash, p], leaky, ANA)
    record("bash-script-advanced", lambda p: ["script", "-q", "-m", "advanced", "-B", p, "-T", p + ".timing", "-c", bash], leaky, BEN)
    record("bash.ttyrec", lambda p: ["ttyrec", "-e", bash, p], leaky, ANA)
    # The two programs that turn a recording into the SVG a README embeds,
    # given the first session above. Neither is told anything special.
    cast = os.path.join(OUT, "bash-asciinema2-stdin.cast")
    subprocess.run(["/opt/termtosvg/bin/termtosvg", "render", cast, os.path.join(OUT, "termtosvg.svg")],
                   check=True, stdout=subprocess.DEVNULL)
    subprocess.run(["svg-term", "--in", cast, "--out", os.path.join(OUT, "svg-term.svg")], check=True)
    print("  termtosvg.svg\n  svg-term.svg")
    for who in (ANA, BEN):
        for i in range(4):
            record("hands-%s-%d.cast" % (who.name, i + 1), lambda p: ["asciinema3", "rec", "-q", "-c", bash, p],
                   typing, who, rand=random.Random(i))


if __name__ == "__main__":
    main()
