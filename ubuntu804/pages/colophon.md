Title: Colophon
Url: /colophon
Bookmark: Colophon
Order: 5
Desc: How the Ubuntu 8.04 desktop above was rebuilt in HTML and CSS, and what it cost.

# Colophon

This page is a recreation of an **Ubuntu 8.04 LTS “Hardy Heron”** desktop
running Firefox 3, rebuilt from a single 1280x720 screenshot. The desktop is
not an image. The panel, the window frame, the title bar, both toolbars, the
status bar and the masthead are HTML elements with CSS backgrounds, and the
page you are reading is inside the browser window.

## How the numbers were arrived at

The screenshot was treated as the specification and measured, not eyeballed.
Every band was sampled with ImageMagick a pixel column at a time — the GNOME
panel is 24px of `#e8e8e0`, the title bar is 22px of a gradient that darkens to
`#c88038` at row 37 and lifts again by row 46, the separators between toolbars
are two rows rather than one (`#c8c0b0` over `#f8fcf8`), and the masthead is
exactly 90px because `ubuntu.css` says so.

Three of the measurements were worth the trouble:

**The font size.** Ubuntu 8.04's default UI font was “Sans 10”, which at the X
server’s 96dpi is 13.333px. Rather than assume that, the six gaps between the
`File`&hellip;`Help` menu labels were measured and the pair *(font size,
padding)* solved by least squares. The answer came back 13.329px and 11.17px.
That is not a coincidence; it is a confirmation.

**The content font size.** `ubuntu.css` sets `html { font-size: 0.90em }`,
which against a 16px default is 14.4px. The independent check: `#content` has a
`3em` left margin, and 5px of window frame plus three times 14.4 puts body text
at x=48.2 — which is where the reference’s body text starts.

**The masthead’s light wash.** The reference’s masthead is bright behind the
logo and settles into flat tan on the right, which looks like two backgrounds
and is one. Ubuntu’s `headerlogo.png` is a *fully opaque* 454x90 bitmap whose
own background carries that gradient. It was sampled across and rebuilt here as
a twelve‐stop CSS gradient, so nothing of Canonical’s artwork ships in the
masthead at all.

## How close it gets, and why it cannot get closer

Every band of the chrome was diffed against the reference at 1× — the panel,
the title bar, the menu bar, both toolbars, the status bar and the exposed
wallpaper. Excluding text, six of the seven come out **byte‐identical**.

The seventh is the wallpaper, and finding out why was the most interesting
thing here. All 921,600 pixels of the reference screenshot sit exactly on the
RGB565 lattice: red and blue are multiples of 8, green of 4, with no
exceptions. It was captured from a 16‐bit framebuffer. That has two
consequences. Every colour measured off it is the truncated form of whatever
the Human theme really specified, so the comparison truncates this site’s
render the same way before measuring anything — otherwise a perfect match
would still report about 2% error. And GNOME *dithered* the wallpaper down to
those 16 bits: in a smooth patch of the reference strip, 77% of horizontally
adjacent pixels differ, against 27% for a clean resample of the same file.
That per‐pixel noise is unreproducible by anything drawing in 24‐bit colour, so
roughly 6% is the floor there. Lossless WebP scores no better than quality 78,
which is how you know the format is not the limiting factor.

## The clock

The clock in the panel is real, in UTC, and costs nothing:

    <!--#config timefmt="%a %b %e, %-l:%M %p" --><!--#echo var="date_gmt" -->

That is a server‐side include, expanded by nginx inside the worker process.
There is no CGI, no fork, no process, no disk access and no JavaScript involved
in telling you the time. On a machine whose real job is the NTP pool, spending
a Perl interpreter on a decoration would have been a poor trade.

`%e` is space‐padded and `%-l` is not, which reproduces the reference exactly:
two spaces in `Sep  3`, one after the comma. Plain `%l` gives you two in both
places, which is the kind of thing you only find out by trying it on the actual
server.

## What ships

**Fonts.** DejaVu Sans, the same face Ubuntu 8.04 fell back to, self‐hosted and
subset at build time to the codepoints this site actually uses. The full font
is also served, declared *before* the subset in the stylesheet so that the
subset wins for everything it covers and the full file is fetched only if a page
ever contains a character the subset lacks.

**Images.** The Heron wallpaper, the masthead’s header gradient, the Human
theme’s home and window‐decoration icons, Firefox 3's title bar icon and
toolbar throbber, and thirteen 88&times;31 badges drawn for this site with
ImageMagick and no antialiasing. Full attribution is in
[CREDITS.txt]({{ROOT}}/CREDITS.txt).

**No JavaScript.** Not “minimal”, not “progressive” — none. The menus in the
menu bar are `:focus-within`, keyboard‐reachable, and open with CSS. The window
buttons are decoration and are marked as such. The location bar is real text,
selectable and not typeable, because there is nothing to submit it to.

One consequence worth admitting: **File &rarr; Print&hellip; is inert**, along
with the rest of that menu, because printing needs `print()` and `print()`
needs JavaScript. The print stylesheet does the job instead — your browser’s
own Ctrl+P gives you the page without a picture of a desktop wrapped around it.

## Build

A Perl script reads a `Key: value` header off the top of each page file,
renders the body (`Text::Markdown` for `.md`, verbatim for `.html`),
interpolates it into the chrome template, and writes real static `.html` files
to real static paths. The bookmarks toolbar is generated from those headers, so
adding a page to the navigation means adding a page.

Nothing is assembled in your browser. Every URL here is a file.

## Deliberately not done

An iframe shell. A single‐page app that swaps content. Client‐side routing. A
fixed 1280x720 stage you have to pinch‐zoom. A CGI clock with GeoIP timezone
lookup, which would have meant a Perl fork on every page view and 60MB of
database on a machine with 3.1GB free. Back, forward, reload and stop buttons —
the reference screenshot does not have them, because whoever took it had
already customised them away, and the screenshot was the specification.
