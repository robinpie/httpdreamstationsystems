# dreamstation.systems 🏰 web monorepo

Monorepo

Does not include mta-sts.dreamstation.systems or pool.ntp.org (the latter just redirects you to ntppool.org).

| Subtree       |  Domain          | License             |
|---------------|--------------------------------------|--------------------|
| `openget/`    | https://grandexchange.dreamstation.systems  |  GPL-2.0-only |
| `rootdomain/` | https://dreamstation.systems              |  mixed / messy |
| `cgi/`        | https://dreamstation.systems/professional/status |  mixed / messy |

All content also available over HTTP.

`cgi/` is the one subtree that is executable rather than content. It deploys to
`/srv/cgi`, a **sibling** of nginx's docroot rather than a directory inside it,
so no script ever sits somewhere nginx could serve its source. See
`~/configNotes/status.txt`.