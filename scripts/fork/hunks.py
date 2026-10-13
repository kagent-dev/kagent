#!/usr/bin/env python3
"""The re-pin's hunk check (FORK.md, "Re-pin"): every hunk a carried commit
brought is in the candidate as it is in the consumed head.

    scripts/fork/hunks.py <pin> <consumed-head> <candidate-head>

The carried commits are pin..consumed-head. For each of their hunks (git diff
-U0, so a hunk is one run of removed and added lines), the lines it added are
looked up in its file at the consumed head and at the candidate head:

- added lines the consumed head still has and the candidate lacks: missing;
- removed lines the consumed head no longer has and the candidate has: back.

Either is a lost hunk. A hunk a later carried commit rewrote is in neither form
in the consumed head and is not judged: that later commit's hunk is. A hunk
upstream merged in another form is lost too, and so is one reworked onto
upstream's changes in a hand re-pin: the re-pin names it in ACCEPT_HUNKS
(whitespace-separated hunk ids `<sha>:<path>:<line>`, or a prefix of one at a
`:` — `<sha>` accepts every hunk of that commit, `<sha>:<path>` every hunk in
that file), and the report lists it as accepted.

Prints a markdown report on stdout. Exit 0: no hunk lost but accepted ones.
Exit 1: a hunk is lost, each named by commit, file and hunk header.
"""

import os
import re
import subprocess
import sys

HUNK_RE = re.compile(rb"^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@")
EXCERPT = 4


def git(*args):
    return subprocess.run(
        ["git", "-c", "core.quotePath=false", *args],
        check=True,
        stdout=subprocess.PIPE,
    ).stdout


class Blobs:
    """File contents at a commit, through one `git cat-file --batch`."""

    def __init__(self):
        self.proc = subprocess.Popen(
            ["git", "cat-file", "--batch"],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
        )
        self.cache = {}

    def text(self, rev, path):
        """The file with a newline on either side, so a block of whole lines
        matches as `\\n<lines>\\n`; None when the file is not there."""
        key = (rev, path)
        if key not in self.cache:
            self.proc.stdin.write(rev.encode() + b":" + path + b"\n")
            self.proc.stdin.flush()
            header = self.proc.stdout.readline().split()
            if header[-1] == b"missing" or header[1] != b"blob":
                self.cache[key] = None
            else:
                size = int(header[2])
                data = self.proc.stdout.read(size)
                self.proc.stdout.read(1)
                if not data.endswith(b"\n"):
                    data += b"\n"
                self.cache[key] = b"\n" + data
        return self.cache[key]


def path_of(line, prefix):
    name = line[4:].rstrip(b"\n")
    if name == b"/dev/null":
        return None
    if name.startswith(b'"'):
        # A name git quotes for a tab, newline or quote in it: no carried file has one.
        name = name[1:-1]
    return name[len(prefix):]


def carried_hunks(pin, head):
    """Yield (sha, subject, path, header, removed, added) per -U0 hunk of the
    carried commits, oldest first."""
    out = git(
        "log", "--reverse", "--no-merges", "-p", "-U0", "-M", "--no-color",
        "--no-ext-diff", "--format=%x00%H %s", f"{pin}..{head}",
    )
    sha = subject = None
    old_path = new_path = None
    lines = out.split(b"\n")
    i = 0
    while i < len(lines):
        line = lines[i]
        i += 1
        if line.startswith(b"\x00"):
            sha, _, subject = line[1:].decode(errors="replace").partition(" ")
            old_path = new_path = None
        elif line.startswith(b"diff --git "):
            old_path = new_path = None
        elif line.startswith(b"--- ") and old_path is None and new_path is None:
            old_path = path_of(line, b"a/") or b""
            plus = lines[i]
            i += 1
            new_path = path_of(plus, b"b/")
        elif m := HUNK_RE.match(line):
            n_removed = 1 if m.group(2) is None else int(m.group(2))
            n_added = 1 if m.group(4) is None else int(m.group(4))
            removed, added = [], []
            while len(removed) < n_removed or len(added) < n_added:
                body = lines[i]
                i += 1
                if body.startswith(b"-"):
                    removed.append(body[1:])
                elif body.startswith(b"+"):
                    added.append(body[1:])
                # "\ No newline at end of file" carries no line.
            header = line.split(b" @@")[0] + b" @@"
            yield sha, subject, new_path or old_path, header.decode(), removed, added


def block(lines):
    return b"\n" + b"\n".join(lines) + b"\n"


def accepted(hunk_id, accept):
    sha, _, rest = hunk_id.partition(":")
    for entry in accept:
        e_sha, sep, e_rest = entry.partition(":")
        if len(e_sha) < 7 or not sha.startswith(e_sha):
            continue
        if not sep or rest == e_rest or rest.startswith(e_rest + ":"):
            return True
    return False


def main():
    if len(sys.argv) != 4:
        print(__doc__.strip().splitlines()[2].strip(), file=sys.stderr)
        return 64
    pin, head, candidate = (git("rev-parse", r).decode().strip() for r in sys.argv[1:])
    accept = os.environ.get("ACCEPT_HUNKS", "").split()
    blobs = Blobs()
    replayed = {}
    for row in git("log", "--no-merges", "--format=%H %s", f"{pin}..{candidate}").decode(errors="replace").splitlines():
        r_sha, _, r_subject = row.partition(" ")
        replayed.setdefault(r_subject, r_sha)

    lost, waived, judged = [], [], 0
    for sha, subject, path, header, removed, added in carried_hunks(pin, head):
        before = blobs.text(head, path) or b"\n"
        after = blobs.text(candidate, path) or b"\n"
        if added:
            kept = block(added) in before
            why = "its added lines are not in the candidate" if kept and block(added) not in after else None
        else:
            kept = block(removed) not in before
            why = "its removed lines are back in the candidate" if kept and block(removed) in after else None
        if not kept:
            continue
        judged += 1
        if why is None:
            continue
        hunk_id = f"{sha[:7]}:{path.decode(errors='replace')}:{header.split('+')[1].split(',')[0].split(' ')[0]}"
        entry = (sha, subject, path.decode(errors="replace"), header, hunk_id, why, added or removed)
        (waived if accepted(hunk_id, accept) else lost).append(entry)

    def item(entry, excerpt):
        sha, subject, path, header, hunk_id, why, body = entry
        twin = replayed.get(subject)
        twin = f"; in the candidate `{twin[:7]}`" if twin else "; no candidate commit has its subject"
        print(f"- `{sha[:7]}` {subject} — `{path}` `{header}`: {why}{twin} (id `{hunk_id}`)")
        if excerpt:
            print()
            print("  ```")
            for b in body[:EXCERPT]:
                print("  " + b.decode(errors="replace"))
            if len(body) > EXCERPT:
                print(f"  … {len(body) - EXCERPT} more line(s)")
            print("  ```")

    print(f"#### Carried hunks ({judged} judged)\n")
    if lost:
        print(f"{len(lost)} carried hunk(s) are lost in the candidate `{candidate[:7]}`:\n")
        for entry in lost:
            item(entry, True)
        print(
            "\nPut each hunk back into its commit on the candidate, or, where the re-pin drops or reworks it on"
            " purpose, name it in `ACCEPT_HUNKS` (the dispatch input `accept_hunks`): its id, or the commit for all its hunks.\n"
        )
    else:
        but = " but the accepted ones below" if waived else ""
        print(f"Every carried hunk is in the candidate `{candidate[:7]}`{but}.\n")
    if waived:
        print(f"Accepted as dropped or reworked ({len(waived)}):\n")
        for entry in waived:
            item(entry, False)
        print()
    return 1 if lost else 0


if __name__ == "__main__":
    sys.exit(main())
