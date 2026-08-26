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
	"strings"
)

// TextOptions tunes plain-text rendering.
type TextOptions struct {
	// Width is the target line width. 72 suits finger and terminal readers.
	Width int
	// CRLF emits RFC-style line endings, which finger wants.
	CRLF bool
	// ShowLinks appends a URL list; off for finger, where nothing is clickable and the noise costs more than it gives.
	ShowLinks bool
	// BaseURL prefixes relative links when ShowLinks is set.
	BaseURL string
}

// Text renders a Doc as plain text.
//
// This is the lowest common denominator and the one every other text-shaped protocol borrows table layout from: if a table reads correctly here, it reads correctly over finger, Gopher and Gemini.
func Text(d *Doc, o TextOptions) string {
	if o.Width <= 0 {
		o.Width = 72
	}
	var b strings.Builder
	w := &lineWriter{b: &b, crlf: o.CRLF}

	if d.Title != "" {
		w.line(d.Title)
		w.line(strings.Repeat("=", min(len(d.Title), o.Width)))
	}
	if d.Subtitle != "" {
		w.line(d.Subtitle)
	}
	if d.Title != "" || d.Subtitle != "" {
		w.blank()
	}

	var links []Link
	for _, blk := range d.Blocks {
		switch v := blk.(type) {
		case Heading:
			w.line(strings.ToUpper(v.Text))
			w.line(strings.Repeat("-", min(len(v.Text), o.Width)))
		case Para:
			for _, l := range wrap(v.Text, o.Width) {
				w.line(l)
			}
			w.blank()
		case Pre:
			for _, l := range strings.Split(strings.TrimRight(v.Text, "\n"), "\n") {
				w.line(l)
			}
			w.blank()
		case Facts:
			if v.Title != "" {
				w.line(v.Title)
			}
			keyW := 0
			for _, p := range v.Pairs {
				keyW = max(keyW, len(p.Key))
			}
			for _, p := range v.Pairs {
				w.line("  " + pad(p.Key+":", keyW+1, AlignLeft) + " " + p.Value)
			}
			w.blank()
		case Table:
			for _, l := range textTable(v, o.Width) {
				w.line(l)
			}
			w.blank()
		case Links:
			if v.Title != "" {
				w.line(v.Title)
			}
			for _, it := range v.Items {
				line := "  * " + it.Text
				if it.Desc != "" {
					line += " — " + it.Desc
				}
				w.line(line)
				links = append(links, it)
			}
			w.blank()
		case Chart:
			for _, s := range v.Series {
				if len(s.Points) == 0 {
					continue
				}
				w.line(fmt.Sprintf("  %-6s %s", s.Name, Sparkline(s.Points, min(48, o.Width-10))))
			}
			if lo, hi, ok := chartRange(v); ok {
				w.line(fmt.Sprintf("         low %s  high %s", GPShort(int64(lo)), GPShort(int64(hi))))
			}
			w.blank()
		case Form:
			if v.WebOnly {
				continue
			}
			if v.Prompt != "" {
				w.line(v.Prompt)
			}
			w.blank()
		case Raw:
			// HTML-only; deliberately dropped.
		}
	}

	if len(d.Footnotes) > 0 {
		w.line(strings.Repeat("-", min(40, o.Width)))
		for _, f := range d.Footnotes {
			for i, l := range wrap(f, o.Width-2) {
				if i == 0 {
					w.line("* " + l)
				} else {
					w.line("  " + l)
				}
			}
		}
	}
	if o.ShowLinks && len(links) > 0 {
		w.blank()
		w.line("Links:")
		for _, l := range links {
			w.line("  " + l.Text + " -> " + o.BaseURL + l.Href)
		}
	}
	return b.String()
}

type lineWriter struct {
	b       *strings.Builder
	crlf    bool
	pending bool // a blank line is queued but not yet written
}

func (w *lineWriter) line(s string) {
	if w.pending {
		w.raw("")
		w.pending = false
	}
	w.raw(s)
}

// blank queues a blank line rather than writing it, so consecutive blocks never produce a run of empty lines and a trailing blank never ships.
func (w *lineWriter) blank() { w.pending = true }

func (w *lineWriter) raw(s string) {
	w.b.WriteString(s)
	if w.crlf {
		w.b.WriteString("\r\n")
	} else {
		w.b.WriteString("\n")
	}
}

// textTable lays out a table as aligned columns, dropping columns marked Retro:false and shrinking the widest column until the whole thing fits.
func textTable(t Table, width int) []string {
	cols, rows := retroColumns(t)
	if len(cols) == 0 {
		return nil
	}
	var out []string
	if t.Caption != "" {
		out = append(out, t.Caption)
	}
	if len(rows) == 0 {
		msg := t.Empty
		if msg == "" {
			msg = "(nothing to show)"
		}
		return append(out, "  "+msg)
	}

	w := make([]int, len(cols))
	for i, c := range cols {
		w[i] = len(c.Title)
	}
	for _, r := range rows {
		for i := range cols {
			if i < len(r) {
				w[i] = max(w[i], runeLen(r[i].Text))
			}
		}
	}

	// Shrink the widest column repeatedly until the table fits. Names are normally the widest and the most tolerant of truncation.
	const gap = 2
	fits := func() int {
		total := gap * (len(cols) - 1)
		for _, x := range w {
			total += x
		}
		return total
	}
	for fits() > width {
		widest, wi := 0, -1
		for i, x := range w {
			if x > widest {
				widest, wi = x, i
			}
		}
		if wi < 0 || w[wi] <= 6 {
			break
		}
		w[wi]--
	}

	sep := strings.Repeat(" ", gap)
	var head []string
	for i, c := range cols {
		head = append(head, pad(truncate(c.Title, w[i]), w[i], c.Align))
	}
	out = append(out, strings.TrimRight(strings.Join(head, sep), " "))

	var rule []string
	for i := range cols {
		rule = append(rule, strings.Repeat("-", w[i]))
	}
	out = append(out, strings.Join(rule, sep))

	for _, r := range rows {
		var cells []string
		for i, c := range cols {
			txt := ""
			if i < len(r) {
				txt = r[i].Text
			}
			cells = append(cells, pad(truncate(txt, w[i]), w[i], c.Align))
		}
		out = append(out, strings.TrimRight(strings.Join(cells, sep), " "))
	}
	return out
}

// retroColumns drops columns hidden from text protocols and the matching cells.
func retroColumns(t Table) ([]Column, [][]Cell) {
	keep := make([]int, 0, len(t.Columns))
	var cols []Column
	for i, c := range t.Columns {
		if c.Retro {
			keep = append(keep, i)
			cols = append(cols, c)
		}
	}
	if len(cols) == 0 { // no column opted in: show them all
		return t.Columns, t.Rows
	}
	rows := make([][]Cell, 0, len(t.Rows))
	for _, r := range t.Rows {
		nr := make([]Cell, 0, len(keep))
		for _, i := range keep {
			if i < len(r) {
				nr = append(nr, r[i])
			} else {
				nr = append(nr, Cell{})
			}
		}
		rows = append(rows, nr)
	}
	return cols, rows
}

// wrap breaks text to width on word boundaries.
func wrap(s string, width int) []string {
	if width <= 0 {
		width = 72
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	var out []string
	line := words[0]
	for _, wd := range words[1:] {
		if runeLen(line)+1+runeLen(wd) > width {
			out = append(out, line)
			line = wd
			continue
		}
		line += " " + wd
	}
	return append(out, line)
}

func runeLen(s string) int { return len([]rune(s)) }
