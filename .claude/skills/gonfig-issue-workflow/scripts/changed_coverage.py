#!/usr/bin/env python3
"""List uncovered code blocks on lines changed against a base branch, like Codecov's patch coverage.

Go measures coverage per package: lines of package A exercised only by tests of package B count as
uncovered, e.g. internal/file code run by the cmd tests. Codecov uses the same profile, so this script
predicts its "lines missing coverage" before pushing. New, untracked files are included.

Usage (from the repository root):
  changed_coverage.py [BASE]    BASE defaults to main
"""

import re
import subprocess
import sys
import tempfile

MODULE_PREFIX_RE = re.compile(r"^\S+?/((?:cmd|internal|pkg)/\S+|[^/\s]+\.go):(\d+)\.\d+,(\d+)\.\d+ \d+ (\d+)$")


def changed_lines(base):
    diff = subprocess.run(["git", "diff", "-U0", base], capture_output=True, text=True, check=True).stdout
    untracked = subprocess.run(["git", "ls-files", "--others", "--exclude-standard"], capture_output=True, text=True, check=True).stdout.split()
    for path in untracked:
        # git diff --no-index exits with 1 if the files differ
        diff += subprocess.run(["git", "diff", "-U0", "--no-index", "/dev/null", path], capture_output=True, text=True).stdout

    changed, current = {}, None
    for line in diff.splitlines():
        if line.startswith("+++ b/"):
            current = line[6:].strip()
        m = re.match(r"@@ -\S+ \+(\d+)(?:,(\d+))? @@", line)
        if m and current:
            start, n = int(m.group(1)), int(m.group(2) or 1)
            changed.setdefault(current, set()).update(range(start, start + n))
    return changed


def main():
    base = sys.argv[1] if len(sys.argv) > 1 else "main"
    with tempfile.NamedTemporaryFile(suffix=".out") as profile:
        test = subprocess.run(["go", "test", f"-coverprofile={profile.name}", "./..."], capture_output=True, text=True)
        if test.returncode != 0:
            print(test.stdout + test.stderr)
            sys.exit("tests failed")
        changed = changed_lines(base)

        missing = []
        for line in open(profile.name):
            m = MODULE_PREFIX_RE.match(line.strip())
            if not m or m.group(4) != "0":
                continue
            path, start, end = m.group(1), int(m.group(2)), int(m.group(3))
            if any(l in changed.get(path, set()) for l in range(start, end + 1)):
                source = open(path).read().splitlines()[start - 1].strip()
                missing.append(f"{path}:{start}: {source}")

    print("\n".join(missing) if missing else "no uncovered changed blocks")


if __name__ == "__main__":
    main()
