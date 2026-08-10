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

// Command openget is the OpenGET daemon: an Old School RuneScape Grand Exchange price tracker and flipping tool built on the OSRS Wiki's free real-time prices API.
//
// One binary runs everything — the ingestion pollers, the historical backfill, the nightly rollup, the web site, and the generators for the Gopher, Gemini, Spartan and finger frontends.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"dreamstation.systems/openget/internal/calc"
	"dreamstation.systems/openget/internal/config"
	"dreamstation.systems/openget/internal/ingest"
	"dreamstation.systems/openget/internal/retro"
	"dreamstation.systems/openget/internal/store"
	"dreamstation.systems/openget/internal/web"
	"dreamstation.systems/openget/internal/wiki"
)

// version is stamped by the Makefile via -ldflags.
var version = "dev"

func main() {
	var (
		cfgPath  = flag.String("config", "/etc/openget/config.toml", "path to config.toml")
		noIngest = flag.Bool("no-ingest", false, "serve only; run no pollers (for a read-only replica or local UI work)")
		noWeb    = flag.Bool("no-web", false, "ingest only; do not listen for HTTP")
		once     = flag.String("once", "", "run a single job and exit: mapping, latest, 5m, 1h, 24h, volumes, stats, maintenance")
		spike    = flag.Bool("top", false, "print the top 20 items by margin and exit (no database needed)")
		showVer  = flag.Bool("version", false, "print version and exit")
		debug    = flag.Bool("debug", false, "verbose logging")
	)
	flag.Parse()

	if *showVer {
		fmt.Println("openget", version)
		return
	}

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	// Text to stderr: systemd captures it into the journal, and the dashboard TUI reads it from there. Structured so fields survive that trip.
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Error("configuration", "err", err)
		os.Exit(1)
	}

	api, err := wiki.New(wiki.Options{
		Base:      cfg.APIBase,
		UserAgent: cfg.UserAgent,
		MinGap:    cfg.Poll.MinGap.Duration,
		Timeout:   cfg.Poll.Timeout.Duration,
	})
	if err != nil {
		log.Error("wiki client", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if *spike {
		if err := runSpike(ctx, api); err != nil {
			log.Error("spike", "err", err)
			os.Exit(1)
		}
		return
	}

	db, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		log.Error("open database", "path", cfg.DBPath, "err", err)
		os.Exit(1)
	}
	defer db.Close()

	sv, _ := db.SchemaVersion(ctx)
	log.Info("openget starting", "version", version, "db", cfg.DBPath, "schema", sv, "ua", api.UserAgent())

	ing := ingest.New(db, api, cfg, log)

	// Recipes and indices are data, reloaded from disk on every start, so a game update is a regenerate-and-restart rather than a migration.
	if cfg.DataDir != "" {
		if n, err := db.LoadRecipes(ctx, filepath.Join(cfg.DataDir, "recipes.toml")); err != nil {
			log.Error("could not load recipes", "err", err)
		} else if n > 0 {
			log.Info("recipes loaded", "count", n)
		}
		n, unresolved, err := db.LoadIndicesFile(ctx, filepath.Join(cfg.DataDir, "indices.toml"))
		switch {
		case err != nil:
			log.Error("could not load indices", "err", err)
		case len(unresolved) > 0:
			// Named constituents that no longer resolve mean a game update renamed something; the index still works, minus that item.
			log.Warn("indices loaded with unresolved constituents",
				"count", n, "unresolved", strings.Join(unresolved, ", "))
		case n > 0:
			log.Info("indices loaded", "count", n)
		}
	}

	if *once != "" {
		if err := runOnce(ctx, ing, *once); err != nil {
			log.Error("job failed", "job", *once, "err", err)
			os.Exit(1)
		}
		log.Info("job complete", "job", *once)
		return
	}

	// Retro frontends regenerate straight off the stats recomputation rather than on a timer of their own, so the gophermaps are never staler than the numbers that produced them.
	if cfg.Retro.Enabled {
		gen := retro.New(db, cfg, log)
		ing.OnStats = gen.Regenerate
		if err := gen.Regenerate(ctx); err != nil {
			log.Warn("initial retro generation failed", "err", err)
		}
	}

	errc := make(chan error, 1)

	if !*noWeb {
		srv := web.New(db, cfg, log, ing, version)
		hs := &http.Server{
			Addr:              cfg.Listen,
			Handler:           srv,
			ReadHeaderTimeout: 10 * time.Second,
			WriteTimeout:      60 * time.Second,
			IdleTimeout:       120 * time.Second,
		}
		go func() {
			log.Info("http listening", "addr", cfg.Listen)
			if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errc <- fmt.Errorf("http: %w", err)
			}
		}()
		defer func() {
			sc, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = hs.Shutdown(sc)
		}()
	}

	if !*noIngest {
		go func() {
			ing.Run(ctx)
			errc <- nil
		}()
	}

	select {
	case <-ctx.Done():
		log.Info("shutting down")
	case err := <-errc:
		if err != nil {
			log.Error("fatal", "err", err)
			stop()
			os.Exit(1)
		}
	}
}

func runOnce(ctx context.Context, ing *ingest.Ingester, job string) error {
	switch job {
	case "mapping":
		return ing.Mapping(ctx)
	case "latest":
		return ing.Latest(ctx)
	case "5m":
		return ing.BucketPoll(ctx, wiki.Step5m)
	case "1h":
		return ing.BucketPoll(ctx, wiki.Step1h)
	case "24h":
		return ing.BucketPoll(ctx, wiki.Step24h)
	case "volumes":
		return ing.Volumes(ctx)
	case "stats":
		return ing.Recompute(ctx)
	case "maintenance":
		return ing.Maintenance(ctx)
	default:
		return fmt.Errorf("unknown job %q", job)
	}
}

// runSpike backs the -top flag: prove the data path end to end with no database, no HTML and no storage — just fetch, join, compute, print.
func runSpike(ctx context.Context, api *wiki.Client) error {
	items, err := api.Mapping(ctx)
	if err != nil {
		return err
	}
	latest, err := api.Latest(ctx)
	if err != nil {
		return err
	}
	type row struct {
		name  string
		flip  calc.Flip
		limit int
	}
	var rows []row
	for _, it := range items {
		l, ok := latest[it.ID]
		if !ok || l.High == nil || l.Low == nil {
			continue
		}
		limit := 0
		if it.Limit != nil {
			limit = *it.Limit
		}
		rows = append(rows, row{it.Name, calc.NewFlip(it.ID, *l.Low, *l.High, limit), limit})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].flip.Margin > rows[j].flip.Margin })

	fmt.Printf("%d items, %d with both prices observed\n\n", len(items), len(rows))
	fmt.Printf("%-32s %12s %12s %10s %8s %7s\n", "ITEM", "BUY", "SELL", "MARGIN", "LIMIT", "ROI")
	for i, r := range rows {
		if i >= 20 {
			break
		}
		fmt.Printf("%-32s %12d %12d %10d %8d %6.2f%%\n",
			trunc(r.name, 32), r.flip.Buy, r.flip.Sell, r.flip.Margin, r.limit, r.flip.ROI)
	}
	return nil
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
