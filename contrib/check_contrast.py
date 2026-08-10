#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
# Copyright (C) 2026 robinpie
#
# This program is free software; you can redistribute it and/or modify
# it under the terms of the GNU General Public License as published by
# the Free Software Foundation; version 2 of the License.
#
# This program is distributed in the hope that it will be useful,
# but WITHOUT ANY WARRANTY; without even the implied warranty of
# MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
# GNU General Public License for more details.

"""WCAG 2.2 AA contrast check for internal/web/static/openget.css.

Reads the palette out of the stylesheet's :root block rather than carrying a copy, so it cannot drift from what is actually served. Run it after touching any colour:

    python3 contrib/check_contrast.py

Exits non-zero if any pair fails, so it can be wired into a make target.

The value here is the PAIR LIST, not the arithmetic. Two things it encodes that are easy to forget:

  - Text on parchment must be checked against three grounds: the flat colour, the even-row tint, and the hover tint. The even row is always tightest and is what failed the 2026-08-08 audit.
  - 13.5px bold is NOT WCAG "large text". That threshold is 18.66px bold or 24px regular. Bolding a number does not lower its requirement.
"""

import pathlib
import re
import sys

CSS = pathlib.Path(__file__).resolve().parent.parent / "internal/web/static/openget.css"


def srgb(c):
    c /= 255.0
    return c / 12.92 if c <= 0.04045 else ((c + 0.055) / 1.055) ** 2.4


def lum(h):
    h = h.lstrip("#")
    if len(h) == 3:
        h = "".join(x * 2 for x in h)
    r, g, b = (int(h[i:i + 2], 16) for i in (0, 2, 4))
    return 0.2126 * srgb(r) + 0.7152 * srgb(g) + 0.0722 * srgb(b)


def cr(a, b):
    l1, l2 = lum(a), lum(b)
    if l1 < l2:
        l1, l2 = l2, l1
    return (l1 + 0.05) / (l2 + 0.05)


def blend(fg, alpha, bg):
    """Composite fg over bg at alpha. Needed for the rgba() row tints, which
    are what the eye actually sees behind table text."""
    f, b = fg.lstrip("#"), bg.lstrip("#")
    return "#" + "".join(
        "%02x" % round(int(f[i:i + 2], 16) * alpha + int(b[i:i + 2], 16) * (1 - alpha))
        for i in (0, 2, 4)
    )


def palette():
    """Pull --name: #hex; pairs out of the first :root block."""
    text = CSS.read_text(encoding="utf-8")
    start = text.index(":root {")
    block = text[start:text.index("\n}", start)]
    p = dict(re.findall(r"--([a-z0-9-]+):\s*(#[0-9a-fA-F]{3,6})\s*;", block))
    if not p:
        sys.exit(f"no custom properties found in {CSS}")
    return p


def main():
    p = palette()
    parch = p["parch-0"]
    even = blend("#78541e", 0.10, parch)      # tbody tr:nth-child(even)
    hover = blend("#ffbb22", 0.20, parch)     # tbody tr:hover
    grid = blend(p["wood-pale"], 0.22, p["void"])

    # (label, fg, bg, required ratio)
    text_pairs = [
        ("body --text on --ground",            p["text"],     p["ground"],    4.5),
        (".muted --text-dim on --ground",      p["text-dim"], p["ground"],    4.5),
        ("h1/h2 --gold on --ground (large)",   p["gold"],     p["ground"],    3.0),
        ("h3 --parch-0 on --ground",           p["parch-0"],  p["ground"],    4.5),
        ("code --amber on --ground",           p["amber"],    p["ground"],    4.5),
        ("link --link on --ground",            p["link"],     p["ground"],    4.5),
        ("visited --link-vis on --ground",     p["link-vis"], p["ground"],    4.5),
        ("pre --parch-1 on --void",            p["parch-1"],  p["void"],      4.5),
        ("nav tab --parch-1 on --wood-mid",    p["parch-1"],  p["wood-mid"],  4.5),
        ("nav tab current --ink on --parch-0", p["ink"],      p["parch-0"],   4.5),
        ("brand small --text-dim on --wood-mid", p["text-dim"], p["wood-mid"], 4.5),
        ("search placeholder on --void",       "#7d745f",     p["void"],      4.5),
        ("thead th --gold on --wood-mid",      p["gold"],     p["wood-mid"],  4.5),
        ("caption --text-dim on --ground",     p["text-dim"], p["ground"],    4.5),
        # Table body: three grounds each. 13.5px bold is not large text.
        ("td --ink on parchment",              p["ink"],      parch,          4.5),
        ("td --ink on even row",               p["ink"],      even,           4.5),
        ("td.good --good-ink on parchment",    p["good-ink"], parch,          4.5),
        ("td.good --good-ink on even row",     p["good-ink"], even,           4.5),
        ("td.bad --bad-ink on parchment",      p["bad-ink"],  parch,          4.5),
        ("td.bad --bad-ink on even row",       p["bad-ink"],  even,           4.5),
        ("td link --link-ink on parchment",    p["link-ink"], parch,          4.5),
        ("td link --link-ink on even row",     p["link-ink"], even,           4.5),
        ("td link --link-ink on hover row",    p["link-ink"], hover,          4.5),
        # .facts and .ogform grounds run wood-dark -> void, so check both ends.
        ("facts dt --text-dim on --wood-dark", p["text-dim"], p["wood-dark"], 4.5),
        ("facts dd --parch-0 on --void",       p["parch-0"],  p["void"],      4.5),
        ("facts dd.good --good on --void",     p["good"],     p["void"],      4.5),
        ("facts dd.bad --bad on --wood-dark",  p["bad"],      p["wood-dark"], 4.5),
        ("facts dd.bad --bad on --void",       p["bad"],      p["void"],      4.5),
        ("field label --gold-dim on --void",   p["gold-dim"], p["void"],      4.5),
        ("button --gold on --wood",            p["gold"],     p["wood"],      4.5),
        ("notes li --text-dim on --wood-dark", p["text-dim"], p["wood-dark"], 4.5),
        ("chart ylab --text-dim on --void",    p["text-dim"], p["void"],      4.5),
        ("chart legend --parch-1 on --void",   p["parch-1"],  p["void"],      4.5),
        ("skip link --ink on --parch-0",       p["ink"],      p["parch-0"],   4.5),
        ("footer --text-dim on --wood",        p["text-dim"], p["wood"],      4.5),
        ("footer link --link on --wood",       p["link"],     p["wood"],      4.5),
    ]

    # 1.4.11: control boundaries and focus rings need 3:1 against what is adjacent to them, which for a field is whatever the form ground has faded to at that point.
    ui_pairs = [
        ("input border vs black fill",         p["wood-pale"], p["void"],     3.0),
        ("search border vs masthead",          p["wood-pale"], p["wood"],     3.0),
        ("input border vs form ground",        p["wood-pale"], p["wood-dark"], 3.0),
        ("button border vs form ground",       p["wood-pale"], p["void"],     3.0),
        ("focus ring --gold on --ground",      p["gold"],      p["ground"],   3.0),
        ("focus ring --gold on thead --wood",  p["gold"],      p["wood"],     3.0),
        # Gold is 1.03:1 on parchment, hence the tbody override.
        ("focus ring --ink on parchment",      p["ink"],       parch,         3.0),
        ("chart sell #ffbb22 on --void",       "#ffbb22",      p["void"],     3.0),
        ("chart buy #78adff on --void",        "#78adff",      p["void"],     3.0),
    ]

    fails = []
    for title, pairs in (("text (1.4.3)", text_pairs), ("non-text (1.4.11)", ui_pairs)):
        print(f"\n=== {title} ===")
        for label, fg, bg, need in pairs:
            r = cr(fg, bg)
            ok = r >= need
            if not ok:
                fails.append((label, r, need))
            print(f"{'PASS' if ok else 'FAIL':4} {r:6.2f} (need {need})  {label}")

    # Not a failure: gridlines are decoration, the y-axis labels carry the values. Printed so nobody promotes them to load-bearing by accident.
    print(f"\n(gridline vs --void is {cr(grid, p['void']):.2f} — decorative, "
          "the ylab text carries the numbers)")

    if fails:
        print(f"\n{len(fails)} FAILURES")
        for label, r, need in fails:
            print(f"  {r:5.2f} < {need}  {label}")
        return 1
    print(f"\nAll {len(text_pairs) + len(ui_pairs)} pairs pass WCAG 2.2 AA.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
