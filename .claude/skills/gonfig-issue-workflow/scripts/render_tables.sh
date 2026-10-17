#!/bin/sh
# Render Markdown files with GitHub's Markdown API and report the column counts of their tables.
# Every row of a table must have the same number of columns, and no literal "\|" must remain:
# an unescaped pipe within a code span of a table row would split the cell.
#
# Usage: render_tables.sh FILE [FILE ...]
set -e

for file in "$@"; do
	python3 -I -c 'import json, sys; print(json.dumps({"text": open(sys.argv[1]).read(), "mode": "gfm", "context": "denglertai/gonfig"}))' "$file" |
		gh api -X POST /markdown --input - |
		python3 -I -c '
import re, sys
html = sys.stdin.read()
tables = re.findall(r"<table[^>]*>(.*?)</table>", html, re.S)
columns = [sorted({len(re.findall(r"<t[dh][ >]", row)) for row in re.findall(r"<tr[^>]*>(.*?)</tr>", t, re.S)}) for t in tables]
bad = [i + 1 for i, c in enumerate(columns) if len(c) != 1]
print(sys.argv[1], "tables:", len(tables), "columns:", columns, "literal \\|:", html.count("\\|"), "inconsistent tables:", bad or "none")
' "$file"
done
