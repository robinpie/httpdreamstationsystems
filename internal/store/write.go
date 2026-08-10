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
	"fmt"
	"time"

	"dreamstation.systems/openget/internal/wiki"
)

// UpsertItems merges a /mapping response into the items table.
//
// Rows are never deleted. An item that vanishes from /mapping keeps its row and its whole price history, marked removed=1 — the archive is the asset, and a delisted item's history is exactly the part upstream can no longer serve at all.
func (d *DB) UpsertItems(ctx context.Context, items []wiki.Item, now time.Time) (int, error) {
	ts := now.Unix()
	n := 0
	err := d.Tx(ctx, func(tx *sql.Tx) error {
		st, err := tx.PrepareContext(ctx, `
			INSERT INTO items (id,name,examine,members,lowalch,highalch,buy_limit,value,icon,first_seen,last_seen,removed)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,0)
			ON CONFLICT(id) DO UPDATE SET
				name=excluded.name, examine=excluded.examine, members=excluded.members,
				lowalch=excluded.lowalch, highalch=excluded.highalch,
				buy_limit=excluded.buy_limit, value=excluded.value, icon=excluded.icon,
				last_seen=excluded.last_seen, removed=0`)
		if err != nil {
			return err
		}
		defer st.Close()
		for _, it := range items {
			members := 0
			if it.Members {
				members = 1
			}
			if _, err := st.ExecContext(ctx, it.ID, it.Name, it.Examine, members,
				nullInt(it.LowAlch), nullInt(it.HighAlch), nullInt(it.Limit), nullInt(it.Value),
				it.Icon, ts, ts); err != nil {
				return fmt.Errorf("upsert item %d (%s): %w", it.ID, it.Name, err)
			}
			n++
		}
		// Anything not in this mapping is delisted. Flag, never delete.
		_, err = tx.ExecContext(ctx, `UPDATE items SET removed = 1 WHERE last_seen < ? AND removed = 0`, ts)
		return err
	})
	return n, err
}

// UpsertLatest overwrites the hot latest table.
func (d *DB) UpsertLatest(ctx context.Context, data map[int]wiki.Latest, now time.Time) (int, error) {
	ts := now.Unix()
	n := 0
	err := d.Tx(ctx, func(tx *sql.Tx) error {
		st, err := tx.PrepareContext(ctx, `
			INSERT INTO latest (item_id,high,high_time,low,low_time,fetched_at)
			VALUES (?,?,?,?,?,?)
			ON CONFLICT(item_id) DO UPDATE SET
				high=excluded.high, high_time=excluded.high_time,
				low=excluded.low, low_time=excluded.low_time,
				fetched_at=excluded.fetched_at`)
		if err != nil {
			return err
		}
		defer st.Close()
		known, err := knownIDs(ctx, tx)
		if err != nil {
			return err
		}
		for id, l := range data {
			// /latest occasionally carries ids /mapping has not listed yet (new items land in one before the other). Skip rather than violate the foreign key; the daily mapping poll picks them up.
			if !known[id] {
				continue
			}
			if _, err := st.ExecContext(ctx, id,
				nullInt64(l.High), nullInt64(l.HighTime),
				nullInt64(l.Low), nullInt64(l.LowTime), ts); err != nil {
				return fmt.Errorf("upsert latest %d: %w", id, err)
			}
			n++
		}
		return nil
	})
	return n, err
}

// UpsertBucket writes a whole bucket-endpoint response into the price tier for step. Existing rows are replaced, so re-fetching a bucket is idempotent and a gap can always be re-healed by refetching.
func (d *DB) UpsertBucket(ctx context.Context, step string, b *wiki.Bucket) (int, error) {
	table, err := tableFor(step)
	if err != nil {
		return 0, err
	}
	n := 0
	err = d.Tx(ctx, func(tx *sql.Tx) error {
		st, err := tx.PrepareContext(ctx, `
			INSERT INTO `+table+` (item_id,bucket_ts,avg_high,high_vol,avg_low,low_vol)
			VALUES (?,?,?,?,?,?)
			ON CONFLICT(item_id,bucket_ts) DO UPDATE SET
				avg_high=excluded.avg_high, high_vol=excluded.high_vol,
				avg_low=excluded.avg_low,  low_vol=excluded.low_vol`)
		if err != nil {
			return err
		}
		defer st.Close()
		known, err := knownIDs(ctx, tx)
		if err != nil {
			return err
		}
		for k, a := range b.Data {
			id := 0
			if _, err := fmt.Sscan(k, &id); err != nil {
				continue
			}
			if !known[id] {
				continue
			}
			// A bucket with neither price is pure noise — skip it rather than spend a row saying "nothing traded", which is already implied by the absence of a row.
			if a.AvgHighPrice == nil && a.AvgLowPrice == nil {
				continue
			}
			if _, err := st.ExecContext(ctx, id, b.Timestamp,
				nullInt64(a.AvgHighPrice), a.HighPriceVolume,
				nullInt64(a.AvgLowPrice), a.LowPriceVolume); err != nil {
				return fmt.Errorf("upsert %s %d@%d: %w", table, id, b.Timestamp, err)
			}
			n++
		}
		return nil
	})
	return n, err
}

// UpsertTimeseries writes the points of a per-item /timeseries call into the tier for step. Used only by the one-time historical backfill.
func (d *DB) UpsertTimeseries(ctx context.Context, step string, itemID int, pts []wiki.TimePoint) (int, error) {
	table, err := tableFor(step)
	if err != nil {
		return 0, err
	}
	n := 0
	err = d.Tx(ctx, func(tx *sql.Tx) error {
		st, err := tx.PrepareContext(ctx, `
			INSERT INTO `+table+` (item_id,bucket_ts,avg_high,high_vol,avg_low,low_vol)
			VALUES (?,?,?,?,?,?)
			ON CONFLICT(item_id,bucket_ts) DO NOTHING`)
		if err != nil {
			return err
		}
		defer st.Close()
		for _, p := range pts {
			if p.AvgHighPrice == nil && p.AvgLowPrice == nil {
				continue
			}
			res, err := st.ExecContext(ctx, itemID, p.Timestamp,
				nullInt64(p.AvgHighPrice), p.HighPriceVolume,
				nullInt64(p.AvgLowPrice), p.LowPriceVolume)
			if err != nil {
				return fmt.Errorf("backfill %s %d@%d: %w", table, itemID, p.Timestamp, err)
			}
			if a, _ := res.RowsAffected(); a > 0 {
				n++
			}
		}
		return nil
	})
	return n, err
}

// UpsertVolumes records a /volumes snapshot.
func (d *DB) UpsertVolumes(ctx context.Context, ts int64, data map[int]int64) (int, error) {
	n := 0
	err := d.Tx(ctx, func(tx *sql.Tx) error {
		st, err := tx.PrepareContext(ctx, `
			INSERT INTO volumes (item_id,ts,volume) VALUES (?,?,?)
			ON CONFLICT(item_id,ts) DO UPDATE SET volume=excluded.volume`)
		if err != nil {
			return err
		}
		defer st.Close()
		known, err := knownIDs(ctx, tx)
		if err != nil {
			return err
		}
		for id, v := range data {
			if !known[id] {
				continue
			}
			if _, err := st.ExecContext(ctx, id, ts, v); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

// knownIDs loads the item id set so bulk upserts can skip ids that /mapping has not caught up with, instead of failing the whole transaction on a foreign-key violation.
func knownIDs(ctx context.Context, tx *sql.Tx) (map[int]bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM items`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int]bool, 5000)
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Rollups
// ---------------------------------------------------------------------------

// Rollup aggregates a finer price tier into a coarser one for buckets since `since`, filling any gap the bulk endpoints left behind.
//
// Both tiers are also populated directly from their own bulk endpoints, so this is belt-and-braces: if a /1h poll is missed (restart, upstream blip), the next nightly rollup reconstructs the bucket from the 5m rows we did capture. Prices are volume-weighted, because a plain mean of bucket averages would treat a bucket with one trade as equal to one with 10,000.
func (d *DB) Rollup(ctx context.Context, fromStep, toStep string, since int64) (int64, error) {
	src, err := tableFor(fromStep)
	if err != nil {
		return 0, err
	}
	dst, err := tableFor(toStep)
	if err != nil {
		return 0, err
	}
	var width int64
	switch toStep {
	case "1h":
		width = 3600
	case "24h":
		width = 86400
	default:
		return 0, fmt.Errorf("store: cannot roll up into %q", toStep)
	}

	// COALESCE(sum,0)=0 guards a divide-by-zero when every contributing bucket recorded a price but no volume; in that case fall back to a plain mean.
	q := fmt.Sprintf(`
		INSERT INTO %s (item_id,bucket_ts,avg_high,high_vol,avg_low,low_vol)
		SELECT item_id,
		       (bucket_ts / %d) * %d AS b,
		       CASE WHEN sum(CASE WHEN avg_high IS NOT NULL THEN high_vol ELSE 0 END) > 0
		            THEN sum(CASE WHEN avg_high IS NOT NULL THEN avg_high * high_vol ELSE 0 END)
		                 / sum(CASE WHEN avg_high IS NOT NULL THEN high_vol ELSE 0 END)
		            ELSE avg(avg_high) END,
		       sum(high_vol),
		       CASE WHEN sum(CASE WHEN avg_low IS NOT NULL THEN low_vol ELSE 0 END) > 0
		            THEN sum(CASE WHEN avg_low IS NOT NULL THEN avg_low * low_vol ELSE 0 END)
		                 / sum(CASE WHEN avg_low IS NOT NULL THEN low_vol ELSE 0 END)
		            ELSE avg(avg_low) END,
		       sum(low_vol)
		  FROM %s
		 WHERE bucket_ts >= ?
		 GROUP BY item_id, b
		 HAVING count(*) > 0
		ON CONFLICT(item_id,bucket_ts) DO NOTHING`, dst, width, width, src)

	res, err := d.w.ExecContext(ctx, q, since)
	if err != nil {
		return 0, fmt.Errorf("store: rollup %s->%s: %w", fromStep, toStep, err)
	}
	return res.RowsAffected()
}

// Prune deletes price rows older than cutoff from one tier. A cutoff of zero means "keep forever" and does nothing.
func (d *DB) Prune(ctx context.Context, step string, cutoff int64) (int64, error) {
	if cutoff <= 0 {
		return 0, nil
	}
	table, err := tableFor(step)
	if err != nil {
		return 0, err
	}
	// Chunked so a first prune after a long outage cannot hold the write lock for minutes at a stretch — the 5m pollers must keep making progress.
	var total int64
	for {
		res, err := d.w.ExecContext(ctx, fmt.Sprintf(`
			DELETE FROM %s WHERE rowid IN (
				SELECT rowid FROM %s WHERE bucket_ts < ? LIMIT 20000
			)`, table, table), cutoff)
		if err != nil {
			// WITHOUT ROWID tables have no rowid; fall back to a plain delete.
			res, err = d.w.ExecContext(ctx,
				fmt.Sprintf(`DELETE FROM %s WHERE bucket_ts < ?`, table), cutoff)
			if err != nil {
				return total, err
			}
			n, _ := res.RowsAffected()
			return total + n, nil
		}
		n, _ := res.RowsAffected()
		total += n
		if n == 0 {
			return total, nil
		}
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// PruneVolumes drops volume snapshots older than cutoff.
func (d *DB) PruneVolumes(ctx context.Context, cutoff int64) (int64, error) {
	if cutoff <= 0 {
		return 0, nil
	}
	res, err := d.w.ExecContext(ctx, `DELETE FROM volumes WHERE ts < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ---------------------------------------------------------------------------
// Run bookkeeping
// ---------------------------------------------------------------------------

// StartRun records the beginning of a job and returns its id.
func (d *DB) StartRun(ctx context.Context, job string) (int64, error) {
	res, err := d.w.ExecContext(ctx,
		`INSERT INTO poll_runs (job,started_at) VALUES (?,?)`, job, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FinishRun closes out a job record.
func (d *DB) FinishRun(ctx context.Context, id int64, ok bool, rows int, note string) error {
	okv := 0
	if ok {
		okv = 1
	}
	if len(note) > 500 {
		note = note[:500]
	}
	_, err := d.w.ExecContext(ctx,
		`UPDATE poll_runs SET ended_at=?, ok=?, rows=?, note=? WHERE id=?`,
		time.Now().Unix(), okv, rows, note, id)
	return err
}

// TrimRuns keeps the poll_runs table from growing without bound.
func (d *DB) TrimRuns(ctx context.Context, keep int) error {
	_, err := d.w.ExecContext(ctx, `
		DELETE FROM poll_runs WHERE id NOT IN (
			SELECT id FROM poll_runs ORDER BY id DESC LIMIT ?
		)`, keep)
	return err
}

// MarkBackfilled records that an item's history has been fetched at a step.
func (d *DB) MarkBackfilled(ctx context.Context, step string, itemID, points int) error {
	_, err := d.w.ExecContext(ctx, `
		INSERT INTO backfill_state (step,item_id,done_at,points) VALUES (?,?,?,?)
		ON CONFLICT(step,item_id) DO UPDATE SET done_at=excluded.done_at, points=excluded.points`,
		step, itemID, time.Now().Unix(), points)
	return err
}

// PendingBackfill lists item ids that still need a /timeseries fetch at step, most-traded first so the useful half of the archive lands early — if the backfill is interrupted, the items people actually look up are already done.
func (d *DB) PendingBackfill(ctx context.Context, step string, limit int) ([]int, error) {
	rows, err := d.r.QueryContext(ctx, `
		SELECT i.id
		  FROM items i
		  LEFT JOIN backfill_state b ON b.item_id = i.id AND b.step = ?
		  LEFT JOIN item_stats s     ON s.item_id = i.id
		 WHERE b.item_id IS NULL AND i.removed = 0
		 ORDER BY COALESCE(s.daily_gp_vol, 0) DESC, i.id
		 LIMIT ?`, step, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// BackfillProgress reports how many items are done at a step, out of how many.
func (d *DB) BackfillProgress(ctx context.Context, step string) (done, total int, err error) {
	err = d.r.QueryRowContext(ctx,
		`SELECT count(*) FROM backfill_state WHERE step = ?`, step).Scan(&done)
	if err != nil {
		return
	}
	err = d.r.QueryRowContext(ctx,
		`SELECT count(*) FROM items WHERE removed = 0`).Scan(&total)
	return
}
