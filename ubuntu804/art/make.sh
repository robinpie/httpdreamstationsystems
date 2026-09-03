#!/bin/sh
# Generate the shippable bitmaps from the originals in ../assets-src and the
# hand-drawn SVGs here. Run from this directory; writes beside this script.
#
# WHY these are committed rather than built by build.pl, when build.pl happily
# subsets fonts on every run: the line is whether an artifact depends on the
# SITE CONTENT.
#
#   The font subset does - it is derived from a codepoint census of the
#   generated HTML, so it has to be rebuilt whenever a page changes, and its
#   bytes varying between fontTools versions is a cosmetic annoyance in git.
#
#   These three do not. They are fixed functions of fixed inputs, and building
#   them per-machine bought nothing and cost correctness: ImageMagick
#   7.1.2-HDRI on the workstation and 7.1.1 on the server disagreed about
#   firefox24.png by 10% RMSE with a max channel difference of 255, in a piece
#   of visible panel chrome. Whichever machine built last silently changed the
#   panel icon.
#
# So: content-derived artifacts are built, content-independent ones are
# committed. That also leaves build.pl needing neither ImageMagick nor librsvg
# - only perl, Text::Markdown and pyftsubset - which is what makes building on
# the server a short list.
set -eu

command -v magick       >/dev/null || { echo "need ImageMagick 7 (magick)" >&2; exit 1; }
command -v rsvg-convert >/dev/null || { echo "need librsvg (rsvg-convert)" >&2; exit 1; }

SRC=../assets-src

# The panel's window-list icon, 24px. Firefox 3 shipped 16/32/48, so 24 has to
# be resampled, and the filter was chosen by measurement rather than habit:
# against the reference screenshot's own panel icon, aligned at +1+0 (which is
# where the icon box starts, and independently confirms site.css's 1px panel
# padding), Catrom scores 5.62% where Lanczos scores 5.84% and plain Cubic
# 6.88%. Down from 32 rather than up from 16: 16 padded to 24 scores 36%.
magick "$SRC/firefox32.png" -filter Catrom -resize 24x24 -strip firefox24.png

# The location bar's identity icon and the browser tab favicon. rsvg-convert
# and NOT magick: Debian builds ImageMagick without librsvg, and its internal
# SVG renderer draws this one wrong - grey outer arc, inner arc missing, 24%
# RMSE - without failing. rsvg-convert's output is byte-identical on both
# machines.
rsvg-convert -w 16 -h 16 mark16.svg -o favicon16.png

# The wallpaper. Quality 78: the heron has hard edges so the setting does move
# the number, but only from 1.76% to 1.52% against the reference, and lossless
# costs 290kB against 32kB. Indistinguishable at 2.5x zoom on the 48px of it
# anyone ever sees.
magick "$SRC/warty-final-ubuntu.png" -strip -quality 78 \
       -define webp:method=6 heron.webp

ls -l firefox24.png favicon16.png heron.webp
