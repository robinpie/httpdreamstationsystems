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

package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"dreamstation.systems/openget/internal/render"
	"dreamstation.systems/openget/internal/store"
	"dreamstation.systems/openget/internal/views"
)

// Rendering endpoints for the retro frontends.
//
// The CGI helpers under gophernicus and molly-brown, and the finger .nouser script, are all a handful of lines that fetch one of these URLs and print the body. Keeping the rendering here rather than in each script means all four protocols stay in step with the shared view models, and it is the only workable arrangement for finger anyway: the finger unit runs under ProtectSystem=strict, so its scripts cannot open the SQLite file at all (even a read needs write access for the WAL and shm sidecars). Talking to localhost HTTP is the one thing that sandbox does allow.
func (s *Server) retroRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /retro/{proto}/item", s.retroItem)
	m.HandleFunc("GET /retro/{proto}/search", s.retroSearch)
	m.HandleFunc("GET /retro/{proto}/page/{slug}", s.retroPage)
}

// retroWrite emits a Doc in the requested protocol's format.
func (s *Server) retroWrite(w http.ResponseWriter, proto string, d *render.Doc) {
	// These paths are for machine consumption by our own helpers; keeping them out of search results avoids indexing four near-duplicates of every page.
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Cache-Control", "public, max-age=60")

	switch proto {
	case "gemini":
		w.Header().Set("Content-Type", "text/gemini; charset=utf-8")
		fmt.Fprint(w, render.RetroLinks(render.Gemtext(d, render.GemtextOptions{
			Prefix: "/ge", Width: 72, WebBase: s.cfg.BaseURL,
		}), "/ge", proto))
	case "gopher":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, render.RetroLinks(render.Gophermap(d, render.GopherOptions{
			Host: gopherHost, Port: 70, Prefix: "/ge", Width: 70,
			SearchSelector: "/ge/cgi-bin/search",
		}), "/ge", proto))
	default: // "text", used by finger
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, render.Text(d, render.TextOptions{Width: 72, CRLF: true}))
	}
}

// gopherHost must match gophernicus's -h flag, or generated links point at a host that does not answer.
const gopherHost = "dreamstation.systems"

func validProto(p string) bool {
	switch p {
	case "gopher", "gemini", "text":
		return true
	}
	return false
}

// retroItem resolves an item by numeric id or by name and renders its page.
//
// Name resolution is generous on purpose: finger queries arrive as usernames, so "abyssal_whip" has to find the Abyssal whip. Underscores become spaces, an exact case-insensitive match wins, and anything else falls through to a search so the reply is a shortlist rather than a dead end.
func (s *Server) retroItem(w http.ResponseWriter, r *http.Request) {
	proto := r.PathValue("proto")
	if !validProto(proto) {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		q = strings.TrimSpace(r.URL.Query().Get("id"))
	}
	if q == "" {
		s.retroWrite(w, proto, notFoundDoc("No item requested.", ""))
		return
	}
	verbose := qBool(r, "verbose")

	if id, err := strconv.Atoi(q); err == nil {
		s.retroItemByID(w, r, proto, id, verbose)
		return
	}

	name := strings.ReplaceAll(q, "_", " ")
	if it, err := s.db.GetItemByName(ctx, name); err == nil {
		s.retroItemByID(w, r, proto, it.ID, verbose)
		return
	}

	// Never free-to-play filtered: Gopher and Gemini have no cookies to carry the site-wide toggle, so a capsule reader must see the whole catalogue.
	items, err := s.db.SearchItems(ctx, name, 15, false)
	if err != nil {
		s.log.Error("retro item search", "err", err)
		s.retroWrite(w, proto, notFoundDoc("Lookup failed.", ""))
		return
	}
	if len(items) == 1 {
		s.retroItemByID(w, r, proto, items[0].ID, verbose)
		return
	}
	if len(items) == 0 {
		s.retroWrite(w, proto, notFoundDoc(
			fmt.Sprintf("No such user, and no item matching %q.", name),
			"Try a shorter name, or browse the lists below."))
		return
	}
	d, err := s.views.Search(ctx, name)
	if err != nil {
		s.retroWrite(w, proto, notFoundDoc("Lookup failed.", ""))
		return
	}
	d.Title = fmt.Sprintf("No exact match for %q", name)
	d.Subtitle = "Did you mean one of these?"
	s.retroWrite(w, proto, d)
}

func (s *Server) retroItemByID(w http.ResponseWriter, r *http.Request, proto string, id int, verbose bool) {
	d, err := s.views.ItemPage(r.Context(), id, "1w")
	if errors.Is(err, store.ErrNotFound) {
		s.retroWrite(w, proto, notFoundDoc(fmt.Sprintf("No item with id %d.", id), ""))
		return
	}
	if err != nil {
		s.log.Error("retro item", "id", id, "err", err)
		s.retroWrite(w, proto, notFoundDoc("Lookup failed.", ""))
		return
	}
	if proto == "text" && !verbose {
		d = briefItem(d)
	}
	s.retroWrite(w, proto, d)
}

// briefItem trims an item page to its headline facts, for a finger query made without the /W switch.
func briefItem(d *render.Doc) *render.Doc {
	out := &render.Doc{Title: d.Title, Subtitle: d.Subtitle, Updated: d.Updated, Path: d.Path}
	for _, b := range d.Blocks {
		if f, ok := b.(render.Facts); ok && f.Title == "Prices" {
			out.Add(f)
			break
		}
	}
	out.Add(render.Para{Muted: true,
		Text: "Use \"finger /W " + slugify(d.Title) + "@dreamstation.systems\" for volume, buy limit, alch values and a chart."})
	return out
}

func slugify(s string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), " ", "_"))
}

func (s *Server) retroSearch(w http.ResponseWriter, r *http.Request) {
	proto := r.PathValue("proto")
	if !validProto(proto) {
		http.NotFound(w, r)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	d, err := s.views.Search(r.Context(), q)
	if err != nil {
		s.log.Error("retro search", "err", err)
		s.retroWrite(w, proto, notFoundDoc("Search failed.", ""))
		return
	}
	s.retroWrite(w, proto, d)
}

// retroPage renders one of the named list pages on demand, so a dynamic frontend can offer anything the static generator writes.
func (s *Server) retroPage(w http.ResponseWriter, r *http.Request) {
	proto := r.PathValue("proto")
	if !validProto(proto) {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	slug := r.PathValue("slug")

	var (
		d   *render.Doc
		err error
	)
	switch slug {
	case "home", "index":
		d, err = s.views.Home(ctx)
	case "alch":
		d, err = s.views.AlchList(ctx, views.AlchOptions{Limit: 40})
	case "alch-low":
		d, err = s.views.AlchList(ctx, views.AlchOptions{Spell: "low", Limit: 40})
	case "indices":
		d, err = s.views.IndicesPage(ctx)
	case "calc":
		d, err = s.views.CalcIndex(ctx)
	case "about":
		d, err = s.views.About(ctx)
	case "status":
		why, paused, free := "", false, int64(-1)
		if s.ing != nil {
			why, paused = s.ing.Paused()
			free = s.ing.FreeDiskMB()
		}
		d, err = s.views.Status(ctx, s.version, free, why, paused)
	default:
		if f, ok := views.FinderBySlug(slug); ok {
			o := f.Options
			o.Limit = 40
			d, _, err = s.views.ListPage(ctx, f.Title, "/flips/"+f.Slug, o, f.Blurb)
			break
		}
		if strings.HasPrefix(slug, "calc-") {
			d, err = s.views.CalcKind(ctx, strings.TrimPrefix(slug, "calc-"))
			break
		}
		s.retroWrite(w, proto, notFoundDoc("No such page: "+slug, ""))
		return
	}
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.retroWrite(w, proto, notFoundDoc("No such page: "+slug, ""))
			return
		}
		s.log.Error("retro page", "slug", slug, "err", err)
		s.retroWrite(w, proto, notFoundDoc("That page could not be built.", ""))
		return
	}
	s.retroWrite(w, proto, d)
}

func notFoundDoc(msg, hint string) *render.Doc {
	d := &render.Doc{Title: "OpenGET", NoIndex: true}
	d.Add(render.Para{Text: msg})
	if hint != "" {
		d.Add(render.Para{Muted: true, Text: hint})
	}
	d.Add(render.Links{Title: "Elsewhere", Items: []render.Link{
		{Text: "Highest margins", Href: "/margin"},
		{Text: "Highest ROI", Href: "/roi"},
		{Text: "High volume", Href: "/volume"},
		{Text: "Money makers", Href: "/calc"},
	}})
	return d
}

// retroItemCtx is a convenience used by tests.
func (s *Server) retroItemCtx(ctx context.Context, id int) (*render.Doc, error) {
	return s.views.ItemPage(ctx, id, "1w")
}
