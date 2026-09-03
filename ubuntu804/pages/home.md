Title: Welcome to dreamstation.systems!
Url: /
Bookmark: Welcome
Order: 1
Desc: A small Debian VPS that mostly tells the time, wearing an Ubuntu 8.04 desktop as a website.

# Welcome to dreamstation.systems!

This is a small virtual machine with one CPU, a public IP address, and opinions
about protocols. It answers on HTTP, and also on
[Gopher, Gemini, Spartan, finger, DICT, QOTD and FTP]({{ROOT}}/links) —
and it spends most of its actual working life handing out the time to strangers
as a member of the [NTP pool](https://www.ntppool.org/).

What you are looking at is a recreation of an **Ubuntu 8.04 LTS “Hardy Heron”**
desktop running Firefox 3, rebuilt in HTML and CSS from a screenshot, pixel row
by pixel row. The panel is real. The clock is real. The menus open. Nothing here
runs any JavaScript, because there is none on this site to run.

## Getting around

The bookmarks toolbar is the navigation. So is the **Bookmarks** menu, and the
bottom of the **View** menu, because in 2008 there were always three ways to do
everything.

The Firefox icon up in the panel and the house in the toolbar both go home. The
one on the window’s own title bar does not — in Hardy that opened metacity’s
window menu, not a page, so here it is decoration. So are the window buttons at
the other end of that bar; they close nothing, being a picture of buttons.

## What this machine actually does

The short version is *time*. The long version is on the
[Now]({{ROOT}}/now) page, and the live version — probed on demand, no
caching, no JavaScript — is the
[status page](/professional/status) on the grown‐up part of this domain.

## How it was built

Everything about the construction, the measurements, the fonts, the licences and
the things deliberately left out is on the [Colophon]({{ROOT}}/colophon).
The whole site, this template included, is in a
[public git repository](https://github.com/robinpie/httpdreamstationsystems).

## Get in touch

There is a mailbox on this machine, run by Postfix and Dovecot on the same one
CPU as everything else. The address is on the
[About]({{ROOT}}/about) page, spelled out rather than linked, because
address harvesters are not the correspondents I had in mind.
