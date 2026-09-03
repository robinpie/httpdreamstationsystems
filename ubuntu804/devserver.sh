#!/bin/sh
# devserver.sh — a loopback nginx for the verification loop.
#
#   ./devserver.sh start [output-dir]
#   ./devserver.sh stop
#   ./devserver.sh status
#   ./devserver.sh restart [output-dir]
#
# Serves a built tree at http://127.0.0.1:8804/tmp-ubuntu804/ so verify.py and
# a browser can see it BEFORE it is committed. Without this the only way to
# look at a change is to commit it, because the post-commit hook is the deploy
# — one screenshot per commit, on the live site.
#
# Three things about it are deliberate:
#
#   - It runs as YOU, in its own prefix under /tmp, with its own pid and logs.
#     It does not touch /etc/nginx, does not need root, and cannot disturb the
#     nginx that is actually serving the site. Which also means it works on the
#     server, where /home/robin is mode 710 and the system nginx (www-data)
#     could not read a build tree in there anyway.
#   - It listens on 127.0.0.1 only. Nothing about this should be reachable.
#   - It INCLUDES nginx/tmp-ubuntu804.conf, the same file installed into
#     /etc/nginx/snippets. So the dev loop exercises the real ssi on,
#     ssi_silent_errors, error_page and try_files directives, and the two
#     cannot drift apart. If the snippet is wrong, it is wrong here too, which
#     is the entire point.
set -eu

SRC=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PORT=${UB804_PORT:-8804}
RUN=${TMPDIR:-/tmp}/ubuntu804-dev
CONF=$RUN/nginx.conf
PIDFILE=$RUN/nginx.pid

die() { echo "devserver.sh: $*" >&2; exit 1; }

command -v nginx >/dev/null || die "nginx not installed. Debian: apt install nginx-light"

resolve_out() {
    if [ $# -ge 1 ] && [ -n "${1:-}" ]; then
        [ -d "$1" ] || die "no such output directory: $1"
        (CDPATH= cd -- "$1" && pwd)
    elif [ -d "$SRC/../build-output" ]; then
        (CDPATH= cd -- "$SRC/../build-output" && pwd)
    elif [ -d "$SRC/../rootdomain/tmp-ubuntu804" ]; then
        (CDPATH= cd -- "$SRC/../rootdomain/tmp-ubuntu804" && pwd)
    else
        die "no build output found. Run build.pl first, or name the directory."
    fi
}

write_conf() {
    out=$1
    mkdir -p "$RUN/root" "$RUN/temp"
    # The tree has to appear at /tmp-ubuntu804/ for the snippet's location and
    # for every absolute URL the build writes, so mount it there by symlink
    # rather than rewriting any paths.
    rm -f "$RUN/root/tmp-ubuntu804"
    ln -s "$out" "$RUN/root/tmp-ubuntu804"
    cat > "$CONF" <<EOF
worker_processes 1;
daemon on;
pid $PIDFILE;
error_log $RUN/error.log warn;
events { worker_connections 64; }
http {
    include /etc/nginx/mime.types;
    default_type application/octet-stream;
    access_log $RUN/access.log;
    client_body_temp_path $RUN/temp/body;
    proxy_temp_path       $RUN/temp/proxy;
    fastcgi_temp_path     $RUN/temp/fastcgi;
    uwsgi_temp_path       $RUN/temp/uwsgi;
    scgi_temp_path        $RUN/temp/scgi;
    sendfile on;
    types_hash_bucket_size 128;
    server {
        listen 127.0.0.1:$PORT;
        root $RUN/root;
        index index.html;
        include $SRC/nginx/tmp-ubuntu804.conf;
    }
}
EOF
}

running() { [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; }

case ${1:-status} in
  start)
    running && die "already running (pid $(cat "$PIDFILE")); use restart"
    out=$(resolve_out "${2:-}")
    write_conf "$out"
    nginx -t -c "$CONF" 2>&1 | grep -v 'types_hash' || true
    nginx -c "$CONF"
    sleep 1
    running || die "failed to start; see $RUN/error.log"
    echo "devserver.sh: serving $out"
    echo "              http://127.0.0.1:$PORT/tmp-ubuntu804/"
    ;;
  stop)
    running || die "not running"
    nginx -s quit -c "$CONF"
    echo "devserver.sh: stopped"
    ;;
  restart)
    if running; then nginx -s quit -c "$CONF"; sleep 1; fi
    exec "$0" start "${2:-}"
    ;;
  status)
    if running; then
      echo "devserver.sh: running, pid $(cat "$PIDFILE"), port $PORT"
      echo "              root -> $(readlink "$RUN/root/tmp-ubuntu804")"
    else
      echo "devserver.sh: not running"
    fi
    ;;
  *) die "usage: $0 {start|stop|restart|status} [output-dir]" ;;
esac
