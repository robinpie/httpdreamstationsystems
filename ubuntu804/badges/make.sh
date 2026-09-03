#!/bin/sh
# Draw the 88x31 badges for the wallpaper strip.
#
# Run from this directory; writes *.png beside this script. The PNGs are
# committed, so this runs only when a badge changes — build.pl just copies
# them. It is a script rather than thirteen hand-drawn files because thirteen
# badges that share a plate should share one definition of the plate.
#
# +antialias is the whole trick. At this size DejaVu Sans Bold antialiased is
# grey mush; hard-edged it reads as the 1998 pixel lettering these things are
# supposed to look like.
#
# Text is measured first and then drawn at an absolute baseline, rather than
# composited with -gravity: gravity is a persistent setting in ImageMagick and
# leaks from the annotate that sets it into the -composite that follows, which
# silently shifts and clips the layer. Measuring is three extra subprocesses
# per badge and removes the whole class of problem.
set -eu

FONT=../fonts-src/DejaVuSans-Bold.ttf
W=88; H=31
TAGW=26          # tag block width
PLATEW=62        # W - TAGW
TAG_PT=9;  TAG_BASE=19
LINE_PT=8; L1_BASE=13; L2_BASE=24

# ink width of a string at a given pointsize
tw() {
  magick -size 400x40 xc:none -font "$FONT" +antialias -pointsize "$2" \
    -fill white -gravity west -annotate +0+0 "$1" -trim -format '%w' info:
}
# x that centres a string of width $1 in the box [$2, $2+$3)
cx() { echo $(( $2 + ($3 - $1) / 2 )); }

# badge <out.png> <tag> <tag-bg> <plate-bg> <line1> <line2>
badge() {
  out=$1; tag=$2; tagbg=$3; platebg=$4; l1=$5; l2=$6
  tx=$(cx "$(tw "$tag" $TAG_PT)"  0      $TAGW)
  x1=$(cx "$(tw "$l1"  $LINE_PT)" $TAGW  $PLATEW)
  x2=$(cx "$(tw "$l2"  $LINE_PT)" $TAGW  $PLATEW)
  magick -size ${W}x${H} "xc:$platebg" \
    -fill "$tagbg" -draw "rectangle 0,0 $((TAGW-1)),$((H-1))" \
    -font "$FONT" +antialias -gravity None -fill white \
    -pointsize $TAG_PT  -draw "text $tx,$TAG_BASE '$tag'" \
    -pointsize $LINE_PT -draw "text $x1,$L1_BASE '$l1'" \
                        -draw "text $x2,$L2_BASE '$l2'" \
    -fill none -stroke black -strokewidth 1 \
      -draw "rectangle 0.5,0.5 $((W-1)).5,$((H-1)).5" \
    -alpha off -strip "$out"
}

#      file               tag  tag bg     plate bg   line 1        line 2
badge valid-html.png      H5   "#e06000" "#1b1b1b" "VALID"       "HTML 5"
badge no-js.png           "0"  "#8b0000" "#1b1b1b" "BYTES OF"    "JAVASCRIPT"
badge nginx.png           N    "#009639" "#101c14" "POWERED BY"  "NGINX"
badge debian.png          D    "#a80030" "#1b1418" "DEBIAN 13"   "TRIXIE"
badge ntp.png             NTP  "#1a4f8a" "#101620" "IN THE"      "NTP POOL"
badge gopher.png          70   "#4a3f8f" "#16142a" "ALSO ON"     "GOPHER"
badge gemini.png          GEM  "#5c3a8e" "#171128" "GEMINI"      "CAPSULE"
badge finger.png          79   "#7a5c00" "#1c1808" "RFC 1288"    "FINGER"
badge anybrowser.png      "?"  "#005f87" "#0e1a20" "EVERY"       "BROWSER"
badge heron.png           8    "#dd4814" "#241408" "HARDY"       "HERON"
badge cookies.png         "0"  "#3f6212" "#141a0c" "COOKIES"     "SET, EVER"
badge dejavu.png          Aa   "#4b5563" "#14181c" "SET IN"      "DEJAVU"
badge eightyeight.png     88   "#b45309" "#201408" "x31"         "FOREVER"

echo "wrote $(ls -1 *.png | wc -l) badges"
