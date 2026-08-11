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

package render

import (
	"fmt"
	"html"
	"net/url"
	"strings"
)

// HTMLOptions tunes HTML rendering.
type HTMLOptions struct {
	// SortBase is the path sortable column headers link to.
	SortBase string
	// CurrentSort and Desc mark the active sort for header arrows.
	CurrentSort string
	Desc        bool
}

// HTMLBody renders a Doc's blocks as HTML. The surrounding page chrome (head, nav, footer) lives in the web package's layout template; this produces only the main content, so the two concerns stay separable.
func HTMLBody(d *Doc, o HTMLOptions) string {
	var b strings.Builder

	for _, blk := range d.Blocks {
		switch v := blk.(type) {
		case Heading:
			lvl := clamp(v.Level, 2, 4)
			id := ""
			if v.Anchor != "" {
				id = fmt.Sprintf(` id="%s"`, escapeAttr(v.Anchor))
			}
			fmt.Fprintf(&b, "<h%d%s>%s</h%d>\n", lvl, id, escapeText(v.Text), lvl)

		case Para:
			cls := ""
			if v.Muted {
				cls = ` class="muted"`
			}
			fmt.Fprintf(&b, "<p%s>%s</p>\n", cls, escapeText(v.Text))

		case Pre:
			fmt.Fprintf(&b, "<pre>%s</pre>\n", escapeText(v.Text))

		case Facts:
			b.WriteString(`<div class="facts window">`)
			if v.Title != "" {
				fmt.Fprintf(&b, `<h3 class="title-bar"><span class="title-bar-text">%s</span></h3>`, escapeText(v.Title))
			}
			b.WriteString(`<dl class="window-body">`)
			for _, kv := range v.Pairs {
				title := ""
				if kv.Hint != "" {
					title = fmt.Sprintf(` title="%s"`, escapeAttr(kv.Hint))
				}
				fmt.Fprintf(&b, "<dt%s>%s</dt>", title, escapeText(kv.Key))
				val := escapeText(kv.Value)
				if kv.Link != "" {
					val = fmt.Sprintf(`<a href="%s">%s</a>`, escapeAttr(kv.Link), val)
				}
				fmt.Fprintf(&b, `<dd class="%s">%s</dd>`, toneClass(kv.Tone), val)
			}
			b.WriteString("</dl></div>\n")

		case Table:
			htmlTable(&b, v, o)

		case Links:
			if v.Title != "" {
				fmt.Fprintf(&b, "<h3>%s</h3>\n", escapeText(v.Title))
			}
			b.WriteString(`<ul class="linklist">`)
			for _, it := range v.Items {
				fmt.Fprintf(&b, `<li><a href="%s">%s</a>`, escapeAttr(it.Href), escapeText(it.Text))
				if it.Desc != "" {
					fmt.Fprintf(&b, ` <span class="muted">%s</span>`, escapeText(it.Desc))
				}
				b.WriteString("</li>")
			}
			b.WriteString("</ul>\n")

		case Chart:
			b.WriteString(`<figure class="chartbox window">`)
			if v.Title != "" {
				fmt.Fprintf(&b, `<figcaption class="title-bar"><span class="title-bar-text">%s</span></figcaption>`, escapeText(v.Title))
			}
			// The .window-body is not decoration. 7.css's .window paints its frame with a ::before pseudo-element stretched over the whole box at z-index -1, on the assumption that an opaque body is stacked on top of it; a .window with no body shows that frame through its own contents as a sheet of glass.
			b.WriteString(`<div class="window-body">`)
			b.WriteString(SVG(v, 720, v.Height))
			b.WriteString("</div></figure>\n")

		case Form:
			method := v.Method
			if method == "" {
				method = "get"
			}
			// Deliberately NOT .window, unlike the panels above. A window in 7.css is a frame plus a body, and this form is a flex container whose children are the fields themselves — there is nowhere to put a .window-body without breaking the layout, and a frame with no body shows through. Each skin draws the form panel itself.
			fmt.Fprintf(&b, `<form class="ogform" method="%s" action="%s">`,
				escapeAttr(method), escapeAttr(v.Action))
			if v.Prompt != "" {
				fmt.Fprintf(&b, `<p class="muted">%s</p>`, escapeText(v.Prompt))
			}
			for _, f := range v.Fields {
				htmlField(&b, f)
			}
			submit := v.Submit
			if submit == "" {
				submit = "Calculate"
			}
			fmt.Fprintf(&b, `<button type="submit">%s</button></form>`+"\n", escapeText(submit))

		case Raw:
			b.WriteString(v.HTML)
		}
	}

	if len(d.Footnotes) > 0 {
		b.WriteString(`<div class="notes window">` +
			`<h3 class="title-bar"><span class="title-bar-text">Notes</span></h3>` +
			`<ul class="window-body">`)
		for _, f := range d.Footnotes {
			fmt.Fprintf(&b, "<li>%s</li>", escapeText(f))
		}
		b.WriteString("</ul></div>\n")
	}
	return b.String()
}

func htmlTable(b *strings.Builder, t Table, o HTMLOptions) {
	id := ""
	if t.ID != "" {
		id = fmt.Sprintf(` id="%s"`, escapeAttr(t.ID))
	}
	fmt.Fprintf(b, `<div class="tablewrap"%s>`, id)
	if len(t.Rows) == 0 {
		msg := t.Empty
		if msg == "" {
			msg = "Nothing to show."
		}
		fmt.Fprintf(b, `<p class="muted">%s</p></div>`+"\n", escapeText(msg))
		return
	}
	b.WriteString(`<table>`)
	// A <caption> rather than a paragraph after the table: it is the table's accessible name, which matters on pages carrying two of them, and it puts the text above the grid where the text and gemtext renderers have always printed it.
	if t.Caption != "" {
		fmt.Fprintf(b, `<caption>%s</caption>`, escapeText(t.Caption))
	}
	b.WriteString(`<thead><tr>`)
	for _, c := range t.Columns {
		cls := "l"
		if c.Align == AlignRight {
			cls = "r"
		}
		title := ""
		if c.Hint != "" {
			title = fmt.Sprintf(` title="%s"`, escapeAttr(c.Hint))
		}
		if c.SortKey != "" && o.SortBase != "" {
			// Clicking the active column flips direction; clicking a new one starts descending, which is what you want for every money column and harmless for the rest.
			desc := true
			arrow, sorted := "", ""
			if o.CurrentSort == c.SortKey {
				desc = !o.Desc
				// aria-sort carries the state programmatically; the glyph is decoration once it does, and left exposed it reads as "black down-pointing small triangle" after the column name.
				if o.Desc {
					arrow, sorted = ` <span aria-hidden="true">▾</span>`, ` aria-sort="descending"`
				} else {
					arrow, sorted = ` <span aria-hidden="true">▴</span>`, ` aria-sort="ascending"`
				}
			}
			href := addParams(o.SortBase, map[string]string{
				"sort": c.SortKey,
				"dir":  map[bool]string{true: "desc", false: "asc"}[desc],
			})
			fmt.Fprintf(b, `<th scope="col" class="%s"%s%s><a href="%s">%s%s</a></th>`,
				cls, title, sorted, escapeAttr(href), escapeText(c.Title), arrow)
			continue
		}
		fmt.Fprintf(b, `<th scope="col" class="%s"%s>%s</th>`, cls, title, escapeText(c.Title))
	}
	b.WriteString(`</tr></thead><tbody>`)
	for _, r := range t.Rows {
		b.WriteString("<tr>")
		for i, c := range r {
			cls := "l"
			if i < len(t.Columns) && t.Columns[i].Align == AlignRight {
				cls = "r"
			}
			if tc := toneClass(c.Tone); tc != "" {
				cls += " " + tc
			}
			txt := escapeText(c.Text)
			if c.Link != "" {
				txt = fmt.Sprintf(`<a href="%s">%s</a>`, escapeAttr(c.Link), txt)
			}
			fmt.Fprintf(b, `<td class="%s">%s</td>`, cls, txt)
		}
		b.WriteString("</tr>")
	}
	b.WriteString("</tbody></table></div>\n")
}

func htmlField(b *strings.Builder, f Field) {
	id := "f_" + f.Name
	// A hidden field has no visible label and must not occupy a flex slot, or every form carrying one gains a mysterious empty column.
	if f.Kind == "hidden" {
		fmt.Fprintf(b, `<input type="hidden" name="%s" value="%s">`,
			escapeAttr(f.Name), escapeAttr(f.Value))
		return
	}
	// A checkbox is the one field whose label reads better beside the control than above it, so it gets a modifier class rather than the column layout every other field wants.
	cls := "field"
	if f.Kind == "checkbox" {
		cls = "field check"
	}
	fmt.Fprintf(b, `<label class="%s">`, cls)
	fmt.Fprintf(b, `<span>%s</span>`, escapeText(f.Label))
	switch f.Kind {
	case "select":
		fmt.Fprintf(b, `<select id="%s" name="%s">`, escapeAttr(id), escapeAttr(f.Name))
		for _, op := range f.Options {
			sel := ""
			if op.Selected {
				sel = " selected"
			}
			fmt.Fprintf(b, `<option value="%s"%s>%s</option>`,
				escapeAttr(op.Value), sel, escapeText(op.Label))
		}
		b.WriteString("</select>")
	case "checkbox":
		checked := ""
		if f.Value == "1" || f.Value == "on" {
			checked = " checked"
		}
		fmt.Fprintf(b, `<input type="checkbox" id="%s" name="%s" value="1"%s>`,
			escapeAttr(id), escapeAttr(f.Name), checked)
	default:
		kind := f.Kind
		if kind == "" {
			kind = "text"
		}
		fmt.Fprintf(b, `<input type="%s" id="%s" name="%s" value="%s">`,
			escapeAttr(kind), escapeAttr(id), escapeAttr(f.Name), escapeAttr(f.Value))
	}
	if f.Hint != "" {
		fmt.Fprintf(b, `<small class="muted">%s</small>`, escapeText(f.Hint))
	}
	b.WriteString("</label>")
}

func toneClass(t int) string {
	switch {
	case t > 0:
		return "good"
	case t < 0:
		return "bad"
	}
	return ""
}

// addParams merges query parameters into a URL that may already have some.
func addParams(base string, params map[string]string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func escapeText(s string) string { return html.EscapeString(s) }
func escapeAttr(s string) string { return html.EscapeString(s) }
