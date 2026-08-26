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

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Limits bounds what one token may store, so the scheme cannot be abused as free hosting.
type Limits struct {
	Favourites int
	LedgerRows int
	Alerts     int
}

// ErrLimit is returned when a per-token cap would be exceeded.
var ErrLimit = errors.New("store: per-token limit reached")

// TouchToken creates the token row if absent and slides last_seen forward.
//
// Called only on a WRITE — a first favourite, a first ledger row, a pref change — never on a page view. That distinction is what keeps the table honest: crawlers and casual readers never create rows.
func (d *DB) TouchToken(ctx context.Context, hash string) error {
	now := time.Now().Unix()
	_, err := d.w.ExecContext(ctx, `
		INSERT INTO tokens (token_hash, created, last_seen) VALUES (?,?,?)
		ON CONFLICT(token_hash) DO UPDATE SET last_seen = excluded.last_seen`,
		hash, now, now)
	return err
}

// TokenExists reports whether a token has ever written anything.
func (d *DB) TokenExists(ctx context.Context, hash string) (bool, error) {
	var n int
	err := d.r.QueryRowContext(ctx, `SELECT count(*) FROM tokens WHERE token_hash = ?`, hash).Scan(&n)
	return n > 0, err
}

// PruneTokens deletes tokens untouched since cutoff, and everything they own.
//
// Announced on the site rather than done quietly: data that disappears after a year of disuse is a privacy feature, not merely housekeeping.
func (d *DB) PruneTokens(ctx context.Context, cutoff int64) (int64, error) {
	// Every child table declares ON DELETE CASCADE and foreign_keys is on, so one delete is enough.
	res, err := d.w.ExecContext(ctx, `DELETE FROM tokens WHERE last_seen < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ---------------------------------------------------------------------------
// Favourites
// ---------------------------------------------------------------------------

// AddFavourite records a favourite, enforcing the per-token cap.
func (d *DB) AddFavourite(ctx context.Context, hash string, itemID, cap int) error {
	if err := d.TouchToken(ctx, hash); err != nil {
		return err
	}
	var n int
	if err := d.r.QueryRowContext(ctx,
		`SELECT count(*) FROM favourites WHERE token_hash = ?`, hash).Scan(&n); err != nil {
		return err
	}
	if cap > 0 && n >= cap {
		return fmt.Errorf("%w: %d favourites", ErrLimit, cap)
	}
	_, err := d.w.ExecContext(ctx,
		`INSERT INTO favourites (token_hash,item_id,added) VALUES (?,?,?)
		 ON CONFLICT(token_hash,item_id) DO NOTHING`, hash, itemID, time.Now().Unix())
	return err
}

// RemoveFavourite drops a favourite.
func (d *DB) RemoveFavourite(ctx context.Context, hash string, itemID int) error {
	_, err := d.w.ExecContext(ctx,
		`DELETE FROM favourites WHERE token_hash = ? AND item_id = ?`, hash, itemID)
	return err
}

// IsFavourite reports whether an item is favourited.
func (d *DB) IsFavourite(ctx context.Context, hash string, itemID int) bool {
	var n int
	_ = d.r.QueryRowContext(ctx,
		`SELECT count(*) FROM favourites WHERE token_hash = ? AND item_id = ?`, hash, itemID).Scan(&n)
	return n > 0
}

// ---------------------------------------------------------------------------
// Preferences
// ---------------------------------------------------------------------------

// Prefs is one token's settings.
type Prefs struct {
	Theme       string
	DefaultSort string
	Columns     string
	TZ          string
	RowsPerPage int
	// MembersOnly is stored but never applied: the members split is driven by the site-wide free-to-play cookie instead, which works without a restore code. Kept so the column does not need a migration to drop.
	MembersOnly bool
	MinVolume   int64
}

// DefaultPrefs are used for a reader with no token.
func DefaultPrefs() Prefs {
	return Prefs{Theme: "auto", DefaultSort: "margin", TZ: "UTC", RowsPerPage: 50}
}

// GetPrefs loads preferences, returning the defaults when none are stored.
func (d *DB) GetPrefs(ctx context.Context, hash string) (Prefs, error) {
	p := DefaultPrefs()
	var members int
	err := d.r.QueryRowContext(ctx, `
		SELECT theme, default_sort, columns, tz, rows_per_page, members_only, min_volume
		  FROM prefs WHERE token_hash = ?`, hash).
		Scan(&p.Theme, &p.DefaultSort, &p.Columns, &p.TZ, &p.RowsPerPage, &members, &p.MinVolume)
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	p.MembersOnly = members != 0
	return p, err
}

// SetPrefs stores preferences.
func (d *DB) SetPrefs(ctx context.Context, hash string, p Prefs) error {
	if err := d.TouchToken(ctx, hash); err != nil {
		return err
	}
	members := 0
	if p.MembersOnly {
		members = 1
	}
	_, err := d.w.ExecContext(ctx, `
		INSERT INTO prefs (token_hash,theme,default_sort,columns,tz,rows_per_page,members_only,min_volume)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(token_hash) DO UPDATE SET
			theme=excluded.theme, default_sort=excluded.default_sort,
			columns=excluded.columns, tz=excluded.tz,
			rows_per_page=excluded.rows_per_page,
			members_only=excluded.members_only, min_volume=excluded.min_volume`,
		hash, p.Theme, p.DefaultSort, p.Columns, p.TZ, p.RowsPerPage, members, p.MinVolume)
	return err
}

// ---------------------------------------------------------------------------
// Profit tracker
// ---------------------------------------------------------------------------

// LedgerEntry is one recorded trade.
type LedgerEntry struct {
	ID        int64
	ItemID    int
	ItemName  string
	Side      string // "buy" or "sell"
	Qty       int64
	UnitPrice int64
	TaxPaid   int64
	TS        time.Time
	Note      string
}

// Total is the coin flow of this entry: negative for a buy, positive for the net proceeds of a sale.
func (e LedgerEntry) Total() int64 {
	gross := e.Qty * e.UnitPrice
	if e.Side == "buy" {
		return -gross
	}
	return gross - e.TaxPaid
}

// AddLedger records a trade, enforcing the per-token row cap.
func (d *DB) AddLedger(ctx context.Context, hash string, e LedgerEntry, cap int) error {
	if e.Side != "buy" && e.Side != "sell" {
		return fmt.Errorf("store: side must be buy or sell, got %q", e.Side)
	}
	if err := d.TouchToken(ctx, hash); err != nil {
		return err
	}
	var n int
	if err := d.r.QueryRowContext(ctx,
		`SELECT count(*) FROM ledger WHERE token_hash = ?`, hash).Scan(&n); err != nil {
		return err
	}
	if cap > 0 && n >= cap {
		return fmt.Errorf("%w: %d ledger rows", ErrLimit, cap)
	}
	ts := e.TS
	if ts.IsZero() {
		ts = time.Now()
	}
	_, err := d.w.ExecContext(ctx, `
		INSERT INTO ledger (token_hash,item_id,side,qty,unit_price,tax_paid,ts,note)
		VALUES (?,?,?,?,?,?,?,?)`,
		hash, e.ItemID, e.Side, e.Qty, e.UnitPrice, e.TaxPaid, ts.Unix(), e.Note)
	return err
}

// Ledger lists a token's trades, newest first.
func (d *DB) Ledger(ctx context.Context, hash string, limit int) ([]LedgerEntry, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := d.r.QueryContext(ctx, `
		SELECT l.id, l.item_id, COALESCE(i.name,''), l.side, l.qty, l.unit_price,
		       l.tax_paid, l.ts, l.note
		  FROM ledger l LEFT JOIN items i ON i.id = l.item_id
		 WHERE l.token_hash = ? ORDER BY l.ts DESC, l.id DESC LIMIT ?`, hash, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LedgerEntry
	for rows.Next() {
		var e LedgerEntry
		var ts int64
		if err := rows.Scan(&e.ID, &e.ItemID, &e.ItemName, &e.Side, &e.Qty,
			&e.UnitPrice, &e.TaxPaid, &ts, &e.Note); err != nil {
			return nil, err
		}
		e.TS = time.Unix(ts, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}

// DeleteLedger removes one row.
func (d *DB) DeleteLedger(ctx context.Context, hash string, id int64) error {
	_, err := d.w.ExecContext(ctx,
		`DELETE FROM ledger WHERE token_hash = ? AND id = ?`, hash, id)
	return err
}

// ClearLedger removes every trade a token has recorded, leaving its favourites, preferences, alerts and the token itself alone. Distinct from deleting the token outright: starting a fresh ledger is not the same wish as wanting to be forgotten, and a botched CSV import should not cost someone their restore code.
func (d *DB) ClearLedger(ctx context.Context, hash string) error {
	_, err := d.w.ExecContext(ctx, `DELETE FROM ledger WHERE token_hash = ?`, hash)
	return err
}

// ProfitStats summarises a token's trading.
type ProfitStats struct {
	Trades     int
	Bought     int64 // gp spent
	Sold       int64 // gp received, net of tax
	TaxPaid    int64
	Realised   int64 // Sold - Bought
	Items      int
	FirstTrade time.Time
}

// Profit computes the summary.
//
// Realised profit is simply money in minus money out. Deliberately not a FIFO/average-cost inventory model: that needs a complete trade history to mean anything, and a hand-kept ledger never is complete. A number people can reproduce with a calculator beats a more sophisticated one they cannot.
func (d *DB) Profit(ctx context.Context, hash string) (ProfitStats, error) {
	var s ProfitStats
	var first sql.NullInt64
	err := d.r.QueryRowContext(ctx, `
		SELECT
			count(*),
			COALESCE(sum(CASE WHEN side='buy'  THEN qty*unit_price ELSE 0 END), 0),
			COALESCE(sum(CASE WHEN side='sell' THEN qty*unit_price - tax_paid ELSE 0 END), 0),
			COALESCE(sum(tax_paid), 0),
			count(DISTINCT item_id),
			min(ts)
		  FROM ledger WHERE token_hash = ?`, hash).
		Scan(&s.Trades, &s.Bought, &s.Sold, &s.TaxPaid, &s.Items, &first)
	if err != nil {
		return s, err
	}
	s.Realised = s.Sold - s.Bought
	if first.Valid {
		s.FirstTrade = time.Unix(first.Int64, 0)
	}
	return s, nil
}

// ---------------------------------------------------------------------------
// Alerts
// ---------------------------------------------------------------------------

// Alert is one price watch.
type Alert struct {
	ID        int64
	ItemID    int
	ItemName  string
	Condition string
	Threshold float64
	Active    bool
	Created   time.Time
	LastFired time.Time
}

// AlertConditions are the supported comparisons.
var AlertConditions = []struct{ Key, Label string }{
	{"high_below", "instant-buy price falls below"},
	{"high_above", "instant-buy price rises above"},
	{"low_below", "instant-sell price falls below"},
	{"low_above", "instant-sell price rises above"},
	{"margin_above", "margin rises above"},
	{"roi_above", "ROI rises above (%)"},
}

func validCondition(c string) bool {
	for _, a := range AlertConditions {
		if a.Key == c {
			return true
		}
	}
	return false
}

// AddAlert creates an alert, enforcing the per-token cap.
func (d *DB) AddAlert(ctx context.Context, hash string, itemID int, cond string, threshold float64, cap int) error {
	if !validCondition(cond) {
		return fmt.Errorf("store: unknown alert condition %q", cond)
	}
	if err := d.TouchToken(ctx, hash); err != nil {
		return err
	}
	var n int
	if err := d.r.QueryRowContext(ctx,
		`SELECT count(*) FROM alerts WHERE token_hash = ?`, hash).Scan(&n); err != nil {
		return err
	}
	if cap > 0 && n >= cap {
		return fmt.Errorf("%w: %d alerts", ErrLimit, cap)
	}
	_, err := d.w.ExecContext(ctx, `
		INSERT INTO alerts (token_hash,item_id,condition,threshold,active,created)
		VALUES (?,?,?,?,1,?)`, hash, itemID, cond, threshold, time.Now().Unix())
	return err
}

// Alerts lists a token's alerts.
func (d *DB) Alerts(ctx context.Context, hash string) ([]Alert, error) {
	rows, err := d.r.QueryContext(ctx, `
		SELECT a.id, a.item_id, COALESCE(i.name,''), a.condition, a.threshold,
		       a.active, a.created, a.last_fired
		  FROM alerts a LEFT JOIN items i ON i.id = a.item_id
		 WHERE a.token_hash = ? ORDER BY a.id`, hash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Alert
	for rows.Next() {
		var a Alert
		var active int
		var created int64
		var fired sql.NullInt64
		if err := rows.Scan(&a.ID, &a.ItemID, &a.ItemName, &a.Condition,
			&a.Threshold, &active, &created, &fired); err != nil {
			return nil, err
		}
		a.Active = active != 0
		a.Created = time.Unix(created, 0)
		if fired.Valid {
			a.LastFired = time.Unix(fired.Int64, 0)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DeleteAlert removes one alert.
func (d *DB) DeleteAlert(ctx context.Context, hash string, id int64) error {
	_, err := d.w.ExecContext(ctx, `DELETE FROM alerts WHERE token_hash = ? AND id = ?`, hash, id)
	return err
}

// AlertEvent is one firing.
type AlertEvent struct {
	ID      int64
	AlertID int64
	ItemID  int
	FiredAt time.Time
	Message string
}

// EvaluateAlerts checks every active alert against current stats and appends events for those that now hold.
//
// Alerts are PULL-based: firing appends a row, and the user reads it from a private Atom feed or the dashboard. Cloning ge-tracker's email and SMS alerts would drag in delivery infrastructure, bounce handling, spam complaints and — for SMS — an actual bill, to deliver the same information any feed reader already collects.
//
// An alert re-fires only after its condition has stopped holding, so a price sitting under a threshold produces one event rather than one every minute.
func (d *DB) EvaluateAlerts(ctx context.Context, now time.Time) (int, error) {
	rows, err := d.r.QueryContext(ctx, `
		SELECT a.id, a.token_hash, a.item_id, a.condition, a.threshold, a.last_fired,
		       i.name, s.high, s.low, s.margin, s.roi_pct
		  FROM alerts a
		  JOIN items i      ON i.id = a.item_id
		  LEFT JOIN item_stats s ON s.item_id = a.item_id
		 WHERE a.active = 1`)
	if err != nil {
		return 0, err
	}
	type pending struct {
		alertID int64
		hash    string
		itemID  int
		msg     string
		hold    bool
	}
	var todo []pending
	for rows.Next() {
		var (
			id                int64
			hash              string
			itemID            int
			cond              string
			threshold         float64
			lastFired         sql.NullInt64
			name              string
			high, low, margin sql.NullInt64
			roi               sql.NullFloat64
		)
		if err := rows.Scan(&id, &hash, &itemID, &cond, &threshold, &lastFired,
			&name, &high, &low, &margin, &roi); err != nil {
			rows.Close()
			return 0, err
		}
		var holds bool
		var msg string
		switch cond {
		case "high_below":
			holds = high.Valid && float64(high.Int64) < threshold
			msg = fmt.Sprintf("%s instant-buy is %d, below %.0f", name, high.Int64, threshold)
		case "high_above":
			holds = high.Valid && float64(high.Int64) > threshold
			msg = fmt.Sprintf("%s instant-buy is %d, above %.0f", name, high.Int64, threshold)
		case "low_below":
			holds = low.Valid && float64(low.Int64) < threshold
			msg = fmt.Sprintf("%s instant-sell is %d, below %.0f", name, low.Int64, threshold)
		case "low_above":
			holds = low.Valid && float64(low.Int64) > threshold
			msg = fmt.Sprintf("%s instant-sell is %d, above %.0f", name, low.Int64, threshold)
		case "margin_above":
			holds = margin.Valid && float64(margin.Int64) > threshold
			msg = fmt.Sprintf("%s margin is %d, above %.0f", name, margin.Int64, threshold)
		case "roi_above":
			holds = roi.Valid && roi.Float64 > threshold
			msg = fmt.Sprintf("%s ROI is %.2f%%, above %.2f%%", name, roi.Float64, threshold)
		}
		todo = append(todo, pending{id, hash, itemID, msg, holds})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	fired := 0
	for _, p := range todo {
		// armed tracks whether the condition was already holding at the last evaluation; last_fired is cleared when it stops, which is what makes this edge-triggered rather than level-triggered.
		var last sql.NullInt64
		if err := d.r.QueryRowContext(ctx,
			`SELECT last_fired FROM alerts WHERE id = ?`, p.alertID).Scan(&last); err != nil {
			continue
		}
		switch {
		case p.hold && !last.Valid:
			if _, err := d.w.ExecContext(ctx, `
				INSERT INTO alert_events (token_hash,alert_id,item_id,fired_at,message)
				VALUES (?,?,?,?,?)`, p.hash, p.alertID, p.itemID, now.Unix(), p.msg); err != nil {
				return fired, err
			}
			if _, err := d.w.ExecContext(ctx,
				`UPDATE alerts SET last_fired = ? WHERE id = ?`, now.Unix(), p.alertID); err != nil {
				return fired, err
			}
			fired++
		case !p.hold && last.Valid:
			if _, err := d.w.ExecContext(ctx,
				`UPDATE alerts SET last_fired = NULL WHERE id = ?`, p.alertID); err != nil {
				return fired, err
			}
		}
	}
	return fired, nil
}

// AlertEvents lists recent firings for a token.
func (d *DB) AlertEvents(ctx context.Context, hash string, limit int) ([]AlertEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := d.r.QueryContext(ctx, `
		SELECT id, alert_id, item_id, fired_at, message
		  FROM alert_events WHERE token_hash = ? ORDER BY fired_at DESC, id DESC LIMIT ?`,
		hash, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlertEvent
	for rows.Next() {
		var e AlertEvent
		var ts int64
		if err := rows.Scan(&e.ID, &e.AlertID, &e.ItemID, &ts, &e.Message); err != nil {
			return nil, err
		}
		e.FiredAt = time.Unix(ts, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Gemini client certificates
// ---------------------------------------------------------------------------

// BindCert links a Gemini client certificate to a token.
func (d *DB) BindCert(ctx context.Context, certHash, tokenHash string) error {
	if err := d.TouchToken(ctx, tokenHash); err != nil {
		return err
	}
	_, err := d.w.ExecContext(ctx, `
		INSERT INTO gemini_certs (cert_hash,token_hash,bound_at) VALUES (?,?,?)
		ON CONFLICT(cert_hash) DO UPDATE SET token_hash = excluded.token_hash`,
		certHash, tokenHash, time.Now().Unix())
	return err
}

// TokenForCert resolves a certificate to its bound token hash.
func (d *DB) TokenForCert(ctx context.Context, certHash string) (string, error) {
	var h string
	err := d.r.QueryRowContext(ctx,
		`SELECT token_hash FROM gemini_certs WHERE cert_hash = ?`, certHash).Scan(&h)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return h, err
}
