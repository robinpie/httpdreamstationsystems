reference-1280x720.png
======================

The specification for this whole template: a 1280x720 screenshot of an Ubuntu
8.04 LTS "Hardy Heron" desktop running Firefox 3, with Firefox's back, forward,
reload and stop buttons and its search box already customised away by whoever
took it.

PLAN.md section 2 made this file authoritative over stock Firefox 3, and
../ubuntu804template.txt records what was measured off it. verify.py diffs against
it band by band. It is committed here, rather than left on a workstation, so
that the source tree is self-contained and the verification loop runs anywhere.

Renamed from Screenshot_testubuntu804_2026-09-03_10:19:10.png. The colon in the
original name is legal on ext4 and a nuisance everywhere else, and this is a
public repository that people may check out on other systems.

One measured fact about the file itself, because it bounds every colour in
site.css: all 921,600 of its pixels sit exactly on the RGB565 lattice - red and
blue are multiples of 8, green of 4, with no exceptions. It came off a 16-bit
framebuffer. See ../ubuntu804template.txt, "Verification".

It depicts Canonical's Human theme and wallpaper and Mozilla's Firefox chrome.
Not served; see rootdomain/tmp-ubuntu804/CREDITS.txt for attribution.
