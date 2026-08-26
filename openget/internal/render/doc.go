// SPDX-License-Identifier: GPL-2.0-only
// Copyright (C) 2026 robinpie
//
// This program is free software; you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation; version 2 of the License.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.

// Package render defines protocol-agnostic view models and the renderers that turn them into HTML, gemtext, gophermaps or plain text.
//
// This split is the reason OpenGET can serve Gopher, Gemini, Spartan and finger without being four applications. A handler builds a Doc — headings, tables, links, charts — and knows nothing about the transport. Each protocol gets a renderer that walks the same Doc. Adding a page gives every frontend that page; adding a protocol gives it every page.
//
// The alternative, templating each protocol separately, means every new calculator is written four times and drifts three ways.
package render

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Doc is a whole page.
type Doc struct {
	Title    string
	Subtitle string
	// Path is the canonical HTTP path, used to build cross-protocol links.
	Path   string
	Blocks []Block
	// Footnotes render at the bottom in every protocol. Caveats about data quality belong here so they cannot be shown on the web and quietly dropped from the capsule.
	Footnotes []string
	// Updated is the data timestamp shown to the user.
	Updated time.Time
	// NoIndex marks pages that must stay out of search engines and sitemaps (anything behind a bearer token).
	NoIndex bool
}

// Add appends blocks.
func (d *Doc) Add(b ...Block) { d.Blocks = append(d.Blocks, b...) }

// Note appends a footnote.
func (d *Doc) Note(format string, args ...any) {
	d.Footnotes = append(d.Footnotes, fmt.Sprintf(format, args...))
}

// Block is one element of a Doc.
type Block interface{ isBlock() }

// Heading is a section heading. Level 1 is the page title, so blocks normally start at 2.
type Heading struct {
	Level int
	Text  string
	// Anchor is an optional id for HTML deep links.
	Anchor string
}

func (Heading) isBlock() {}

// Para is a paragraph of prose.
type Para struct {
	Text string
	// Muted marks secondary text (caveats, provenance).
	Muted bool
}

func (Para) isBlock() {}

// Pre is preformatted text, rendered verbatim in every protocol.
type Pre struct {
	Text string
	// Alt describes the content for readers that cannot show it well.
	Alt string
}

func (Pre) isBlock() {}

// Align controls column alignment in tables.
type Align int

const (
	AlignLeft Align = iota
	AlignRight
)

// Column describes one table column.
type Column struct {
	Title string
	Align Align
	// RowHeader marks the column that names its row — the item, the method, the index. HTML promotes those cells to <th scope="row">, so a screen reader reading across row 40 announces "Abyssal whip, Margin, 3,393" rather than a bare number. Set it only on a column that genuinely identifies the row: on a grid of timestamps or a one-column list it adds noise and no meaning.
	RowHeader bool
	// SortKey, when set, makes the column header a sort link on the web.
	SortKey string
	// Hint is a tooltip / footnote explaining the column.
	Hint string
	// Retro false hides the column from the text-shaped protocols, where horizontal room is scarce. The data is identical; only density differs.
	Retro bool
}

// Cell is one table cell. Text is what every protocol prints; Link, when set, makes it navigable.
type Cell struct {
	Text string
	Link string
	// Tone tints the cell on the web: +1 good, -1 bad, 0 neutral. Text protocols ignore it, so the Text must stand alone — a red "-202" has to read as a loss without the colour.
	Tone int
	// Numeric is the underlying value, used for machine-readable output.
	Numeric *float64
	// At, when set, is the instant Text describes ("3m ago", "22:24:20 UTC"). HTML wraps the cell in <time datetime>, which is the difference between a string a machine has to guess at and one it can read — and these pages are served with a minute of shared cache, so the rendered "3m ago" is not necessarily true when it is read.
	At time.Time
}

// C builds a plain cell.
func C(text string) Cell { return Cell{Text: text} }

// CL builds a linked cell.
func CL(text, link string) Cell { return Cell{Text: text, Link: link} }

// Table is a grid.
type Table struct {
	Columns []Column
	Rows    [][]Cell
	// Caption is shown above the table.
	Caption string
	// Empty is the message shown when there are no rows.
	Empty string
	// ID is an HTML anchor.
	ID string
}

func (Table) isBlock() {}

// Link is one navigable entry.
type Link struct {
	Text string
	Href string
	Desc string
	// Current marks the entry the reader is already looking at, becoming aria-current="page" in HTML. Text protocols ignore it.
	Current bool
	// Rel is the HTML link relationship ("prev", "next"). Ignored outside HTML.
	Rel string
}

// Links is a list of links — a menu on Gopher, a list on the web.
type Links struct {
	Title string
	// Level is the heading level for Title on the web. Zero means 2, as for Facts.
	Level int
	// NavLabel, when set, wraps the list in <nav aria-label="..."> on the web, for a set of links that is a way around the page rather than part of its content. Empty leaves it a plain list.
	NavLabel string
	Items    []Link
}

func (Links) isBlock() {}

// KV is one label/value pair.
type KV struct {
	Key   string
	Value string
	Hint  string
	Tone  int
	Link  string
	// At is Cell.At for a fact panel: the instant Value describes, rendered as <time datetime> on the web.
	At time.Time
}

// Facts is a definition list: the item page's price/margin/limit panel.
type Facts struct {
	Title string
	// Level is the heading level for Title on the web. Zero means 2 — these panels are page sections, and hardcoding a depth here is what made every item and calculator page jump from h1 straight to h3. Set it only when a panel is genuinely nested under a Heading of its own.
	Level int
	Pairs []KV
}

func (Facts) isBlock() {}

// Series is one line on a chart.
type Series struct {
	Name   string
	Points []XY
	// Colour is a CSS colour for HTML output; ignored elsewhere.
	Colour string
	// Dash is an SVG stroke-dasharray. Where a chart carries more than one series, at least one of them needs it: two lines separated by hue alone are one line to a reader who cannot use hue, and the legend swatch does not help — matching a chip to a line is the same colour judgement again. Ignored outside HTML.
	Dash string
}

// XY is one charted point. X is a unix timestamp.
type XY struct {
	X int64
	Y float64
}

// Chart is a time series plot. HTML renders server-side SVG; gemtext and the text protocols render a Unicode sparkline, since neither has images.
type Chart struct {
	Title  string
	Series []Series
	// YLabel and Height tune the SVG.
	YLabel string
	Height int
	// AltLink points at a raster or data version for clients that want one.
	AltLink string
}

func (Chart) isBlock() {}

// Form is an input form. Only HTTP renders it interactively; Gopher gets a type-7 search selector, Gemini an input prompt, and plain text a note describing the query syntax.
type Form struct {
	Action string
	Method string
	Prompt string
	Fields []Field
	Submit string
	// WebOnly drops the form from every text protocol. Gemini's input mechanism is one line of text and Gopher's is one search string, so a form with more than one control has no honest equivalent there — and rendering it anyway produces a prompt that silently discards every field but the first, or a type-7 entry pointing at the item search CGI. Retro readers get the same page with the defaults applied instead.
	WebOnly bool
}

func (Form) isBlock() {}

// Field is one form input.
type Field struct {
	Name    string
	Label   string
	Value   string
	Kind    string // "text", "number", "select", "checkbox"
	Options []Option
	Hint    string
}

// Option is one choice in a select field.
type Option struct {
	Value, Label string
	Selected     bool
}

// Raw is an escape hatch for HTML that has no meaningful text equivalent (a stylesheet link, a script). Text protocols drop it entirely, so nothing load-bearing may live here.
type Raw struct{ HTML string }

func (Raw) isBlock() {}

// ---------------------------------------------------------------------------
// Formatting helpers, shared by every renderer so a number reads the same on the web as it does over finger.
// ---------------------------------------------------------------------------

// GP formats a coin amount with thousands separators: 842651 -> "842,651".
func GP(n int64) string {
	s := strconv.FormatInt(abs64(n), 10)
	var b strings.Builder
	if n < 0 {
		b.WriteByte('-')
	}
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// GPShort abbreviates large amounts the way the game does: 842651 -> "842.7k", 1_500_000 -> "1.5m", 2_400_000_000 -> "2.4b".
func GPShort(n int64) string {
	a := abs64(n)
	sign := ""
	if n < 0 {
		sign = "-"
	}
	switch {
	case a >= 1_000_000_000:
		return sign + trimZero(float64(a)/1e9) + "b"
	case a >= 1_000_000:
		return sign + trimZero(float64(a)/1e6) + "m"
	case a >= 10_000:
		return sign + trimZero(float64(a)/1e3) + "k"
	default:
		return GP(n)
	}
}

func trimZero(f float64) string {
	s := strconv.FormatFloat(f, 'f', 1, 64)
	return strings.TrimSuffix(s, ".0")
}

// Pct formats a percentage to one decimal with an explicit sign.
func Pct(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "—"
	}
	return fmt.Sprintf("%+.1f%%", f)
}

// PctPlain formats a percentage without forcing a sign.
func PctPlain(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", f)
}

// Dash is the placeholder for a value that has never been observed. Used everywhere instead of 0, because "nobody has traded this" and "this is free" are very different claims.
const Dash = "—"

// GPOpt formats a possibly-absent amount.
func GPOpt(p *int64) string {
	if p == nil {
		return Dash
	}
	return GP(*p)
}

// PctOpt formats a possibly-absent percentage.
func PctOpt(p *float64) string {
	if p == nil {
		return Dash
	}
	return Pct(*p)
}

// Tone maps a signed value to a cell tone.
func Tone(v int64) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

// ToneF maps a signed float to a cell tone.
func ToneF(v float64) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

// Ago renders a timestamp as a compact relative age: "3m ago", "2h ago".
func Ago(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// AgoUnix is Ago for a unix timestamp pointer.
func AgoUnix(p *int64) string {
	if p == nil {
		return Dash
	}
	return Ago(time.Unix(*p, 0))
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// RetroLinks maps the HTTP paths baked into the shared view models onto the retro tree layout, where prefix is the tree root (e.g. "/ge").
//
// View models are built once for every protocol and naturally speak in web paths (/item/4151, /calc/herblore). Rather than teach every page builder about four link schemes, the mapping is applied once on the way out — and it has to be applied by BOTH the static generator and the dynamic endpoints, or a menu links somewhere its own search results do not.
//
// The dynamic paths differ per protocol because the two servers locate CGI differently: gophernicus runs anything under the doc root's cgi-bin, taking its argument after a "?", while molly-brown matches a CGIPaths glob and passes the rest of the path as PATH_INFO.
func RetroLinks(body, prefix, proto string) string {
	itemPath, searchPath := prefix+"/cgi-bin/item?", prefix+"/cgi-bin/search"
	if proto == "gemini" {
		itemPath, searchPath = prefix+"/cgi-bin/ge/item/", prefix+"/cgi-bin/ge/search"
	}
	return strings.NewReplacer(
		prefix+"/flips/", prefix+"/",
		prefix+"/item/", itemPath,
		prefix+"/calc/", prefix+"/calc-",
		prefix+"/indices/", prefix+"/indices#",
		prefix+"/ge-tax-calculator", prefix+"/about",
		prefix+"/store-profit", prefix+"/alch",
		prefix+"/search", searchPath,
	).Replace(body)
}

// pad left- or right-aligns s to width w, counting runes rather than bytes so the em dash placeholder does not break column alignment in a terminal.
func pad(s string, w int, a Align) string {
	n := utf8.RuneCountInString(s)
	if n >= w {
		return s
	}
	fill := strings.Repeat(" ", w-n)
	if a == AlignRight {
		return fill + s
	}
	return s + fill
}

// truncate shortens s to at most w runes, ending with an ellipsis.
func truncate(s string, w int) string {
	if w <= 0 || utf8.RuneCountInString(s) <= w {
		return s
	}
	r := []rune(s)
	if w == 1 {
		return "…"
	}
	return string(r[:w-1]) + "…"
}
