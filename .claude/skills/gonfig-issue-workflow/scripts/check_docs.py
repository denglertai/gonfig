#!/usr/bin/env python3
"""Run every documented console command of a Markdown file and compare its output with the documented output.

Blocks are handled in document order:
  - A code block introduced by a line like "`app.yaml`:" is written to that file in the work directory,
    so examples can show a file first and the command processing it afterwards.
  - Each "$ command" line of a ```console block is run with `sh -c` in the work directory. The lines
    following it, up to the next "$ " line, are the expected output (stdout and stderr combined).

Blocks nested in list items may be indented. bcrypt hashes are compared by format only, since their salt is random.

Usage:
  check_docs.py FILE --bin DIR [--workdir DIR] [--config FILE | --config-from HEADING] [--replace OLD=NEW ...]

  --bin       directory containing the gonfig binary to test, e.g. a fresh `go build -o DIR/gonfig .`
  --workdir   directory to run the commands in, a temporary directory by default
  --config       file copied to WORKDIR/.gonfig.yaml
  --config-from  heading of FILE whose first yaml block becomes WORKDIR/.gonfig.yaml,
                 e.g. "## Templates" for the template examples of FILTERS.md
  --replace   replaces paths within commands, e.g. /run/secrets=WORKDIR for documented absolute paths

Commands using placeholder paths like /path/to/file.xml are expected to fail and can be ignored.
"""

import argparse
import os
import re
import shutil
import subprocess
import sys
import tempfile

BLOCK_RE = re.compile(r"(?:^[ \t]*`([\w.\-/]+)`:\n(?:[ \t]*\n)?)?^([ \t]*)```(\w*)\n(.*?)^[ \t]*```", re.S | re.M)
BCRYPT_RE = re.compile(r"\$2a\$10\$\S+")


def dedent(body, indent):
    """Removes the indentation of the fence from each line of a nested block"""
    return "".join(line[len(indent):] if line.startswith(indent) else line for line in body.splitlines(True))


def commands(body):
    """Yields the commands of a console block with their expected output lines"""
    lines = body.rstrip("\n").split("\n")
    i = 0
    while i < len(lines):
        if not lines[i].startswith("$ "):
            i += 1
            continue
        cmd = lines[i][2:]
        i += 1
        expected = []
        while i < len(lines) and not lines[i].startswith("$ "):
            expected.append(lines[i])
            i += 1
        yield cmd, expected


def matches(cmd, got, expected):
    if "bcrypt" in cmd:
        return len(got) == len(expected) and all(BCRYPT_RE.sub("H", g) == BCRYPT_RE.sub("H", e) for g, e in zip(got, expected))
    return got == expected


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("file")
    parser.add_argument("--bin", required=True)
    parser.add_argument("--workdir")
    parser.add_argument("--config")
    parser.add_argument("--config-from")
    parser.add_argument("--replace", action="append", default=[])
    args = parser.parse_args()

    workdir = args.workdir or tempfile.mkdtemp(prefix="check-docs-")
    os.makedirs(workdir, exist_ok=True)
    text = open(args.file).read()
    if args.config:
        shutil.copy(args.config, os.path.join(workdir, ".gonfig.yaml"))
    elif args.config_from:
        section = text[text.index(args.config_from):]
        config = re.search(r"```yaml\n(.*?)```", section, re.S).group(1)
        with open(os.path.join(workdir, ".gonfig.yaml"), "w") as f:
            f.write(config)

    replacements = [r.split("=", 1) for r in args.replace]
    env = dict(
        os.environ,
        PATH=os.path.abspath(args.bin) + os.pathsep + os.environ["PATH"],
        # Logs go to stderr and would be compared as output, plugins of the work directory must not be loaded
        GONFIG_LOG_LEVEL="error",
        GONFIG_PLUGIN_PATH=os.path.join(workdir, "no-plugins"),
    )

    count = failures = written = 0
    for m in BLOCK_RE.finditer(text):
        name, indent, lang = m.group(1), m.group(2), m.group(3)
        body = dedent(m.group(4), indent)
        if name:
            # Example files may be in directories, e.g. templates/app.yaml for examples with several files
            os.makedirs(os.path.dirname(os.path.join(workdir, name)), exist_ok=True)
            with open(os.path.join(workdir, name), "w") as f:
                f.write(body)
            written += 1
            continue
        if lang != "console":
            continue

        for cmd, expected in commands(body):
            for old, new in replacements:
                cmd = cmd.replace(old, new.replace("WORKDIR", workdir))
            out = subprocess.run(["sh", "-c", cmd], capture_output=True, text=True, env=env, cwd=workdir)
            text = out.stdout + out.stderr
            got = text.rstrip("\n").split("\n") if text else []
            count += 1
            if not matches(cmd, got, expected):
                failures += 1
                print(f"FAIL: {cmd}\n  expected: {expected}\n  got:      {got}")

    print(f"{count} commands, {written} files written, {failures} failures (workdir {workdir})")
    sys.exit(1 if failures else 0)


if __name__ == "__main__":
    main()
