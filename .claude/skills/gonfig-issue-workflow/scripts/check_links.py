#!/usr/bin/env python3
"""Check relative links and anchors of Markdown files the way GitHub resolves them.

Links to http(s) URLs are skipped. Anchors are derived from headings like GitHub does:
lower case, punctuation removed, spaces replaced by hyphens, e.g. "upper / lower" -> "#upper--lower".

Usage:
  check_links.py ROOT FILE [FILE ...]
"""

import os
import re
import sys


def strip_code(text):
    """Removes code blocks and inline code, whose brackets are no links"""
    return re.sub(r"`[^`\n]*`", "", re.sub(r"```.*?```", "", text, flags=re.S))


def anchors(path):
    text = re.sub(r"```.*?```", "", open(path).read(), flags=re.S)
    return {re.sub(r"[^\w\- ]", "", h.strip().lower()).replace(" ", "-") for h in re.findall(r"^#+ (.+)$", text, re.M)}


def main():
    root, files = sys.argv[1], sys.argv[2:]
    checked = broken = 0
    for f in files:
        for link in re.findall(r"\]\(([^)\s]+)\)", strip_code(open(os.path.join(root, f)).read())):
            if link.startswith(("http://", "https://", "mailto:")):
                continue
            checked += 1
            target, _, fragment = link.partition("#")
            path = os.path.normpath(os.path.join(root, os.path.dirname(f), target)) if target else os.path.join(root, f)
            if not os.path.exists(path):
                print(f"{f}: missing file {link}")
                broken += 1
            elif fragment and os.path.isfile(path) and fragment not in anchors(path):
                print(f"{f}: missing anchor {link}")
                broken += 1
    print(f"{checked} links, {broken} broken")
    sys.exit(1 if broken else 0)


if __name__ == "__main__":
    main()
