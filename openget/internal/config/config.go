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

// Package config loads /etc/openget/config.toml.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Duration wraps time.Duration so TOML can carry "5m" instead of nanoseconds.
type Duration struct{ time.Duration }

// UnmarshalText parses a Go duration string.
func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	d.Duration = v
	return nil
}

// MarshalText renders the duration back to a string.
func (d Duration) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// Config is the whole configuration file.
type Config struct {
	Listen    string `toml:"listen"`     // host:port for the HTTP daemon
	DBPath    string `toml:"db_path"`    // SQLite file
	BaseURL   string `toml:"base_url"`   // public origin, for absolute links
	UserAgent string `toml:"user_agent"` // sent upstream; policy-required
	APIBase   string `toml:"api_base"`   // override for testing
	IconDir   string `toml:"icon_dir"`   // local mirror of item icons
	DataDir   string `toml:"data_dir"`   // recipes.toml and indices.toml live here
	DevMode   bool   `toml:"dev_mode"`   // re-read templates on each request

	Poll      Poll      `toml:"poll"`
	Retention Retention `toml:"retention"`
	Backfill  Backfill  `toml:"backfill"`
	Retro     Retro     `toml:"retro"`
	Limits    Limits    `toml:"limits"`
}

// Poll controls the steady-state ingestion cadence.
//
// There is no point polling faster than these defaults: prices update on RuneLite's 5-minute cadence and the CDN sets cache-control max-age=60, so a tighter loop just re-reads the same bytes.
type Poll struct {
	Latest  Duration `toml:"latest"`
	FiveMin Duration `toml:"five_min"`
	Hourly  Duration `toml:"hourly"`
	Daily   Duration `toml:"daily"`
	Volumes Duration `toml:"volumes"`
	MinGap  Duration `toml:"min_gap"` // politeness spacing between requests
	Timeout Duration `toml:"timeout"`
}

// Retention sets how long each price tier is kept. Zero means forever.
type Retention struct {
	FiveMin Duration `toml:"five_min"`
	Hourly  Duration `toml:"hourly"`
	Daily   Duration `toml:"daily"`
	Volumes Duration `toml:"volumes"`

	// MinFreeDiskMB pauses ingestion writes when the filesystem holding the database drops below this many megabytes free. The archive is the whole point of this service, but so is the rest of the box staying up: an unattended poller must not be the thing that fills the root filesystem. Zero disables the check.
	MinFreeDiskMB int64 `toml:"min_free_disk_mb"`
}

// Backfill controls the one-time historical seed.
type Backfill struct {
	Enabled    bool     `toml:"enabled"`
	Steps      []string `toml:"steps"`        // e.g. ["24h","6h","1h"]
	RatePerSec float64  `toml:"rate_per_sec"` // requests/sec, be polite
	BatchSize  int      `toml:"batch_size"`   // items per pass
	Pause      Duration `toml:"pause"`        // between passes
}

// Retro configures the non-HTTP frontends.
type Retro struct {
	GopherDir  string   `toml:"gopher_dir"`  // where to write gophermaps
	GeminiDir  string   `toml:"gemini_dir"`  // where to write gemtext
	WriteEvery Duration `toml:"write_every"` // regeneration cadence
	Enabled    bool     `toml:"enabled"`
	TopN       int      `toml:"top_n"` // rows per generated list
}

// Limits bounds per-token storage so the tokens cannot be abused as free hosting.
type Limits struct {
	Favourites  int      `toml:"favourites"`
	LedgerRows  int      `toml:"ledger_rows"`
	Alerts      int      `toml:"alerts"`
	MintPerHour int      `toml:"mint_per_hour"` // token mints per IP per hour
	PruneAfter  Duration `toml:"prune_after"`
}

// Default returns the built-in configuration. Every field is overridable, but the defaults are chosen to be a sane, polite production setup on their own.
func Default() Config {
	return Config{
		Listen:  "127.0.0.1:4151", // 4151 == Abyssal whip, and nothing else on this box wants it
		DBPath:  "/var/lib/openget/openget.db",
		BaseURL: "https://grandexchange.dreamstation.systems",
		IconDir: "/var/lib/openget/icons",
		DataDir: "/usr/local/share/openget",
		Poll: Poll{
			Latest:  Duration{60 * time.Second},
			FiveMin: Duration{5 * time.Minute},
			Hourly:  Duration{time.Hour},
			Daily:   Duration{24 * time.Hour},
			Volumes: Duration{time.Hour},
			MinGap:  Duration{500 * time.Millisecond},
			Timeout: Duration{60 * time.Second},
		},
		Retention: Retention{
			FiveMin:       Duration{30 * 24 * time.Hour},
			Hourly:        Duration{2 * 365 * 24 * time.Hour},
			Daily:         Duration{0}, // forever
			Volumes:       Duration{90 * 24 * time.Hour},
			MinFreeDiskMB: 512,
		},
		Backfill: Backfill{
			Enabled:    true,
			Steps:      []string{"24h"},
			RatePerSec: 2,
			BatchSize:  250,
			Pause:      Duration{time.Minute},
		},
		Retro: Retro{
			Enabled:    true,
			GopherDir:  "/srv/gopher/ge",
			GeminiDir:  "/srv/gemini/ge",
			WriteEvery: Duration{5 * time.Minute},
			TopN:       50,
		},
		Limits: Limits{
			Favourites:  500,
			LedgerRows:  50000,
			Alerts:      100,
			MintPerHour: 20,
			PruneAfter:  Duration{365 * 24 * time.Hour},
		},
	}
}

// Load reads path over the defaults. A missing file is not an error — the defaults are a working configuration — but a malformed one is.
func Load(path string) (Config, error) {
	c := Default()
	if path == "" {
		return c, c.validate()
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, c.validate()
	}
	if err != nil {
		return c, err
	}
	if err := toml.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("config: %s: %w", path, err)
	}
	return c, c.validate()
}

func (c *Config) validate() error {
	if strings.TrimSpace(c.UserAgent) == "" {
		return fmt.Errorf("config: user_agent is required — the OSRS Wiki API " +
			"policy-blocks default library User-Agents, and a descriptive one " +
			"with contact info is the price of admission")
	}
	if c.Listen == "" {
		return fmt.Errorf("config: listen is required")
	}
	if c.DBPath == "" {
		return fmt.Errorf("config: db_path is required")
	}
	if c.Backfill.RatePerSec <= 0 {
		c.Backfill.RatePerSec = 2
	}
	if c.Backfill.RatePerSec > 5 {
		// The backfill is thousands of requests against a free service run by volunteers. Refuse to be the reason they add a rate limiter.
		return fmt.Errorf("config: backfill.rate_per_sec = %v is impolite; keep it at or below 5", c.Backfill.RatePerSec)
	}
	if c.Retro.TopN <= 0 {
		c.Retro.TopN = 50
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	return nil
}
