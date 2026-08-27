#!/bin/bash
# Deploys this monorepo's two served subtrees:
#
#   rootdomain/ -> /srv/http   nginx docroot (content only, no executables)
#   cgi/        -> /srv/cgi    CGI scripts run by fcgiwrap
#
# Two carve-outs in /srv/http (excluded paths are protected from --delete):
#   .well-known/acme-challenge/   certbot webroot
#   ntpstats.txt                  written every 5 min by ntpstatsgen.timer
#
# cgi/ is a SIBLING of rootdomain/, not a subdirectory of it, so executables
# never land inside the docroot. If a location block is ever misconfigured,
# nginx has no path by which it could serve a CGI script's source.
set -eu

ROOT="$(cd "$(dirname "$0")" && pwd)"

# ---------------------------------------------------------------- syntax gate
#
# Deploy is automatic on every commit (post-commit / post-merge hooks), so a
# typo would otherwise go straight to the live site and turn the status page
# into a 502 with no warning. Abort the WHOLE deploy — content included — if
# any CGI fails to compile, so the site and its scripts never go out of sync.
#
# perl -c only catches compile-time errors, not logic ones. That is the point:
# it is a cheap gate against the realistic failure, not a test suite.
if compgen -G "$ROOT/cgi/*.cgi" >/dev/null; then
	for f in "$ROOT"/cgi/*.cgi; do
		if ! perl -c "$f" >/dev/null 2>&1; then
			echo "ABORT: $(basename "$f") fails syntax check — nothing deployed." >&2
			perl -c "$f" || true # show the user why
			exit 1
		fi
	done
fi

# -------------------------------------------------------------------- content
sudo rsync -a --delete \
	--exclude '.well-known/acme-challenge/' \
	--exclude 'ntpstats.txt' \
	"$ROOT/rootdomain/" /srv/http/

# Match the rest of the served tree: root-owned, world-readable.
sudo chown -R root:root /srv/http/
sudo chmod -R u=rwX,go=rX /srv/http/

echo "Deployed rootdomain/ -> /srv/http"

# ------------------------------------------------------------------------ cgi
if [ -d "$ROOT/cgi" ]; then
	sudo mkdir -p /srv/cgi
	sudo rsync -a --delete "$ROOT/cgi/" /srv/cgi/

	# Same posture as the docroot, except the execute bit must survive:
	# capital X keeps it only where it already exists, so scripts committed
	# 755 stay runnable and any stray data file does not become executable.
	sudo chown -R root:root /srv/cgi/
	sudo chmod -R u=rwX,go=rX /srv/cgi/

	echo "Deployed cgi/ -> /srv/cgi"
fi
