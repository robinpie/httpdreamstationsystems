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

package ident

import "testing"

func TestRoundTrip(t *testing.T) {
	tok, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if len(tok.Reveal()) != TokenLen {
		t.Fatalf("token is %d chars, want %d", len(tok.Reveal()), TokenLen)
	}
	back, err := Parse(tok.Display())
	if err != nil {
		t.Fatalf("Parse(Display()) failed: %v", err)
	}
	if back != tok {
		t.Errorf("round trip changed the token: %q -> %q", tok.Reveal(), back.Reveal())
	}
}

func TestParseIsForgiving(t *testing.T) {
	tok, _ := New()
	d := tok.Display()
	// Somebody typing a code off a screen will lower-case it, drop the dashes, or add spaces. All of those must still work, or the recovery path — the only recovery path there is — fails on a technicality.
	for _, variant := range []string{
		d,
		removeAll(d, "-"),
		lower(d),
		lower(removeAll(d, "-")),
		" " + d + " ",
	} {
		got, err := Parse(variant)
		if err != nil {
			t.Errorf("Parse(%q) failed: %v", variant, err)
			continue
		}
		if got != tok {
			t.Errorf("Parse(%q) = %q, want %q", variant, got.Reveal(), tok.Reveal())
		}
	}
}

func TestParseConfusables(t *testing.T) {
	// Crockford's whole point: I and L are 1, O is 0. Somebody reading a code aloud must land on the same token.
	base := "0123456789ABCDEFGHJKMNPQRST"[:TokenLen]
	canonical, err := Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	swapped := replace(replace(replace(base, "1", "I"), "0", "O"), "8", "8")
	got, err := Parse(swapped)
	if err != nil {
		t.Fatalf("Parse(%q) failed: %v", swapped, err)
	}
	if got != canonical {
		t.Errorf("confusables not folded: %q vs %q", got.Reveal(), canonical.Reveal())
	}
}

func TestParseRejectsJunk(t *testing.T) {
	for _, bad := range []string{
		"", "short", "U" + "0123456789ABCDEFGHJKMNPQRS", // U is not in the alphabet
		"0123456789ABCDEFGHJKMNPQRSTV", // too long
		"0123456789ABCDEFGHJKMNPQR!",
	} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) should have failed", bad)
		}
	}
}

func TestHashIsStableAndNotThePlaintext(t *testing.T) {
	tok, _ := New()
	h := tok.Hash()
	if h == tok.Reveal() {
		t.Fatal("hash must not be the plaintext")
	}
	if len(h) != 64 {
		t.Errorf("hash is %d chars, want 64 hex", len(h))
	}
	if tok.Hash() != h {
		t.Error("hash is not stable")
	}
}

func TestStringDoesNotLeak(t *testing.T) {
	// A token that prints itself under %v would end up in the journal the first time any struct containing one is logged.
	tok, _ := New()
	if s := tok.String(); s == tok.Reveal() {
		t.Error("String() leaks the token")
	}
}

func TestUniqueness(t *testing.T) {
	seen := map[Token]bool{}
	for i := 0; i < 2000; i++ {
		tok, err := New()
		if err != nil {
			t.Fatal(err)
		}
		if seen[tok] {
			t.Fatalf("duplicate token after %d draws", i)
		}
		seen[tok] = true
	}
}

func removeAll(s, sub string) string { return replace(s, sub, "") }

func replace(s, old, new string) string {
	out := ""
	for i := 0; i < len(s); {
		if len(old) > 0 && i+len(old) <= len(s) && s[i:i+len(old)] == old {
			out += new
			i += len(old)
			continue
		}
		out += string(s[i])
		i++
	}
	return out
}

func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 32
		}
	}
	return string(b)
}
