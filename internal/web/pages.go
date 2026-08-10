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
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"dreamstation.systems/openget/internal/render"
	"dreamstation.systems/openget/internal/store"
	"dreamstation.systems/openget/internal/views"
)

// ---------------------------------------------------------------------------
// The site-wide free-to-play toggle
// ---------------------------------------------------------------------------

// f2pCookie carries the site-wide "hide members' items" checkbox.
//
// A plain cookie rather than a stored preference: the filter has to work for a reader who has never saved anything here, and minting a token — the thing this site calls an account — merely to hide half the catalogue would be a strange price to charge for a checkbox. It also means the toggle costs no database round trip on any page that reads it.
const f2pCookie = "f2p"

// f2pFrom reports whether the toggle is set for this request.
func f2pFrom(r *http.Request) bool {
	c, err := r.Cookie(f2pCookie)
	return err == nil && c.Value == "1"
}

// vb is the views builder for one request, carrying that reader's toggle. Handlers must use this rather than s.views, which is shared by every reader at once and so can never hold a per-request setting.
func (s *Server) vb(r *http.Request) *views.Builder {
	return s.views.F2P(f2pFrom(r))
}

// postF2P writes or clears the toggle and returns to the page it was set from.
func (s *Server) postF2P(w http.ResponseWriter, r *http.Request) {
	c := &http.Cookie{
		Name: f2pCookie, Value: "1", Path: "/", MaxAge: cookieMaxAge,
		HttpOnly: true,
		Secure:   r.TLS != nil || strings.HasPrefix(s.cfg.BaseURL, "https://"),
		SameSite: http.SameSiteLaxMode,
	}
	// An unticked checkbox submits nothing at all — that absence is the only way "off" can arrive, which is why this is a POST of the whole control rather than a link that flips whatever is currently stored.
	if r.FormValue("f2p") != "1" {
		c.Value, c.MaxAge = "", -1
	}
	http.SetCookie(w, c)
	redirectBack(w, r, "/")
}

func (s *Server) home(ctx context.Context, r *http.Request) (*render.Doc, render.HTMLOptions, error) {
	d, err := s.vb(r).Home(ctx)
	return d, render.HTMLOptions{}, err
}

func (s *Server) about(ctx context.Context, r *http.Request) (*render.Doc, render.HTMLOptions, error) {
	d, err := s.views.About(ctx)
	return d, render.HTMLOptions{}, err
}

func (s *Server) status(ctx context.Context, r *http.Request) (*render.Doc, render.HTMLOptions, error) {
	why, paused := "", false
	free := int64(-1)
	if s.ing != nil {
		why, paused = s.ing.Paused()
		free = s.ing.FreeDiskMB()
	}
	d, err := s.views.Status(ctx, s.version, free, why, paused)
	return d, render.HTMLOptions{}, err
}

func (s *Server) search(ctx context.Context, r *http.Request) (*render.Doc, render.HTMLOptions, error) {
	q := r.URL.Query().Get("q")
	d, err := s.vb(r).Search(ctx, q)
	return d, render.HTMLOptions{}, err
}

// listOptionsFrom reads the shared filter/sort/page parameters.
func listOptionsFrom(r *http.Request, base store.ListOptions) store.ListOptions {
	o := base
	if v := r.URL.Query().Get("sort"); v != "" {
		o.Sort = v
	}
	switch r.URL.Query().Get("dir") {
	case "asc":
		o.Desc = false
	case "desc":
		o.Desc = true
	}
	if q := r.URL.Query().Get("q"); q != "" {
		o.Query = q
	}
	switch r.URL.Query().Get("members") {
	case "1", "yes":
		o.MembersOnly, o.F2POnly = true, false
	case "0", "no":
		o.MembersOnly, o.F2POnly = false, true
	}
	if v := qInt64(r, "minvol", -1); v >= 0 {
		o.MinVolume = v
	}
	if v := qInt64(r, "minprice", -1); v >= 0 {
		o.MinPrice = v
	}
	if v := qInt64(r, "maxprice", -1); v >= 0 {
		o.MaxPrice = v
	}
	o.Limit = clampInt(qInt(r, "limit", 100), 10, 500)
	o.Offset = max(0, qInt(r, "page", 1)-1) * o.Limit
	return o
}

func (s *Server) items(ctx context.Context, r *http.Request) (*render.Doc, render.HTMLOptions, error) {
	o := listOptionsFrom(r, store.ListOptions{Sort: "gpvol", Desc: true})
	d, total, err := s.vb(r).ListPage(ctx, "All items", "/items", o,
		"Every tradeable item the API reports. Click a column to sort.")
	if err != nil {
		return nil, render.HTMLOptions{}, err
	}
	s.addPaging(d, r, "/items", total, o)
	return d, render.HTMLOptions{SortBase: currentPath(r), CurrentSort: o.Sort, Desc: o.Desc}, nil
}

func (s *Server) finder(ctx context.Context, r *http.Request) (*render.Doc, render.HTMLOptions, error) {
	f, ok := views.FinderBySlug(r.PathValue("slug"))
	if !ok {
		return nil, render.HTMLOptions{}, store.ErrNotFound
	}
	o := listOptionsFrom(r, f.Options)
	d, total, err := s.vb(r).ListPage(ctx, f.Title, "/flips/"+f.Slug, o, f.Blurb)
	if err != nil {
		return nil, render.HTMLOptions{}, err
	}
	if f.Note != "" {
		d.Note("%s", f.Note)
	}
	s.addPaging(d, r, "/flips/"+f.Slug, total, o)
	return d, render.HTMLOptions{SortBase: currentPath(r), CurrentSort: o.Sort, Desc: o.Desc}, nil
}

// addPaging appends a page navigation block when there is more than one page.
func (s *Server) addPaging(d *render.Doc, r *http.Request, path string, total int, o store.ListOptions) {
	if o.Limit <= 0 || total <= o.Limit {
		return
	}
	page := o.Offset/o.Limit + 1
	pages := (total + o.Limit - 1) / o.Limit
	q := r.URL.Query()
	link := func(p int) string {
		q.Set("page", strconv.Itoa(p))
		return path + "?" + q.Encode()
	}
	var items []render.Link
	if page > 1 {
		items = append(items, render.Link{Text: "← previous", Href: link(page - 1)})
	}
	if page < pages {
		items = append(items, render.Link{Text: "next →", Href: link(page + 1)})
	}
	d.Add(render.Links{
		Title: fmt.Sprintf("Page %d of %d — %s items match", page, pages, render.GP(int64(total))),
		Items: items,
	})
}

func (s *Server) item(ctx context.Context, r *http.Request) (*render.Doc, render.HTMLOptions, error) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		return nil, render.HTMLOptions{}, store.ErrNotFound
	}
	w := r.URL.Query().Get("w")
	if w == "" {
		w = "1w"
	}
	d, err := s.views.ItemPage(ctx, id, w)
	if err != nil {
		return nil, render.HTMLOptions{}, err
	}
	// Star / unstar, reflecting whatever the caller's cookie already says.
	fav, label := "", "Add to favourites"
	if tok, ok := tokenFrom(r); ok && s.db.IsFavourite(ctx, tok.Hash(), id) {
		fav, label = "1", "Remove from favourites"
	}
	d.Add(render.Form{
		Action: "/favourites", Method: "post", Submit: label,
		Fields: []render.Field{
			{Name: "item", Kind: "hidden", Value: strconv.Itoa(id)},
			{Name: "remove", Kind: "hidden", Value: fav},
			{Name: "return", Kind: "hidden", Value: r.URL.RequestURI()},
		},
	})
	return d, render.HTMLOptions{}, nil
}

func (s *Server) alch(ctx context.Context, r *http.Request) (*render.Doc, render.HTMLOptions, error) {
	d, err := s.vb(r).AlchList(ctx, views.AlchOptions{
		Spell:      r.URL.Query().Get("spell"),
		ChargeFire: qBool(r, "firerunes"),
		Limit:      clampInt(qInt(r, "limit", 100), 10, 500),
	})
	return d, render.HTMLOptions{}, err
}

func (s *Server) storeProfit(ctx context.Context, r *http.Request) (*render.Doc, render.HTMLOptions, error) {
	d, err := s.vb(r).StoreProfit(ctx, clampInt(qInt(r, "limit", 100), 10, 500))
	return d, render.HTMLOptions{}, err
}

func (s *Server) taxCalc(ctx context.Context, r *http.Request) (*render.Doc, render.HTMLOptions, error) {
	d, err := s.views.TaxCalc(ctx,
		qInt64(r, "price", 0), qInt64(r, "qty", 1), qInt(r, "item", 0))
	return d, render.HTMLOptions{}, err
}

func (s *Server) calcIndex(ctx context.Context, r *http.Request) (*render.Doc, render.HTMLOptions, error) {
	d, err := s.vb(r).CalcIndex(ctx)
	return d, render.HTMLOptions{}, err
}

func (s *Server) calcKind(ctx context.Context, r *http.Request) (*render.Doc, render.HTMLOptions, error) {
	d, err := s.vb(r).CalcKind(ctx, r.PathValue("kind"))
	return d, render.HTMLOptions{}, err
}

func (s *Server) calcRecipe(ctx context.Context, r *http.Request) (*render.Doc, render.HTMLOptions, error) {
	d, err := s.views.CalcRecipe(ctx, r.PathValue("id"))
	return d, render.HTMLOptions{}, err
}

func (s *Server) indices(ctx context.Context, r *http.Request) (*render.Doc, render.HTMLOptions, error) {
	d, err := s.views.IndicesPage(ctx)
	return d, render.HTMLOptions{}, err
}

func (s *Server) indexPage(ctx context.Context, r *http.Request) (*render.Doc, render.HTMLOptions, error) {
	d, err := s.views.IndexPage(ctx, r.PathValue("id"))
	return d, render.HTMLOptions{}, err
}

// chartSVG serves a standalone SVG chart.
//
// Its real audience is Gemini: gemtext cannot embed an image, but a client will happily fetch one from a link, so the capsule gets the same chart the web site draws rather than only a sparkline.
func (s *Server) chartSVG(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSuffix(r.PathValue("id"), ".svg")
	id, err := strconv.Atoi(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	win := r.URL.Query().Get("w")
	if win == "" {
		win = "1w"
	}
	doc, err := s.views.ItemPage(r.Context(), id, win)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	for _, b := range doc.Blocks {
		c, ok := b.(render.Chart)
		if !ok {
			continue
		}
		w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=60")
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>`+"\n")
		// Standalone SVG carries no stylesheet, so the few rules the chart depends on are inlined here rather than left to a missing file.
		fmt.Fprint(w, strings.Replace(render.SVG(c, 900, 320), "<svg ",
			`<svg style="background:#1e1b16;color:#a1957a;font-family:monospace" `, 1))
		return
	}
	http.NotFound(w, r)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// currentPath returns the request path with its query preserved, so a sort link does not silently drop the active filters.
func currentPath(r *http.Request) string {
	q := r.URL.Query()
	q.Del("sort")
	q.Del("dir")
	q.Del("page")
	if len(q) == 0 {
		return r.URL.Path
	}
	return r.URL.Path + "?" + q.Encode()
}
