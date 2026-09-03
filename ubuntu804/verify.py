#!/usr/bin/env python3
"""verify.py — band-by-band 1x comparison against the reference screenshot.

    ./verify.py [--url URL] [--ref PATH] [--shot PATH] [--strip N]

Runs on the WORKSTATION only (PLAN.md section 12): the reference screenshot,
chromium and ImageMagick are all here, and iterating against the live server
would mean a commit and a deploy per screenshot.

Two things about this comparison are worth knowing before reading its output.

1.  THE REFERENCE IS 16-BIT.  Every one of the reference PNG's 921,600 pixels
    sits exactly on the RGB565 lattice — red and blue are multiples of 8,
    green of 4, no exceptions — so it was captured from a 16bpp framebuffer.
    Nothing rendered in 24-bit colour can match it more closely than 8/255 on
    red and blue, so our screenshot is truncated onto the same lattice before
    anything is measured. Otherwise a perfect render would still report ~2%
    RMSE and the number would mean nothing.

2.  SOME BANDS CANNOT REACH ZERO, BY DESIGN.  The panel says
    "dreamstation.systems" where the reference says "Live session user"; the
    title bar and location bar carry this site's title and URL; the bookmarks
    row is this site's navigation. Those bands are reported, but the number to
    watch there is the STRUCTURE column — a scan down an empty column of that
    band, where nothing but chrome should appear. Bands with no text in them
    (status bar away from "Done", wallpaper) should approach the quantisation
    floor everywhere.

Also expect glyph-level differences even where the text is identical: 2008 X11
rasterisation and a 2026 browser hint the same font differently. Chromium is
run with --disable-lcd-text so at least both sides are greyscale-antialiased.
That is what the FULL column measures and why it sits around 13% on every band
with writing in it while CHROME reads 0.00%.
"""

import argparse, os, re, subprocess, sys

HERE = os.path.dirname(os.path.abspath(__file__))
W, H = 1280, 720
REF_STRIP = 85          # the reference's exposed wallpaper strip, in px

# Bands from PLAN.md section 12.
#
#   anchor   what the band is pinned to, which decides how reference rows map
#            onto ours:
#              'top'    — measured from the top of the screen; same rows both.
#              'window' — pinned to the bottom of the browser window, so it
#                         shifts by the difference between the reference's
#                         85px strip and ours.
#              'bottom' — pinned to the bottom of the screen; compared over the
#                         shorter of the two strips, since both wallpapers are
#                         positioned against the viewport.
#   chrome   x ranges containing NO text on either side. The RMSE over these is
#            the number that means something: it is the chrome itself, with
#            every glyph excluded. It should be at or near zero.
#   probe    a single empty column, scanned row by row. A structural test: it
#            ignores glyphs and asks only whether bar heights, separators,
#            bevels and background ramps land on the same rows.
#   rows     optional subset of the band's rows, relative to its top. The
#            wallpaper needs it: our badge shelf covers the middle of the strip
#            and the reference's is bare desktop, so only the rows above and
#            below the badges compare like with like.
BANDS = [
    dict(name="panel", y0=0, y1=23, anchor="top", probe=400,
         chrome=[(40, 540), (740, 1100)],
         note="site name and clock differ by design"),
    dict(name="title bar", y0=24, y1=48, anchor="top", probe=900,
         chrome=[(30, 410), (815, 1190)],
         note="page title differs by design"),
    dict(name="menu bar", y0=49, y1=73, anchor="top", probe=700,
         chrome=[(390, 1145)],
         note="File..Help labels differ only in rasterisation"),
    dict(name="toolbar", y0=74, y1=113, anchor="top", probe=700,
         chrome=[(420, 1200)],
         note="URL text differs by design"),
    dict(name="bookmarks", y0=114, y1=146, anchor="top", probe=1100,
         chrome=[(440, 1270)],
         note="nav labels differ by design"),
    dict(name="status bar", y0=605, y1=634, anchor="window", probe=700,
         chrome=[(60, 1250)],
         note="'Done' is the only text and it is identical"),
    dict(name="wallpaper", y0=635, y1=719, anchor="bottom", probe=300,
         chrome=[(0, 1280)], rows=list(range(0, 7)) + list(range(40, 48)),
         note="bare desktop rows above/below the badge shelf. ~6% is the FLOOR "
              "here: GNOME dithered the wallpaper down to 16-bit, so 77% of "
              "adjacent pixels in a smooth patch of the reference differ, "
              "against 27% for a clean resample. WebP quality is irrelevant "
              "to this number - lossless scores identically."),
]


def sh(*cmd):
    r = subprocess.run(cmd, capture_output=True)
    if r.returncode:
        sys.exit("verify.py: failed: %s\n%s" % (" ".join(cmd), r.stderr.decode()))
    return r.stdout


def pixels(path):
    raw = sh("magick", path, "-depth", "8", "rgb:-")
    if len(raw) != W * H * 3:
        sys.exit("verify.py: %s is not %dx%d" % (path, W, H))
    return raw


def to565(raw):
    """Truncate 24-bit RGB onto the RGB565 lattice, as a 16bpp X server did."""
    out = bytearray(raw)
    for i in range(0, len(out), 3):
        out[i] &= 0xF8
        out[i + 1] &= 0xFC
        out[i + 2] &= 0xF8
    return bytes(out)


def rmse(a, b, ay0, by0, rowlist, xranges):
    """Root-mean-square error over selected rows and x ranges, as a percentage
    of full scale."""
    total, n = 0, 0
    for r in rowlist:
        ao = (ay0 + r) * W * 3
        bo = (by0 + r) * W * 3
        for x0, x1 in xranges:
            for i in range(x0 * 3, x1 * 3):
                d = a[ao + i] - b[bo + i]
                total += d * d
                n += 1
    return 100.0 * (total / n) ** 0.5 / 255.0 if n else 0.0


def column_mismatch(a, b, x, ay0, by0, rowlist, tol=8):
    """Rows where a single column disagrees by more than `tol` on any channel."""
    bad = []
    for r in rowlist:
        ao = ((ay0 + r) * W + x) * 3
        bo = ((by0 + r) * W + x) * 3
        d = max(abs(a[ao + i] - b[bo + i]) for i in range(3))
        if d > tol:
            bad.append((by0 + r, tuple(a[ao:ao + 3]), tuple(b[bo:bo + 3]), d))
    return bad


def strip_height():
    conf = open(os.path.join(HERE, "site.conf"), encoding="utf-8").read()
    m = re.search(r"^StripHeight:\s*(\d+)", conf, re.M)
    return int(m.group(1)) if m else 48


# ------------------------------------------------------------------ geometry

def geometry(shot):
    """The content column is ours, not the reference's, so it is checked
    against ubuntu.css's own numbers rather than against the screenshot."""
    def px(x, y):
        o = (y * W + x) * 3
        return tuple(shot[o:o + 3])

    checks = []

    # The masthead is 90px, from ubuntu.css, and starts the row after the
    # bookmarks toolbar's separator (y146).
    checks.append(("masthead starts at y147",
                   px(1100, 147) != px(1100, 146), True))
    # header.png's last four rows are its bottom bevel: y233..236 in absolute
    # terms if and only if the masthead is exactly 90px tall.
    bevel = [px(1100, y) for y in (233, 234, 235, 236)]
    darkening = all(sum(bevel[i]) > sum(bevel[i + 1]) for i in range(2))
    checks.append(("masthead is exactly 90px (header.png bevel at y233-236)",
                   darkening, True))
    # Content background begins immediately after it.
    checks.append(("page background begins at y237",
                   px(1100, 237) == (0xF8, 0xFC, 0xF8), True))

    # ubuntu.css: #content { margin-left: 3em } at 14.4px, inside 5px of window
    # frame and gutter, puts body ink at x=48.
    h1 = None
    for y in range(240, 320):
        row = [x for x in range(6, 400)
               if sum(px(x, y)) < 360 and px(x, y) != (0xF8, 0xFC, 0xF8)]
        if row:
            h1 = (y, min(row))
            break
    checks.append(("first content ink starts at x=48 (3em left margin)",
                   h1 is not None and 46 <= h1[1] <= 50,
                   "x=%s" % (h1[1] if h1 else "none")))

    # The h1 rule: 2px of #6d4c07 running the width of the measure. Detected by
    # demanding a LONG continuous run of it — the h1's own glyphs are the same
    # colour, so a first-brown-pixel scan finds the text, not the rule.
    def brown(c):
        return abs(c[0] - 0x6D) < 14 and abs(c[1] - 0x4C) < 14 and c[2] < 0x34

    rule = None
    for y in range(250, 360):
        xs = [x for x in range(6, W - 6) if brown(px(x, y))]
        if len(xs) > 800 and max(xs) - min(xs) == len(xs) - 1:
            rule = (y, min(xs), max(xs))
            break
    checks.append(("h1 rule spans x=48 to x~1043 (15em right margin held)",
                   rule is not None and 46 <= rule[1] <= 50
                   and 1035 <= rule[2] <= 1050,
                   "y=%s x=%s-%s" % rule if rule else "not found"))
    return checks


# ---------------------------------------------------------------------- main

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--url", default="http://127.0.0.1:8804/tmp-ubuntu804/")
    ap.add_argument("--ref", default=os.path.join(
        HERE, "..", "Screenshot_testubuntu804_2026-09-03_10:19:10.png"))
    ap.add_argument("--shot", default=os.path.join(HERE, "..", ".work", "shots", "verify.png"))
    ap.add_argument("--strip", type=int, default=None,
                    help="strip height the page was built with (default: read site.conf)")
    ap.add_argument("--no-shoot", action="store_true", help="reuse an existing --shot")
    a = ap.parse_args()

    strip = a.strip if a.strip is not None else strip_height()
    shift = REF_STRIP - strip     # how far window-anchored bands move down

    if not a.no_shoot:
        os.makedirs(os.path.dirname(a.shot), exist_ok=True)
        sh("chromium", "--headless", "--disable-gpu", "--no-sandbox",
           "--force-device-scale-factor=1", "--disable-lcd-text",
           "--window-size=%d,%d" % (W, H),
           "--screenshot=" + a.shot, a.url)

    ref = to565(pixels(a.ref))
    ours = to565(pixels(a.shot))

    print("verify.py  %dx%d at 1x, both sides truncated to RGB565" % (W, H))
    print("           reference strip %dpx, ours %dpx -> window-anchored bands "
          "shift +%d\n" % (REF_STRIP, strip, shift))
    print("  %-11s %-13s %8s %8s   %s" % (
        "band", "rows (ours)", "chrome", "full", "structure"))
    print("  " + "-" * 76)

    worst = 0.0
    for b in BANDS:
        height = b["y1"] - b["y0"] + 1
        if b["anchor"] == "top":
            ay0 = by0 = b["y0"]
        elif b["anchor"] == "window":
            ay0, by0 = b["y0"], b["y0"] + shift
        else:                                      # bottom of the screen
            height = min(height, strip)
            ay0 = by0 = H - height
        rowlist = [r for r in b.get("rows", range(height)) if r < height]

        e_chrome = rmse(ref, ours, ay0, by0, rowlist, b["chrome"])
        e_full = rmse(ref, ours, ay0, by0, rowlist, [(0, W)])
        worst = max(worst, e_chrome)
        bad = column_mismatch(ref, ours, b["probe"], ay0, by0, rowlist)
        struct = ("clean at x=%d" % b["probe"] if not bad else
                  "%d/%d rows differ at x=%d" % (len(bad), len(rowlist), b["probe"]))
        print("  %-11s %-13s %7.2f%% %7.2f%%   %s" % (
            b["name"], "%d-%d" % (by0, by0 + height - 1),
            e_chrome, e_full, struct))
        for y, p, q, d in bad[:6]:
            print("      y%-4d ref #%02X%02X%02X  ours #%02X%02X%02X  (%d)"
                  % (y, p[0], p[1], p[2], q[0], q[1], q[2], d))
        if len(bad) > 6:
            print("      ... and %d more" % (len(bad) - 6))
        print("      note: %s" % b["note"])

    print("\n  geometry of the content column (checked against ubuntu.css, "
          "not the screenshot)")
    print("  " + "-" * 74)
    ok = True
    for label, passed, detail in geometry(ours):
        ok &= bool(passed)
        print("  [%s] %-58s %s" % ("ok" if passed else "FAIL", label,
                                   "" if detail is True else detail))

    print("\n  worst band RMSE: %.2f%%" % worst)
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
