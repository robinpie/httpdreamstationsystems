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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"dreamstation.systems/openget/internal/calc"
	"dreamstation.systems/openget/internal/store"
)

// The public JSON API.
//
// We consume a free API run by volunteers, so we offer one back on the same terms: no key, no signup, and a User-Agent policy that mirrors theirs rather than inventing a stricter one. It is also what the finger and Gopher CGI helpers talk to, since those run under a sandbox that forbids touching the SQLite file directly.
func (s *Server) apiRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api", s.apiIndex)
	m.HandleFunc("GET /api/", s.apiIndex)
	m.HandleFunc("GET /api/item/{id}", s.apiItem)
	m.HandleFunc("GET /api/search", s.apiSearch)
	m.HandleFunc("GET /api/items", s.apiItems)
	m.HandleFunc("GET /api/timeseries/{id}", s.apiSeries)
	m.HandleFunc("GET /api/tax", s.apiTax)
	m.HandleFunc("GET /api/status", s.apiStatus)
}

func (s *Server) writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// Open CORS, exactly as upstream does for us. A price tracker that cannot be called from a browser tool is not much of a public API.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "public, max-age=60")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		s.log.Error("api encode", "err", err)
	}
}

func (s *Server) apiError(w http.ResponseWriter, code int, msg string) {
	s.writeJSON(w, code, map[string]string{"error": msg})
}

func (s *Server) apiIndex(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{
		"service":     "OpenGET",
		"version":     s.version,
		"source":      "OSRS Wiki real-time prices API, in partnership with RuneLite",
		"source_docs": "https://oldschool.runescape.wiki/w/RuneScape:Real-time_Prices",
		"policy": "Please set a descriptive User-Agent with contact details, and do not " +
			"sustain more than a few requests per second, which are the same terms I'm " +
			"given upstream.",
		"endpoints": map[string]string{
			"GET /api/items":           "list items; params: sort, dir, q, members, minvol, limit, page",
			"GET /api/item/{id}":       "one item with prices, margin, ROI and buy limit",
			"GET /api/search?q=":       "name search",
			"GET /api/timeseries/{id}": "price history; params: step (5m|1h|6h|24h), since, limit",
			"GET /api/tax?price=":      "GE tax for a price; optional item= to check the exempt list",
			"GET /api/status":          "archive size and ingestion health",
		},
	})
}

// apiItemJSON is the wire shape for an item.
type apiItemJSON struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Examine   string `json:"examine"`
	Members   bool   `json:"members"`
	BuyLimit  *int   `json:"buy_limit"`
	HighAlch  *int64 `json:"high_alch"`
	LowAlch   *int64 `json:"low_alch"`
	Value     *int64 `json:"value"`
	Icon      string `json:"icon"`
	Removed   bool   `json:"removed"`
	TaxExempt bool   `json:"tax_exempt"`

	InstantBuy  *int64   `json:"instant_buy"`  // API "high": what you pay now
	InstantSell *int64   `json:"instant_sell"` // API "low": what you get now
	BuyTime     *int64   `json:"instant_buy_time"`
	SellTime    *int64   `json:"instant_sell_time"`
	Tax         *int64   `json:"tax"`
	Margin      *int64   `json:"margin"`
	ROIPct      *float64 `json:"roi_pct"`
	Potential   *int64   `json:"potential_profit"`
	Volume24h   int64    `json:"volume_24h"`
	AlchProfit  *int64   `json:"alch_profit"`
	Change1h    *float64 `json:"change_1h"`
	Change24h   *float64 `json:"change_24h"`
	Change7d    *float64 `json:"change_7d"`
	Change30d   *float64 `json:"change_30d"`
}

func toAPIItem(it *store.Item) apiItemJSON {
	return apiItemJSON{
		ID: it.ID, Name: it.Name, Examine: it.Examine, Members: it.Members,
		BuyLimit: it.BuyLimit, HighAlch: it.HighAlch, LowAlch: it.LowAlch,
		Value: it.Value, Icon: it.Icon, Removed: it.Removed,
		TaxExempt:   calc.IsExempt(it.ID),
		InstantBuy:  it.High,
		InstantSell: it.Low,
		BuyTime:     it.HighTime,
		SellTime:    it.LowTime,
		Tax:         it.Tax,
		Margin:      it.Margin,
		ROIPct:      it.ROIPct,
		Potential:   it.Potential,
		Volume24h:   it.AvgVol24h,
		AlchProfit:  it.AlchProf,
		Change1h:    it.Change1h,
		Change24h:   it.Change24h,
		Change7d:    it.Change7d,
		Change30d:   it.Change30d,
	}
}

func (s *Server) apiItem(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		s.apiError(w, http.StatusBadRequest, "item id must be a number")
		return
	}
	it, err := s.db.GetItem(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		s.apiError(w, http.StatusNotFound, "no such item")
		return
	}
	if err != nil {
		s.log.Error("api item", "err", err)
		s.apiError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	s.writeJSON(w, http.StatusOK, toAPIItem(it))
}

func (s *Server) apiSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		s.apiError(w, http.StatusBadRequest, "q is required")
		return
	}
	// The API takes its filters from its parameters and nowhere else — a cookie quietly reshaping a JSON response would be a nasty surprise for anyone scripting against it.
	items, err := s.db.SearchItems(r.Context(), q, clampInt(qInt(r, "limit", 25), 1, 200), false)
	if err != nil {
		s.log.Error("api search", "err", err)
		s.apiError(w, http.StatusInternalServerError, "search failed")
		return
	}
	out := make([]apiItemJSON, 0, len(items))
	for _, it := range items {
		out = append(out, toAPIItem(it))
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"query": q, "count": len(out), "items": out})
}

func (s *Server) apiItems(w http.ResponseWriter, r *http.Request) {
	o := listOptionsFrom(r, store.ListOptions{Sort: "gpvol", Desc: true})
	items, total, err := s.db.ListItems(r.Context(), o)
	if err != nil {
		s.log.Error("api items", "err", err)
		s.apiError(w, http.StatusInternalServerError, "list failed")
		return
	}
	out := make([]apiItemJSON, 0, len(items))
	for _, it := range items {
		out = append(out, toAPIItem(it))
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"total": total, "count": len(out), "sort": o.Sort, "desc": o.Desc, "items": out,
	})
}

func (s *Server) apiSeries(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		s.apiError(w, http.StatusBadRequest, "item id must be a number")
		return
	}
	step := r.URL.Query().Get("step")
	if step == "" {
		step = "1h"
	}
	switch step {
	case "5m", "1h", "6h", "24h":
	default:
		s.apiError(w, http.StatusBadRequest, "step must be one of 5m, 1h, 6h, 24h")
		return
	}
	since := qInt64(r, "since", 0)
	pts, err := s.db.Series(r.Context(), id, step, since, clampInt(qInt(r, "limit", 2000), 1, 20000))
	if err != nil {
		s.log.Error("api series", "err", err)
		s.apiError(w, http.StatusInternalServerError, "series failed")
		return
	}
	type pt struct {
		TS      int64  `json:"timestamp"`
		High    *int64 `json:"avg_instant_buy"`
		Low     *int64 `json:"avg_instant_sell"`
		HighVol int64  `json:"instant_buy_volume"`
		LowVol  int64  `json:"instant_sell_volume"`
	}
	out := make([]pt, 0, len(pts))
	for _, p := range pts {
		out = append(out, pt{p.TS, p.High, p.Low, p.HighVol, p.LowVol})
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"item_id": id, "step": step, "count": len(out), "points": out,
	})
}

func (s *Server) apiTax(w http.ResponseWriter, r *http.Request) {
	price := qInt64(r, "price", -1)
	if price < 0 {
		s.apiError(w, http.StatusBadRequest, "price is required")
		return
	}
	item := qInt(r, "item", 0)
	qty := qInt64(r, "qty", 1)
	if qty < 1 {
		qty = 1
	}
	t := calc.Tax(item, price)
	s.writeJSON(w, http.StatusOK, map[string]any{
		"item_id":        item,
		"exempt":         item > 0 && calc.IsExempt(item),
		"price":          price,
		"quantity":       qty,
		"tax_per_item":   t,
		"net_per_item":   price - t,
		"total_tax":      t * qty,
		"total_received": (price - t) * qty,
		"rules": fmt.Sprintf("2%% of the sale price, paid by the seller, rounded down, capped at %d gp per item.",
			calc.TaxCap),
	})
}

func (s *Server) apiStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	counts, err := s.db.TableCounts(ctx)
	if err != nil {
		s.apiError(w, http.StatusInternalServerError, "status failed")
		return
	}
	arch := map[string]any{}
	for _, step := range []string{"5m", "1h", "24h"} {
		oldest, newest, rows, err := s.db.ArchiveSpan(ctx, step)
		if err != nil {
			continue
		}
		e := map[string]any{"rows": rows}
		if !oldest.IsZero() {
			e["oldest"] = oldest.UTC().Format(time.RFC3339)
			e["newest"] = newest.UTC().Format(time.RFC3339)
		}
		arch[step] = e
	}
	out := map[string]any{
		"version":  s.version,
		"uptime":   time.Since(s.started).Round(time.Second).String(),
		"tables":   counts,
		"archive":  arch,
		"db_bytes": s.db.SizeOnDisk(),
	}
	if s.ing != nil {
		why, paused := s.ing.Paused()
		out["ingestion_paused"] = paused
		if paused {
			out["ingestion_pause_reason"] = why
		}
		out["free_disk_mb"] = s.ing.FreeDiskMB()
	}
	if t, err := s.db.LatestFetch(ctx); err == nil && !t.IsZero() {
		out["prices_updated"] = t.UTC().Format(time.RFC3339)
	}
	s.writeJSON(w, http.StatusOK, out)
}
