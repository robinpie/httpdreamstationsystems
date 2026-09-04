#!/usr/bin/env python3
"""Build favicon.ico (32px + 16px) and apple-touch-icon.png (180px) from the
same geometry as favicon.svg.

ImageMagick's internal SVG renderer ignores stroke-width through <use>, so both
are drawn from primitives here rather than rasterised out of the SVG. If you
change the geometry in favicon.svg, change it here too and re-run.

The SVG is theme-aware via prefers-color-scheme; neither output format can be.
The ICO uses a mid grey that stays legible against both light and dark browser
chrome. The apple-touch-icon cannot use the same trick: iOS composites any
transparency onto black, which would hide a dark mark entirely, so it is drawn
dark-on-white as an opaque tile. Do not add rounded corners -- iOS applies its
own mask and pre-rounded art gets clipped twice.
"""
import subprocess, sys, os, tempfile

OUT = sys.argv[1] if len(sys.argv) > 1 else \
    "/home/robin/configNotes/http/rootdomain/favicon.ico"
TOUCH_OUT = os.path.join(os.path.dirname(OUT), "apple-touch-icon.png")
COLOR = "#666666"
TOUCH_FG, TOUCH_BG, TOUCH_PX = "#111111", "#ffffff", 180

S = 512 / 32.0                                  # SVG user units -> 512px render
STARS = [(16, 8.6), (8.2, 22.4), (23.8, 22.4)]  # asterism: one up, two down
ARMS = [((0, -5), (0, 5)),
        ((-4.35, -2.5), (4.35, 2.5)),
        ((-4.35, 2.5), (4.35, -2.5))]
STROKE = 3

draw = ["stroke-linecap round"]
for cx, cy in STARS:
    for (x1, y1), (x2, y2) in ARMS:
        draw.append("line %.2f,%.2f %.2f,%.2f"
                    % ((cx + x1) * S, (cy + y1) * S,
                       (cx + x2) * S, (cy + y2) * S))

tmp = tempfile.mkdtemp()
big, p32, p16 = (os.path.join(tmp, n) for n in ("big.png", "32.png", "16.png"))

subprocess.run(["magick", "-size", "512x512", "xc:none", "-stroke", COLOR,
                "-strokewidth", str(STROKE * S), "-fill", "none",
                "-draw", " ".join(draw), big], check=True)
for path, size in ((p32, 32), (p16, 16)):
    subprocess.run(["magick", big, "-resize", "%dx%d" % (size, size), path],
                   check=True)
subprocess.run(["magick", p32, p16, OUT], check=True)
print("wrote", OUT)

# Apple touch icon: same geometry, opaque, dark-on-white, with a margin. The
# mark is redrawn rather than recoloured from big.png because that render is
# transparent-background antialiased grey, and flattening it onto white would
# leave grey fringing. INNER leaves ~8% padding on each side; iOS crops nothing
# but the art looks cramped edge-to-edge next to other home screen icons.
INNER = int(TOUCH_PX * 0.84)
touch_big = os.path.join(tmp, "touch.png")
subprocess.run(["magick", "-size", "512x512", "xc:" + TOUCH_BG,
                "-stroke", TOUCH_FG, "-strokewidth", str(STROKE * S),
                "-fill", "none", "-draw", " ".join(draw), touch_big], check=True)
subprocess.run(["magick", touch_big,
                "-resize", "%dx%d" % (INNER, INNER),
                "-background", TOUCH_BG, "-gravity", "center",
                "-extent", "%dx%d" % (TOUCH_PX, TOUCH_PX),
                "-alpha", "off", TOUCH_OUT], check=True)
print("wrote", TOUCH_OUT)
