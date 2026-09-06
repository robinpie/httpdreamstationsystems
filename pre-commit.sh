#!/bin/bash
# pre-commit: run every pre-commit step, in order.
#
# .git/hooks/pre-commit used to be a symlink straight to datestamp-hook.pl,
# because there was only one step. There are two now, and git runs exactly one
# pre-commit hook, so the symlink points here instead and this file calls both:
#
#     datestamp-hook.pl        schema.org dateModified on staged HTML
#     assets-build/make-feed.py  Atom + RSS for /personal/blog.html
#
# ADDING A STEP: put it below, and make it re-stage anything it rewrites. A
# step that edits a file without `git add`ing it produces a commit whose
# contents do not match what the hook computed.
#
# See githooks.txt for the symlink convention and why core.hooksPath is not
# used. Install with:
#
#     ln -sf ../../pre-commit.sh .git/hooks/pre-commit
set -eu

# NOT dirname "$0": git invokes this through the .git/hooks/pre-commit
# symlink, so $0 is that path and dirname lands in .git/hooks, where none of
# the scripts below exist. `git rev-parse --show-toplevel` resolves the repo
# root whatever path the hook was reached by — the same thing deploy-hook.sh
# does, and for the same reason.
ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"

# ------------------------------------------------------- schema.org datestamp
./datestamp-hook.pl

# ---------------------------------------------------------------------- feeds
#
# feed.xml and rss.xml are generated from blog.html and the og:description of
# each post, so they are committed artifacts: the deploy is a plain rsync of
# rootdomain/ and has no build step that could produce them later.
#
# THE UNSTAGED GUARD, matching datestamp-hook.pl's: the generator reads the
# WORKING TREE, not the index. If a source file has unstaged edits, the feed
# built from it would describe posts this commit does not contain — so the
# feeds are left exactly as they are and the commit proceeds. This is a
# warning, not an abort: an unrelated commit should not be blocked by a draft
# sitting in the tree.
feed_sources=$(git diff --name-only -- \
	'rootdomain/personal/blog.html' 'rootdomain/personal/*.html' \
	| grep -v -e 'personal/feed\.xml$' -e 'personal/rss\.xml$' || true)

if [ -n "$feed_sources" ]; then
	echo "pre-commit: unstaged changes under rootdomain/personal/ —" >&2
	echo "pre-commit: feeds NOT regenerated. Stage or stash, then re-run." >&2
	echo "$feed_sources" | sed 's/^/pre-commit:   /' >&2
else
	./assets-build/make-feed.py
	# Only stage them if they actually moved. `git add` on unchanged files is
	# harmless but noisy in the hook's output, and this keeps a commit that
	# touched no post from silently listing the feeds among its changes.
	for f in rootdomain/personal/feed.xml rootdomain/personal/rss.xml; do
		if ! git diff --quiet -- "$f"; then
			git add "$f"
			echo "pre-commit: re-staged $f"
		fi
	done
fi
