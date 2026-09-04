# dreamstation.systems 🏰 web monorepo

Monorepo for everything the web server serves, plus the config that serves it.

Does not include pool.ntp.org (that vhost just redirects you to ntppool.org —
the domain is the NTP pool project's, not ours; only the redirect is here).

| Subtree          | Domain / destination                             | License        |
|------------------|--------------------------------------------------|----------------|
| `openget/`       | https://grandexchange.dreamstation.systems       | GPL-2.0-only   |
| `rootdomain/`    | https://dreamstation.systems                     | mixed / messy  |
| `cgi/`           | https://dreamstation.systems/professional/status | mixed / messy  |
| `ubuntu804/`     | https://dreamstation.systems/tmp-ubuntu804       | mixed / messy  |
| `nginx/`         | `/etc/nginx`                                     | mixed / messy  |
| `status-sample/` | `/usr/local/bin` + systemd                       | mixed / messy  |
| `assets-build/`  | build-time only, ships nothing                   | mixed / messy  |

`rootdomain/` also serves mta-sts.dreamstation.systems and
openpgpkey.dreamstation.systems — both are separate vhosts in `nginx/` sharing
the one docroot, so `.well-known/mta-sts.txt` and the WKD tree under
`.well-known/openpgpkey/` are in this repo too.

Only `rootdomain/` is reachable over HTTP. Every other subtree is its sibling,
never a subdirectory, so nothing executable or build-time can land in the
docroot even if a location block is misconfigured.

All content also available over Gopher, Gemini, Spartan and finger.

Notes on how the pieces work: `nginx.txt`, `site-assets.txt`, `githooks.txt`,
`ubuntu804/ubuntu804template.txt`, `unicodePedanticism.txt`. Notes on services this repo
does not own live in `~/configNotes/` — `status.txt` and `openget.txt` are the
two worth reading alongside it.

Some scripting is AI-assisted.
