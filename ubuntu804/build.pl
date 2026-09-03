#!/usr/bin/perl
#
# build.pl — generate the Ubuntu 8.04 desktop template's static output.
#
#   ./build.pl [output-dir]        default: ../build-output
#
# Reads site.conf, chrome.html, desktop.html, site.css and pages/*.{md,html};
# writes a complete static tree — real .html files at real paths, no client
# JavaScript anywhere, nothing assembled in the browser.
#
# This runs on the WORKSTATION, not on the server. The server never builds:
# the generated tree is rsynced into ~/configNotes/http/rootdomain/tmp-ubuntu804
# and committed there, where the existing post-commit hook deploys it. So the
# dependencies below need to exist here and nowhere else:
#
#   Text::Markdown   CPAN. Only for .md pages. Debian: libtext-markdown-perl,
#                    Arch: perl-text-markdown. Required at point of use, so a
#                    tree of nothing but .html pages builds without it.
#   pyftsubset       fonttools, for the woff2 subsetting. Needs brotli.
#                    Debian: `fonttools`, NOT `python3-fonttools` — the latter
#                    is the library only and is often already installed as
#                    somebody else's dependency, which makes this easy to
#                    misdiagnose: `import fontTools` works while the command
#                    does not exist. Arch: python-fonttools.
#
# That is the whole list. No ImageMagick, no librsvg. Every bitmap this build
# ships is committed under art/ and only copied here, because a bitmap is a
# fixed function of a fixed input and building it per-machine bought nothing:
# ImageMagick 7.1.2-HDRI here and 7.1.1 on the server disagreed about
# firefox24.png by 10% RMSE in visible panel chrome, so whichever machine built
# last silently changed the panel icon. art/make.sh regenerates them and needs
# both tools; see the reasoning at the top of it.
#
# The font subset stays a build step because it is the one artifact derived
# from the site's CONTENT — a codepoint census of the generated HTML — so it
# has to be rebuilt whenever a page changes. Its bytes do vary between
# fontTools versions, which shows up as four churned woff2 files when the build
# machine changes. Cosmetic; the coverage is identical.
#
# House style (see ~/configNotes/CLAUDE.md): core-only Perl, strict and
# warnings at the top and nothing else, expensive modules required where they
# are used. Comments explain why, especially where a line encodes something
# that was measured rather than chosen.

use strict;
use warnings;
# The %named entity table below contains literal non-ASCII characters, and this
# script reads and writes through :encoding(UTF-8) layers, so the source has to
# be decoded too. Without it '\x{d7}' is a two-BYTE string, the census records
# U+00C3 and U+0097 instead of U+00D7, and the font subset silently loses the
# multiplication sign — which is exactly what happened before this line existed.
use utf8;

# `use utf8` above decodes this file's literals, so anything printed from them
# needs an encoding layer on the way out or perl warns "Wide character" and
# mangles it. Diagnostics carrying an em dash are still diagnostics.
binmode STDOUT, ':encoding(UTF-8)';
binmode STDERR, ':encoding(UTF-8)';

# ---------------------------------------------------------------- where things are

my $SRC = $0;
$SRC =~ s{/[^/]+$}{};        # dirname($0) without File::Basename
$SRC = '.' if $SRC eq $0;
my $OUT = $ARGV[0] || "$SRC/../build-output";

# Codepoints seen across every generated file, for the font subset (step 7).
my %seen_cp;

# Every path this build wrote, relative to $OUT. prune() deletes anything in
# $OUT that is not in here, which is what makes it safe to build STRAIGHT INTO
# rootdomain/tmp-ubuntu804 with no rsync --delete step and therefore no second
# machine. Without it, deleting a page would leave its old index.html deployed
# and live forever.
my %written;

# ---------------------------------------------------------------- small helpers

sub slurp {
    my ($path) = @_;
    open my $fh, '<:encoding(UTF-8)', $path or die "build.pl: read $path: $!\n";
    local $/;
    my $s = <$fh>;
    close $fh;
    return $s;
}

sub mkpath_for {
    my ($path) = @_;
    my $dir = $path;
    $dir =~ s{/[^/]*$}{};
    return if !length $dir || -d $dir;
    require File::Path;
    File::Path::make_path($dir);
}

sub note_written {
    my ($path) = @_;
    my $rel = $path;
    $rel =~ s{^\Q$OUT\E/?}{};
    $written{$rel} = 1;
}

sub spit {
    my ($path, $content) = @_;
    note_written($path);
    mkpath_for($path);
    open my $fh, '>:encoding(UTF-8)', $path or die "build.pl: write $path: $!\n";
    print $fh $content;
    close $fh or die "build.pl: close $path: $!\n";
}

sub run {
    my @cmd = @_;
    system(@cmd) == 0 or die "build.pl: failed: @cmd\n";
}

sub esc {                    # for text and for double-quoted attributes
    my ($s) = @_;
    return '' if !defined $s;
    $s =~ s/&/&amp;/g;
    $s =~ s/</&lt;/g;
    $s =~ s/>/&gt;/g;
    $s =~ s/"/&quot;/g;
    return $s;
}

sub urlenc {                 # for the validator link's ?doc= parameter
    my ($s) = @_;
    $s =~ s{([^A-Za-z0-9._~\-])}{sprintf '%%%02X', ord $1}ge;
    return $s;
}

# ---------------------------------------------------------------- 1. site.conf

sub read_conf {
    my %c;
    for my $line (split /\n/, slurp("$SRC/site.conf")) {
        next if $line =~ /^\s*(?:#|$)/;
        my ($k, $v) = $line =~ /^(\w+):\s*(.*?)\s*$/
            or die "build.pl: site.conf: cannot parse: $line\n";
        $c{$k} = $v;
    }
    for my $k (qw(Base Root SiteName Home StripHeight)) {
        exists $c{$k} or die "build.pl: site.conf: missing $k\n";
    }
    # A trailing slash on either of these would double up in every generated
    # href, so refuse it here rather than debugging //i/mark.svg later.
    for my $k (qw(Base Root)) {
        die "build.pl: site.conf: $k must not end in '/'\n" if $c{$k} =~ m{/$};
    }
    # Scheme and host on their own, for the 404 page's location bar.
    ($c{Origin}) = $c{Base} =~ m{^(https?://[^/]+)}
        or die "build.pl: site.conf: Base is not an absolute http(s) URL\n";
    return \%c;
}

# ---------------------------------------------------------------- 2. the pages

# Each page file starts with an RFC822-style header block, then a blank line,
# then the body:
#
#     Title: About
#     Url: /about
#     Bookmark: About
#     Order: 2
#
#     ## Who runs this
#
# Title and Url are required. Bookmark is optional — omitting it keeps a page
# out of the navigation, which is how 404 stays out of it. Order sorts the
# bookmarks row. Desc becomes the meta description. Out overrides the output
# path, which only 404 needs (it must land on 404.html, not 404/index.html,
# because that is the file nginx's error_page points at).
#
# Loc overrides the text in the location bar, and is inserted VERBATIM rather
# than escaped, so it is template content and carries template trust. Only 404
# uses it, to show the address that was actually requested instead of its own:
#
#     Loc: {{ORIGIN}}<!--#echo var="REQUEST_URI" -->
#
# which nginx expands per request. #echo defaults to encoding=entity, so the
# URI is HTML-escaped by nginx before it reaches the page — verified against
# the running server with a URI full of angle brackets, not assumed.

sub read_pages {
    opendir my $dh, "$SRC/pages" or die "build.pl: opendir pages: $!\n";
    my @files = sort grep { /\.(?:md|html)$/ } readdir $dh;
    closedir $dh;
    @files or die "build.pl: pages/ has no .md or .html files\n";

    my @pages;
    for my $f (@files) {
        my $raw = slurp("$SRC/pages/$f");
        my ($head, $body) = split /\n\s*\n/, $raw, 2;
        defined $body
            or die "build.pl: $f: no blank line after the header block\n";

        my %p = (file => $f, fmt => ($f =~ /\.md$/ ? 'md' : 'html'));
        for my $line (split /\n/, $head) {
            next if $line =~ /^\s*(?:#|$)/;
            my ($k, $v) = $line =~ /^([A-Za-z]+):\s*(.*?)\s*$/
                or die "build.pl: $f: bad header line: $line\n";
            $p{lc $k} = $v;
        }
        for my $k (qw(title url)) {
            length($p{$k} || '')
                or die "build.pl: $f: header '$k' is required\n";
        }
        $p{url} =~ m{^/}
            or die "build.pl: $f: Url must start with '/': $p{url}\n";
        $p{order} = 999 unless defined $p{order} && $p{order} =~ /^-?\d+$/;
        $p{body}  = $body;

        # Output path. '/' is the tree's index; '/about' is a directory with an
        # index.html in it, so the URL needs no extension and no redirect.
        $p{out} = $p{out} ? $p{out}
                : $p{url} eq '/' ? 'index.html'
                :                  substr($p{url}, 1) . '/index.html';
        push @pages, \%p;
    }

    # Two pages writing the same file is a silent data-loss bug; catch it.
    my %by_out;
    for my $p (@pages) {
        my $clash = $by_out{ $p->{out} };
        die "build.pl: $p->{file} and $clash both write $p->{out}\n" if $clash;
        $by_out{ $p->{out} } = $p->{file};
    }

    return [ sort { $a->{order} <=> $b->{order} || $a->{url} cmp $b->{url} } @pages ];
}

# ---------------------------------------------------------------- 4. bodies

sub render_body {
    my ($p) = @_;
    return $p->{body} if $p->{fmt} eq 'html';   # passthrough, verbatim

    # Required here rather than at the top so that a tree of .html-only pages
    # builds on a machine without it.
    require Text::Markdown;
    return Text::Markdown->new->markdown($p->{body});
}

# ---------------------------------------------------------------- 3. navigation

# The bookmarks toolbar IS the site nav (requirement 4), and the Bookmarks and
# View menus mirror it. Flat list, no folders — PLAN.md section 16.
sub nav_html {
    my ($pages, $current, $c) = @_;
    my ($bm, $menu) = ('', '');
    for my $p (@$pages) {
        next unless length($p->{bookmark} || '');
        my $href = $c->{Root} . ($p->{url} eq '/' ? '/' : $p->{url});
        my $here = $p->{url} eq $current->{url} ? ' aria-current="page"' : '';
        my $lbl  = esc($p->{bookmark});
        $bm .= sprintf
            qq{        <a href="%s"%s><img src="%s/i/favicon16.png" width="16" height="16" alt="">%s</a>\n},
            esc($href), $here, $c->{Root}, $lbl;
        $menu .= sprintf qq{            <li><a href="%s"%s>%s</a></li>\n},
            esc($href), $here, esc($p->{title});
    }
    return ($bm, $menu);
}

# ---------------------------------------------------------------- 5/6. assembly

sub fill {
    my ($tpl, $vars) = @_;
    $tpl =~ s/\{\{(\w+)\}\}/
        exists $vars->{$1} ? $vars->{$1}
                           : die "build.pl: unknown token {{$1}}\n"
    /ge;
    return $tpl;
}

sub census {                 # remember every codepoint that will be rendered
    my ($html) = @_;
    my $text = $html;

    # Entity references have to be resolved before counting, or a page whose
    # only em dash is written &mdash; would drop U+2014 from the subset and
    # fall back to the full font for one glyph. Unknown names are fatal rather
    # than ignored, for exactly that reason.
    my %named = (
        amp => '&',  lt => '<',   gt => '>',   quot => '"', apos => "'",
        nbsp => "\xA0", copy => '©', reg => '®', deg => '°', middot => '·',
        laquo => '«', raquo => '»', times => '×', plusmn => '±',
        ndash => '–', mdash => '—', hellip => '…', bull => '•',
        larr => '←', rarr => '→', uarr => '↑', darr => '↓',
        ldquo => '“', rdquo => '”', lsquo => '‘', rsquo => '’',
        dagger => '†', sect => '§', para => '¶', euro => '€', pound => '£',
    );
    # Guard against the bug that put `use utf8` at the top of this file: if the
    # source ever stops being decoded, these stop being single characters and
    # the census starts recording mojibake instead of the glyph.
    for my $k (sort keys %named) {
        die "build.pl: census: \$named{$k} is not one character — is `use utf8` "
          . "still in effect?\n" if length($named{$k}) != 1;
    }

    $text =~ s/&#x([0-9A-Fa-f]+);/chr hex $1/ge;
    $text =~ s/&#(\d+);/chr $1/ge;
    $text =~ s{&([A-Za-z][A-Za-z0-9]*);}{
        exists $named{$1} ? $named{$1}
            : die "build.pl: census: unknown entity &$1; — add it to %named\n"
    }ge;

    $seen_cp{ ord $_ } = 1 for split //, $text;
}

sub build_pages {
    my ($pages, $c) = @_;
    my $chrome  = slurp("$SRC/chrome.html");
    my $desktop = slurp("$SRC/desktop.html");

    for my $p (@$pages) {
        my ($bm, $menu) = nav_html($pages, $p, $c);
        my $canonical = $c->{Base} . ($p->{url} eq '/' ? '/' : $p->{url});

        my %vars = (
            ROOT          => $c->{Root},
            ORIGIN        => esc($c->{Origin}),
            HOME          => esc($c->{Home}),
            SITENAME      => esc($c->{SiteName}),
            STRIP_H       => $c->{StripHeight},
            TITLE         => esc($p->{title}),
            DESC          => esc($p->{desc} || $c->{SiteName}),
            CANONICAL     => esc($canonical),
            CANONICAL_ENC => urlenc($canonical),
            # The location bar shows the real URL of the real page, unless the
            # page overrode it (see Loc, below). At narrow widths CSS swaps in
            # the short form (PLAN.md section 11).
            LOC_SHORT     => esc($c->{SiteName}),
            BOOKMARKS     => $bm,
            MENU_PAGES    => $menu,
        );
        # These three are filled FROM the vars above, so they have to come
        # after the hash literal rather than inside it. fill() is a single
        # pass: tokens inside a value it substitutes are never revisited, so
        # anything that carries tokens of its own has to be filled separately
        # first. That is not a stylistic point — page bodies contain
        # {{ROOT}}-relative links, and building BODY inside the hash literal
        # above shipped every one of them to production as the literal text
        # "{{ROOT}}/about". Hence also the assertion at the end of this loop.
        $vars{BODY}     = fill(render_body($p), \%vars);
        $vars{LOC_FULL} = defined $p->{loc} ? fill($p->{loc}, \%vars)
                                            : esc($canonical);
        $vars{STRIP}    = fill($desktop, \%vars);

        my $html = fill($chrome, \%vars);
        # Zero client-side JavaScript is a hard requirement (PLAN.md 4 and
        # 16), so the build enforces it rather than trusting the templates: a
        # script element, an inline event handler or a javascript: URL in the
        # output is a failed build, not a code review finding.
        for my $bad (
            [ qr/<\s*script/i,                          'a script element' ],
            [ qr/\son(?:click|load|error|submit|change|input|focus|blur|mouse\w+|key\w+|touch\w+|pointer\w+)\s*=/i,
                                                        'an inline event handler' ],
            [ qr/javascript\s*:/i,                      'a javascript: URL' ],
        ) {
            die "build.pl: $p->{file}: $bad->[1] reached the output\n"
                if $html =~ $bad->[0];
        }

        # Nothing may reach the output still wearing a token. This is the
        # cheapest possible regression test for the single-pass fill() trap
        # described above, and it is here because that trap has already been
        # fallen into once.
        die "build.pl: $p->{file}: unsubstituted token $1 reached the output\n"
            if $html =~ /(\{\{\w+\}\})/;

        census($html);
        spit("$OUT/$p->{out}", $html);
        printf "  %-22s -> %s\n", $p->{file}, $p->{out};
    }
}

# ---------------------------------------------------------------- 7a. the CSS

# site.css carries {{ROOT}}, {{STRIP_H}} and {{SUBSET_RANGE}}, so it goes
# through the same substitution as the HTML — one mechanism, not two. It is
# written after the pages because SUBSET_RANGE is not known until the census
# is complete.
sub build_css {
    my ($c, $range) = @_;
    my $css = fill(slurp("$SRC/site.css"), {
        ROOT          => $c->{Root},
        STRIP_H       => $c->{StripHeight},
        SUBSET_RANGE  => $range,
    });
    spit("$OUT/s/site.css", $css);
    print "  site.css               -> s/site.css\n";
}

# ---------------------------------------------------------------- 7b. the fonts

# Collapse the codepoint census into CSS unicode-range syntax, and into the
# comma list pyftsubset wants. Both come from the same sorted set so they
# cannot disagree — a subset that carries a glyph its unicode-range excludes is
# dead weight, and the reverse is a missing glyph.
sub codepoint_ranges {
    my (@cp) = @_;
    my @runs;
    for my $cp (@cp) {
        if (@runs && $runs[-1][1] == $cp - 1) { $runs[-1][1] = $cp }
        else                                  { push @runs, [ $cp, $cp ] }
    }
    my $css = join ', ', map {
        $_->[0] == $_->[1] ? sprintf 'U+%X', $_->[0]
                           : sprintf 'U+%X-%X', @$_
    } @runs;
    my $pyft = join ',', map {
        $_->[0] == $_->[1] ? sprintf 'U+%04X', $_->[0]
                           : sprintf 'U+%04X-%04X', @$_
    } @runs;
    return ($css, $pyft, scalar @runs);
}

sub build_fonts {
    # Printable ASCII is forced in regardless of the census. The panel clock is
    # produced by nginx at request time — "Thu Sep  3, 4:27 PM UTC" appears in
    # no source file — so every day name, month name, digit and AM/PM marker
    # has to be assumed present. Cheap insurance: 95 glyphs.
    $seen_cp{$_} = 1 for 0x20 .. 0x7E;

    my @cp = sort { $a <=> $b } keys %seen_cp;
    # Control characters carry no glyph and would only widen the ranges.
    @cp = grep { $_ >= 0x20 && $_ != 0x7F } @cp;
    my ($css_range, $pyft_range, $nruns) = codepoint_ranges(@cp);

    my @faces = (
        [ 'DejaVuSans.ttf',      'dejavu-subset.woff2',      'dejavu-full.woff2'      ],
        [ 'DejaVuSans-Bold.ttf', 'dejavu-subset-bold.woff2', 'dejavu-full-bold.woff2' ],
    );
    for my $f (@faces) {
        my ($ttf, $subset, $full) = @$f;
        note_written("$OUT/f/$subset");
        note_written("$OUT/f/$full");
        mkpath_for("$OUT/f/$subset");
        run('pyftsubset', "$SRC/fonts-src/$ttf",
            "--unicodes=$pyft_range",
            '--layout-features=*', '--flavor=woff2',
            "--output-file=$OUT/f/$subset");
        # The safety net. Declared BEFORE the subset in site.css, so the subset
        # wins for everything it covers and this is fetched only for a
        # codepoint the subset lacks.
        run('pyftsubset', "$SRC/fonts-src/$ttf",
            '--unicodes=*', '--layout-features=*', '--flavor=woff2',
            "--output-file=$OUT/f/$full");
    }
    printf "  fonts: %d codepoints in %d ranges\n", scalar @cp, $nruns;
    for my $n (map { ($_->[1], $_->[2]) } @faces) {
        printf "    %-26s %6.1f kB\n", "f/$n", (-s "$OUT/f/$n") / 1024;
    }
    return $css_range;
}

# ---------------------------------------------------------------- assets

# Copies and conversions, source of truth for what CREDITS.txt has to cover.
# Each entry: [ source, output, how ].
my @ASSETS = (
    # The wallpaper. warty-final-ubuntu.png, which despite the name is 8.04's
    # DEFAULT background — the one with the heron. heron-simple.png is the
    # plain swirls-only variant, and shipping it by mistake cost 6% on the
    # wallpaper band; see ubuntu804template.txt. Kept at full 1600x1200 rather
    # than cropped to the strip, because body's background is cover/fixed
    # against the whole viewport, so which rows show depends on the viewport's
    # aspect ratio and a crop would be right at exactly one ratio.
    [ 'art/heron.webp',              'i/heron.webp',      'copy' ],

    [ 'assets-src/header.png',       'i/header.png',      'copy' ],
    [ 'assets-src/firefox16.png',    'i/firefox16.png',   'copy' ],
    [ 'art/firefox24.png',           'i/firefox24.png',   'copy' ],
    [ 'assets-src/go-home-24.png',   'i/go-home.png',     'copy' ],
    [ 'assets-src/throbber.png',     'i/throbber.png',    'copy' ],
    [ 'assets-src/wb-minimize.png',  'i/wb-minimize.png', 'copy' ],
    [ 'assets-src/wb-maximize.png',  'i/wb-maximize.png', 'copy' ],
    [ 'assets-src/wb-close.png',     'i/wb-close.png',    'copy' ],
    [ 'art/mark.svg',                'i/mark.svg',        'copy' ],
    [ 'art/favicon16.png',           'i/favicon16.png',   'copy' ],
);
sub build_assets {
    for my $a (@ASSETS) {
        my ($src, $dst, $how) = @$a;
        my $in  = "$SRC/$src";
        my $out = "$OUT/$dst";
        -e $in or die "build.pl: missing asset source $in\n";
        note_written($out);
        mkpath_for($out);
        if ($how eq 'copy') {
            # Byte copy, not slurp/spit: these are binaries.
            open my $i, '<:raw', $in  or die "build.pl: $in: $!\n";
            open my $o, '>:raw', $out or die "build.pl: $out: $!\n";
            my $data = do { local $/; <$i> };
            print $o $data;
            close $i; close $o or die "build.pl: $out: $!\n";
        }
        else { die "build.pl: unknown asset method '$how'\n" }
        printf "  %-30s -> %-18s %6.1f kB\n", $src, $dst, (-s $out) / 1024;
    }

    # The badge shelf. Drawn by badges/make.sh and committed, so this is a copy.
    opendir my $dh, "$SRC/badges" or die "build.pl: opendir badges: $!\n";
    my @b = sort grep { /\.png$/ } readdir $dh;
    closedir $dh;
    @b or die "build.pl: badges/ is empty — run badges/make.sh\n";
    my $bytes = 0;
    for my $f (@b) {
        note_written("$OUT/i/b/$f");
        mkpath_for("$OUT/i/b/$f");
        open my $i, '<:raw', "$SRC/badges/$f" or die "build.pl: $f: $!\n";
        open my $o, '>:raw', "$OUT/i/b/$f"    or die "build.pl: $f: $!\n";
        my $data = do { local $/; <$i> };
        print $o $data;
        close $i; close $o or die "build.pl: $f: $!\n";
        $bytes += -s "$OUT/i/b/$f";
    }
    printf "  badges: %d files -> i/b/          %6.1f kB\n", scalar @b, $bytes / 1024;
}

# ---------------------------------------------------------------- 8. CREDITS.txt

sub build_credits {
    my ($c, $pages) = @_;
    # Generated rather than hand-written so it cannot drift from what actually
    # ships: the file list below is the same @ASSETS the build just copied.
    my @shipped = map { "  $_->[1]" } @ASSETS;
    my $shipped = join "\n", @shipped;
    my $stamp = do { my @t = gmtime; sprintf '%04d-%02d-%02d', $t[5] + 1900, $t[4] + 1, $t[3] };

    my $txt = <<"END";
$c->{SiteName} — Ubuntu 8.04 desktop web template
Attribution and licensing.        Generated by build.pl on $stamp.

This page's chrome is a recreation of an Ubuntu 8.04 LTS "Hardy Heron" desktop
running Firefox 3. Most of it is hand-written CSS. The parts that are not are
listed here.

NOT AFFILIATED WITH ANYBODY
---------------------------
"Ubuntu" is a registered trademark of Canonical Ltd. This site is not
affiliated with, endorsed by, sponsored by or connected to Canonical in any
way. The Ubuntu wordmark and the Circle of Friends logo are NOT used anywhere
on this site: the masthead carries an original logotype, and the light wash
behind it — which in Ubuntu 8.04 was part of the opaque headerlogo.png bitmap
that also carried those marks — was measured off that file and rebuilt as a CSS
gradient specifically so the bitmap itself would not have to ship.

"Firefox" and the Firefox logo are trademarks of the Mozilla Foundation. This
site is not Firefox and is not affiliated with, endorsed by or connected to
Mozilla. The Firefox logo and toolbar throbber below are used unmodified, and
only to depict the browser this desktop is a picture of.

THIRD-PARTY ARTWORK THAT SHIPS HERE
-----------------------------------
i/heron.webp
    "warty-final-ubuntu.png", the Ubuntu 8.04 LTS default desktop wallpaper -
    the one with the heron. (Ubuntu kept the historical Warty filename for the
    default background; heron-simple.png, despite its name, is the plain
    variant with no bird.)
    (c) Canonical Ltd. Ubuntu artwork, CC-BY-SA. Re-encoded to WebP at quality
    78; not otherwise altered, cropped or recoloured. From
    /usr/share/backgrounds/ on the Ubuntu 8.04.4 desktop-amd64 install image.

i/header.png
    The masthead gradient from /usr/share/ubuntu-artwork/img/header.png,
    10x90, tiled on x. (c) Canonical Ltd., Ubuntu artwork. Unmodified.

i/go-home.png
    "go-home" from the Human icon theme, 24x24.
    (c) Canonical Ltd. Human icon theme, CC-BY-SA 3.0 / GPL. Unmodified.

i/wb-minimize.png, i/wb-maximize.png, i/wb-close.png
    icon_minimize.png, icon_maximize.png and icon_close.png from the Human
    Metacity theme, 10x10 each.
    (c) Canonical Ltd. Human theme, CC-BY-SA 3.0 / GPL. Unmodified.

i/firefox16.png, i/firefox24.png
    Mozilla Firefox 3's window icon (default16.png and default32.png).
    (c) Mozilla Foundation; MPL 1.1 / GPL 2.0 / LGPL 2.1 tri-license, and the
    logo is additionally a Mozilla trademark. firefox24.png is default32.png
    resampled to 24x24; firefox16.png is unmodified.

i/throbber.png
    Firefox 3's idle toolbar throbber (Throbber.png, 20x20), from
    chrome/classic.jar. (c) Mozilla Foundation, MPL/GPL/LGPL tri-license.
    Unmodified.

f/dejavu-*.woff2
    DejaVu Sans and DejaVu Sans Bold, a Bitstream Vera derivative.
    Bitstream Vera Fonts Copyright (c) 2003 Bitstream, Inc.; DejaVu changes in
    the public domain. The licence permits redistribution and modification;
    subsetting and WebP-era container changes are explicitly allowed. The name
    "Bitstream Vera" is not used, as the licence requires.

CSS DERIVED FROM SOMEBODY ELSE'S STYLESHEET
-------------------------------------------
The content column's typography — the 0.90em/1.75em body, the #002B3D text,
the #6d4c07 headings and links, the 1.6em h1 with its 2px rule, the 3em/15em
margins and the 90px masthead — is taken from
/usr/share/ubuntu-artwork/ubuntu.css, which is a Plone stylesheet:

    Copyright Alexander Limi, http://www.plonesolutions.com
    Additional Plone 2 work: Joe Geldart, Tom Croucher, Michael Zeltner,
    Geir Baekholt.

That file's own header says: "Feel free to use whole or parts of this for your
own designs, but give credit where credit is due." Credit is hereby given.
One rule was deliberately not carried over: ubuntu.css restyles <strong> as a
1.3em brown pseudo-heading, which would turn every bold word on this site into
a subheading.

THE SOURCE TREE, WHICH IS ALSO PUBLIC
-------------------------------------
This site is built from a public git repository, and ubuntu804/assets-src/ in
it holds the extracted originals unmodified: heron-simple.png, header.png,
ubuntu.css, firefox-index.html, the Human go-home and window-decoration icons,
Firefox's default16/default32 and Throbber.png, and headerlogo.png. That last
one is present for reference only and is NOT shipped, for the trademark reason
given above. ubuntu804/fonts-src/ holds the two DejaVu TTFs, ubuntu804/art/ the
bitmaps derived from those originals, and
ubuntu804/reference/ holds the 1280x720 screenshot this whole template was
measured against, which depicts the Human theme, that wallpaper and Firefox 3's
chrome. Same copyrights, same licences, same attribution as everything listed
here. None of it is served.

ORIGINAL TO THIS SITE
---------------------
The logotype (i/mark.svg, i/favicon16.png), the thirteen 88x31 badges under
i/b/, and the location bar's bookmark star and autocomplete arrow (inline SVG
in the markup, drawn to match the reference rather than reusing the Human
theme's differently-shaped bookmark-new icon) were made for this site. So was every line of site.css that is not
described above, including the title bar gradient, the window frame, the
toolbars, the status bar and the masthead's light wash, all reconstructed from
measurements of a screenshot rather than copied from any file.

FILES THIS BUILD SHIPPED
------------------------
$shipped
  i/b/*.png                (13 badges, drawn by badges/make.sh)
  s/site.css
  f/dejavu-subset.woff2, f/dejavu-subset-bold.woff2
  f/dejavu-full.woff2,   f/dejavu-full-bold.woff2
END
    spit("$OUT/CREDITS.txt", $txt);
    printf "  CREDITS.txt            -> CREDITS.txt        %6.1f kB\n",
        (-s "$OUT/CREDITS.txt") / 1024;
    return;
}

# ---------------------------------------------------------------- prune

# Delete anything in $OUT this build did not write. That is what lets the
# output directory BE rootdomain/tmp-ubuntu804 rather than a staging area that
# has to be rsynced --delete into place from somewhere else, which in turn is
# what lets the whole thing be built on the server with no workstation
# involved. deploy-site.sh already rsyncs --delete from the repo to /srv/http,
# so a stale file here is a stale file live.
sub prune {
    return unless -d $OUT;

    # Two guards, because this function deletes files and takes its target
    # from the command line.
    die "build.pl: refusing to prune $OUT: it contains .git\n" if -e "$OUT/.git";

    require File::Find;
    my (@files, @dirs);
    # no_chdir makes $_ the full path, which avoids naming
    # $File::Find::name — a package variable of a runtime-required module,
    # which perl -c warns about as a possible typo.
    File::Find::find({ no_chdir => 1, wanted => sub {
        return if $_ eq $OUT;
        push @files, $_ if -f $_;
        push @dirs,  $_ if -d $_;
    } }, $OUT);

    my @stale = grep {
        my $r = $_; $r =~ s{^\Q$OUT\E/?}{}; !$written{$r}
    } @files;

    # More strangers than files of our own means $OUT is probably not what the
    # caller thinks it is. Stop instead of emptying somebody's directory.
    if (@stale > scalar keys %written) {
        die sprintf "build.pl: refusing to prune %s: %d unrecognised files "
            . "against %d written — is that the right output directory?\n",
            $OUT, scalar @stale, scalar keys %written;
    }

    for my $f (@stale) {
        unlink $f or die "build.pl: unlink $f: $!\n";
        my $r = $f; $r =~ s{^\Q$OUT\E/?}{};
        print "  pruned $r\n";
    }
    # Deepest first; rmdir declines to remove a non-empty directory, which is
    # exactly the filter wanted here.
    rmdir $_ for sort { length($b) <=> length($a) } @dirs;
}

# ---------------------------------------------------------------- main

my $conf  = read_conf();
my $pages = read_pages();

print "build.pl: $SRC -> $OUT\n";
printf "  base %s   root %s   strip %spx\n\n",
    $conf->{Base}, $conf->{Root}, $conf->{StripHeight};

build_pages($pages, $conf);
build_assets();
my $range = build_fonts();
build_css($conf, $range);
build_credits($conf, $pages);
prune();

printf "\nbuild.pl: %d pages, %d files, done.\n",
    scalar @$pages, scalar keys %written;
