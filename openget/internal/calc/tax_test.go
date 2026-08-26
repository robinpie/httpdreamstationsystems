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

package calc

import "testing"

const whip = 4151 // Abyssal whip: taxed, the traditional test item
const bond = 13190

func TestTaxRounding(t *testing.T) {
	// The wiki spells out the boundary explicitly: a seller receives 49 coins whether they list at 49 or at 50, because the 2% rounds down. Any change that breaks these numbers has broken the tax model.
	cases := []struct{ price, tax, net int64 }{
		{0, 0, 0},
		{1, 0, 1},
		{49, 0, 49},
		{50, 1, 49}, // the documented collision
		{51, 1, 50},
		{99, 1, 98},
		{100, 2, 98}, // and it generalises to every multiple of 50
		{1000, 20, 980},
		{842651, 16853, 825798}, // a real whip price from 2026-08-04
	}
	for _, c := range cases {
		if got := Tax(whip, c.price); got != c.tax {
			t.Errorf("Tax(%d) = %d, want %d", c.price, got, c.tax)
		}
		if got := NetSale(whip, c.price); got != c.net {
			t.Errorf("NetSale(%d) = %d, want %d", c.price, got, c.net)
		}
	}
}

func TestTaxCap(t *testing.T) {
	// 2% hits the 5M cap at exactly 250M.
	cases := []struct{ price, want int64 }{
		{249_999_950, 4_999_999},
		{250_000_000, 5_000_000},
		{250_000_050, 5_000_000},
		{2_000_000_000, 5_000_000},
	}
	for _, c := range cases {
		if got := Tax(whip, c.price); got != c.want {
			t.Errorf("Tax(%d) = %d, want %d", c.price, got, c.want)
		}
	}
}

func TestExempt(t *testing.T) {
	if Tax(bond, 8_000_000) != 0 {
		t.Error("bonds must be exempt from the convenience fee")
	}
	if !IsExempt(315) {
		t.Error("shrimps are on the wiki exempt list")
	}
	if IsExempt(whip) {
		t.Error("abyssal whips are not exempt")
	}
	if n := len(ExemptIDs()); n != len(exempt) {
		t.Errorf("ExemptIDs returned %d ids, want %d", n, len(exempt))
	}
}

func TestFlip(t *testing.T) {
	// Whip on 2026-08-04: instant-sell 826000, instant-buy 842651, limit 70.
	//
	// This flip LOSES money: the 16,853 gp tax is larger than the 16,651 gp spread, for a margin of -202. That is the normal state of affairs for a thin spread at 2%, and the site must show it as a loss rather than quietly clamping to zero — "the spread is positive" and "the flip is profitable" stopped being the same statement in May 2025.
	f := NewFlip(whip, 826_000, 842_651, 70)
	if f.Tax != 16_853 {
		t.Errorf("tax = %d, want 16853", f.Tax)
	}
	if f.Margin != -202 {
		t.Errorf("margin = %d, want -202", f.Margin)
	}
	if f.Potential != -202*70 {
		t.Errorf("potential = %d, want %d", f.Potential, -202*70)
	}
	if f.ROI >= 0 {
		t.Errorf("ROI = %v, want negative for a loss-making flip", f.ROI)
	}
}

func TestFlipProfitable(t *testing.T) {
	// A spread wide enough to clear the fee.
	f := NewFlip(whip, 1_000_000, 1_100_000, 8)
	if f.Tax != 22_000 {
		t.Errorf("tax = %d, want 22000", f.Tax)
	}
	if f.Margin != 78_000 {
		t.Errorf("margin = %d, want 78000", f.Margin)
	}
	if f.Potential != 624_000 {
		t.Errorf("potential = %d, want 624000", f.Potential)
	}
	if got := 78_000.0 / 1_000_000.0 * 100; f.ROI != got {
		t.Errorf("ROI = %v, want %v", f.ROI, got)
	}
}

func TestFlipUnobserved(t *testing.T) {
	// A missing price must not be reported as a margin. Claiming an item with no observed buy price has a huge margin is the single most misleading bug a flipping site can ship.
	for _, c := range [][2]int64{{0, 100}, {100, 0}, {0, 0}} {
		f := NewFlip(whip, c[0], c[1], 70)
		if f.Margin != 0 || f.ROI != 0 || f.Potential != 0 {
			t.Errorf("NewFlip(%d,%d) claimed margin %d ROI %v", c[0], c[1], f.Margin, f.ROI)
		}
	}
}

func TestAlchIsUntaxed(t *testing.T) {
	// Alching is not a GE sale, so the 2% must not appear here. Getting this wrong understates alch profit on every item on the site.
	got := AlchProfit(72_000, 100, 60_000)
	if want := int64(11_900); got != want {
		t.Errorf("AlchProfit = %d, want %d (no GE tax on alchemy)", got, want)
	}
}

func TestAlchRuneCost(t *testing.T) {
	high, low := AlchSpellByKey("high"), AlchSpellByKey("low")
	// Nature 100 gp, fire 5 gp. High burns 5 fire runes, low burns 3.
	cases := []struct {
		name  string
		spell AlchSpell
		fire  bool
		want  int64
	}{
		{"high with a staff", high, false, 100},
		{"high without", high, true, 125},
		{"low with a staff", low, false, 100},
		{"low without", low, true, 115},
	}
	for _, c := range cases {
		if got := AlchRuneCost(c.spell, 100, 5, c.fire); got != c.want {
			t.Errorf("AlchRuneCost(%s) = %d, want %d", c.name, got, c.want)
		}
	}
	// An unrecognised key must not yield a zero spell: a page priced with 0 fire runes and an empty column heading is worse than the wrong spell.
	if got := AlchSpellByKey("mud"); got.Key != "high" {
		t.Errorf("AlchSpellByKey(garbage) = %q, want the high alchemy fallback", got.Key)
	}
}
