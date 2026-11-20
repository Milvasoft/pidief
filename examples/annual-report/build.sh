#!/usr/bin/env sh
# Builds annual-report.pdf from template.html + data.json in four pidief steps.
# Usage: ./build.sh            (uses `pidief` from PATH; set PIDIEF=/path/to/pidief to override)
set -eu
cd "$(dirname "$0")"
P="${PIDIEF:-pidief}"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# 1. Fill the template and print it with Chromium. The page draws its charts with JavaScript and sets
#    window.ready; CSS @page rules make page 3 landscape, so the CSS page size must win over --paper.
"$P" template template.html data.json \
  --prefer-css-page-size \
  --wait-for-expression "window.ready === true" \
  --outline \
  --title "2026 Annual Revenue Report" --author "Example Cloud Inc." \
  -o "$TMP/1-printed.pdf"

# 2. Page footer on every page but the cover (%p = page number, %P = page count).
"$P" stamp "$TMP/1-printed.pdf" --pages "2-" \
  --text "Example Cloud Inc.  ·  RPT-2026-11  ·  Page %p of %P" \
  --font-size 7 --color "#7A7F8C" --position bc --offset-y 26 \
  -o "$TMP/2-numbered.pdf"

# 3. The approval seal (a transparent PNG, see make-seal.py) over the signature on the last page.
"$P" stamp "$TMP/2-numbered.pdf" seal.png --pages l \
  --position br --scale 0.2 --offset-x -80 --offset-y 470 --opacity 0.9 \
  -o "$TMP/3-sealed.pdf"

# 4. Archive-grade output: PDF/A-2b.
"$P" pdfa "$TMP/3-sealed.pdf" -o annual-report.pdf

"$P" info annual-report.pdf | grep -E '"(pageCount|pdfa)"'
