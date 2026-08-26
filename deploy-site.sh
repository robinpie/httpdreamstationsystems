#!/bin/bash
#Two carve-outs (excluded paths are protected from --delete):
#   .well-known/acme-challenge/  
#   ntpstats.txt  
set -eu

SRC="$(cd "$(dirname "$0")" && pwd)/rootdomain/"
DEST=/srv/http/

sudo rsync -a --delete \
    --exclude '.well-known/acme-challenge/' \
    --exclude 'ntpstats.txt' \
    "$SRC" "$DEST"

# Match the rest of the served tree: root-owned, world-readable.
sudo chown -R root:root "$DEST"
sudo chmod -R u=rwX,go=rX "$DEST"

echo "Deployed rootdomain/ -> /srv/http"
