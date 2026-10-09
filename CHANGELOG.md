# Changelog

## v1

The first release.

Reads a terminal recording before it is published and says what it kept.

- **The formats terminal recordings are published in**: asciicast
  versions 1, 2 and 3, script(1) logs with their timing in either format,
  ttyrec, animated SVGs from svg-term and termtosvg, and VHS tapes. A
  folder is read whole, and files are recognised by what they hold rather
  than their names.
- **Played back, not searched.** A small terminal follows every cell of the
  screen, so a line typed and killed with ctrl-u, abandoned with ctrl-c, or
  shown in one SVG frame and gone in the next is found, and a screen that
  scrolled, a `clear` or vim editing a file is not mistaken for it.
- **Passwords the keyboard recorded** at prompts that showed nothing, with
  the prompt they answered. The keys are never printed.
- **What no player draws**: window titles, the folder the shell reports,
  iTerm2's and VS Code's shell integration, the clipboard, the terminal's
  version and colour scheme, the paths script logged to.
- **Whose machine and where in the world**: account and host, home folders,
  internal host names, addresses, and the time zone from a clock on the
  screen.
- **Two folders compared**: what ties one to the other, the rhythm of the
  typing included, measured the way Gunetti and Picardi measured free text.
- **Masked by default**, and secrets covered with a fixed width marker
  whether `--reveal` is passed or not.

**Calibrated on 858 public recordings from asciinema.org.** Half carried a
window title nobody watching saw, nearly two in five showed an
account@host, and one in seven gave away its time zone. Of the accounts
with enough live typing, the rhythm tied the two halves of the same account
together nine times in ten and two different accounts about once in
seventy. The recordings are not in the repository, and nothing about any
one of them is printed.
