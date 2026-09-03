Title: Now
Url: /now
Bookmark: Now
Order: 4
Desc: What this machine is running at the moment, and what it is not.

# What this machine is doing now

Last reviewed by hand: **3 September 2026**. For anything that needs to be
right *this second* rather than roughly right, use the
[live status page](/professional/status) — it probes each service when you ask
it to, and it does not cache.

## Running

**Time.** NTP, in `pool.ntp.org`, plus NTS on port 4460 for clients that want
their time signed. This is by an enormous margin the busiest thing on the
machine, and every other service here is a guest in its house. It is also why
the clock in the panel above is served by an nginx SSI directive and not by a
CGI: one fork per page view is a rounding error until it is not.

**The web.** nginx, serving this domain and
`grandexchange.dreamstation.systems`, with one Perl CGI behind fcgiwrap for the
status page.

**Mail.** Postfix, Dovecot and OpenDKIM. One mailbox.

**The old protocols.** Gopher (gophernicus), Gemini (molly-brown), Spartan,
finger, DICT (dictd), QOTD, anonymous FTP (vsftpd), and the four small services
— daytime, echo, discard and chargen. The Spartan server, the finger daemon,
QOTD and the small services are all hand‐rolled, which is a polite way of
saying they are short.

**OpenGET.** An Old School RuneScape Grand Exchange price tracker, served over
HTTP, Gopher, Gemini, Spartan and finger, because a thing worth serving is
worth serving five ways.

**Analytics for the time service.** NTP traffic statistics, regenerated on a
timer and published for Gemini, Gopher and HTTP.

## Not running

**Iodine** — IP-over-DNS, a personal toy rather than a public service. It has
been down since **28 August 2026**, having lost a dependency it needed and
never asked for out loud. It is the only thing on this list that is supposed to
be up and is not.

## Deliberately absent

No JavaScript. No cookies. No analytics, no beacons, no fonts fetched from
somebody else’s CDN, no third‐party anything. Nothing on this site knows you
were here except nginx’s access log.
