# robin's page

A small static personal site. No build step.

Most pages open fine straight from disk. The **themed** pages (see below) are
the exception: they pick their stylesheet server-side via nginx SSI, so over
`file://` the stylesheet link resolves to `themes/.css` and the page renders
unstyled. Serve them through nginx to see them properly — the config lives in
`nginx/snippets/theme.conf` at the repo root.

## Pages

| File          | What it is                          |
|---------------|-------------------------------------|
| `index.html`  | home                                |
| `blog.html`   | blog index                          |
| `gzipt.html`  | a blog post                         |

## Theming

The themed pages carry a switcher pinned to the top-right. Themes are plain
stylesheets; **nginx** picks which one the page links to and remembers the
choice in a cookie. There is no JavaScript involved — the switcher is a GET
form that submits to `?theme=<id>`, and the correct stylesheet is in the first
byte of the response, so there is no flash of the wrong theme to work around.

Themed pages (the list is also spelled out in `nginx/snippets/theme.conf`, and
both have to agree): `index`, `blog`, `hypnospace`, `ntppost`, `ntpuserinfo`,
`slowqotd`, `gzipt`. The other pages under `personal/` are one-off things with
their own styling and are not themed.

- `base.css` — always loaded. Only structural things that hold for *every*
  theme (e.g. where the switcher sits). No aesthetics.
- `themes/plain.css` — deliberately (almost) empty: browser defaults.
- `themes/gtk2.css` — makes the page look like a GTK2 app. **Default.**
- `themes/motif.css` — makes the page look like an OSF/Motif (Xm) X11 app:
  battleship-grey face, chunky 2px highlight/shadow bevels, square corners,
  etched separators, an XmOptionMenu switcher.
- `themes/skeuslop.css` — Windows Vista / Aero (frosted-glass header, glossy
  buttons, blue Segoe UI headings).

Each theme styles both `#theme-switcher select` and `#theme-switcher button`
as a matching widget pair of its era. `plain.css` styles neither on purpose —
it gets the browser's native controls, which is the whole idea.

### Adding a theme

1. Create `themes/<id>.css` (including switcher chrome, or it inherits
   nothing and shows native controls).
2. Add `<id>` to **both** maps at the top of
   `nginx/sites-available/dreamstation.systems`. Anything not in the maps is
   rejected and falls back to the default.
3. Add an `<option>` to the switcher `<form>` in each themed page.

Step 2 is what makes it live; a stylesheet that exists but is not in the maps
is unreachable.

### Changing the default

Change the `default` in the `$theme_cookie` map — that value is what a visitor
with no cookie and no `?theme=` gets. Nothing is hard-coded in the pages any
more, so that is the only place it lives.

### Precedence

`?theme=<id>` beats the cookie, which beats the default. A visitor with
cookies disabled still gets the theme they picked on that page; it just does
not follow them to the next one, and the `?theme=` URL can be bookmarked to
pin a theme without cookies at all.
