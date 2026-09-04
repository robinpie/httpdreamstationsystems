#!/bin/bash
# status-sample.sh — collect the things the status page needs that its CGI
# cannot get for itself. Runs every 30s via status-sample.timer.
#
# WHY THIS EXISTS
# ---------------
# /srv/cgi/status.cgi renders per request as www-data. Three kinds of figure
# are out of its reach:
#
#   1. A rolling 5-minute CPU average. The CGI only runs when somebody visits,
#      and a resume-linked page can go hours between visits — so it cannot
#      collect its own history without the bar almost always reading
#      "instantaneous" instead of "5 min avg".
#
#   2. NTS-KE counters. `chronyc serverstats` is one of the few chronyc
#      commands restricted to root (unix-socket only, regardless of
#      allow/cmdallow). The alternative was a sudoers rule for www-data;
#      giving the web-facing user sudo to read a vanity statistic is a bad
#      trade, so root collects it here and the CGI reads a plain file.
#
#   3. A month of disk history. Same problem as the CPU average but three
#      orders of magnitude worse: nobody visits the page often enough to build
#      a 30-day series out of page views. This is also the only figure here
#      whose history outlives a reboot — see the disk block at the bottom.
#
# This mirrors ntp-qps-sample.sh and tor-bridge-sample.sh: a small privileged
# collector feeding an unprivileged reader.
#
# Output, all world-readable:
#   /run/status/cpu.hist      "<ts> <total_jiffies> <idle_jiffies>", 5min kept
#   /run/status/chrony.txt    "<key> <value>" lines, latest snapshot only
#   /run/status/qps.txt       three condensed NTP rate figures
#   /run/status/disk.txt      current disk figures + a downsampled 30d series
#   /var/lib/status/disk.hist THE ONE PERSISTENT FILE. Append-only, forever.
#
# tmpfs for everything except that last one — / sits around 80% and this box
# has a documented ENOSPC history (see ntpset.txt). The tmpfs files are a few
# KB total and never touch disk. Cleared on reboot, which is harmless: the CGI
# degrades to a shorter labelled window, chrony's counters are "since chronyd
# start" anyway, and disk.txt is rebuilt from the persistent history on the
# first tick after boot.
set -eu

CPU_HIST=/run/status/cpu.hist
CHRONY_OUT=/run/status/chrony.txt
QPS_OUT=/run/status/qps.txt
QPS_HIST=/var/lib/dashboard/ntp_qps.hist
WINDOW=300 # seconds of CPU history to retain

# Disk. Unlike everything else here the history is PERSISTENT and kept forever,
# so it lives in the state directory rather than on tmpfs — see the disk block
# at the bottom of this file for the size arithmetic.
DISK_HIST=/var/lib/status/disk.hist
DISK_OUT=/run/status/disk.txt
DISK_FS=/
DISK_INTERVAL=300     # seconds between disk samples (this timer ticks at 30s)
DISK_WINDOW=2592000   # 30 days, the span the status page graphs
DISK_STEP=7200        # 2h buckets -> at most 360 plotted points
DISK_TAIL=12000       # lines scanned to build those buckets (~41 days at 5min)

# systemd-tmpfiles creates /run/status at boot; create it here too so a manual
# run before the first tmpfiles pass still works.
mkdir -p /run/status

now=$(date +%s)

# ---------------------------------------------------------------- CPU sample
#
# /proc/stat's first line:
#   cpu  user nice system idle iowait irq softirq steal guest guest_nice
# total = every field; idle = idle + iowait.
#
# Counting iowait as idle is deliberate: it measures CPU *executing*, not
# "unable to schedule". On a box whose busiest service is network-bound that
# is the honest reading, and it is what the page's label claims. Verified
# against vmstat at install time.
read -r _ user nice system idle iowait irq softirq steal _ < /proc/stat
total=$((user + nice + system + idle + iowait + irq + softirq + steal))
idle_all=$((idle + iowait))

# Append, then keep only samples inside the window. Written via a temp file and
# renamed so the CGI never reads a half-written history.
#
# Rewriting the whole file each tick is fine here and is NOT the mistake
# ntp-qps-sample.sh had to avoid with TRIM_EVERY: this file is ten lines, not
# seven days of 5-second samples.
tmp=$(mktemp /run/status/.cpu.XXXXXX)
{
	[ -f "$CPU_HIST" ] && awk -v cutoff="$((now - WINDOW))" '$1 >= cutoff' "$CPU_HIST"
	echo "$now $total $idle_all"
} >"$tmp"
chmod 644 "$tmp"
mv -f "$tmp" "$CPU_HIST"

# ------------------------------------------------------------- chrony sample
#
# `chronyc -c serverstats` emits one CSV row. Field order (chrony 4.x):
#   1 NTP packets received      2 NTP packets dropped
#   3 Command packets received  4 Command packets dropped
#   5 Client log records dropped
#   6 NTS-KE connections accepted   <-- wanted
#   7 NTS-KE connections dropped    <-- wanted
#   8 Authenticated NTP packets
#   ...timestamp counters follow
#
# If chronyd is down or the call is refused, leave the previous snapshot in
# place rather than writing zeros: the CGI can tell "stale" from "genuinely
# zero" by the file's own sampled_at, and a zero here would render as a real
# figure.
if stats=$(timeout 5 /usr/bin/chronyc -c serverstats 2>/dev/null) && [ -n "$stats" ]; then
	IFS=, read -r _ _ _ _ _ nts_accepted nts_dropped _ <<<"$stats"
	# Guard against chronyc emitting an error string where a number belongs —
	# a bug that has bitten this box before (see ntpset.txt).
	if [[ $nts_accepted =~ ^[0-9]+$ && $nts_dropped =~ ^[0-9]+$ ]]; then
		tmp=$(mktemp /run/status/.chrony.XXXXXX)
		{
			echo "sampled_at $now"
			echo "nts_ke_accepted $nts_accepted"
			echo "nts_ke_dropped $nts_dropped"
		} >"$tmp"
		chmod 644 "$tmp"
		mv -f "$tmp" "$CHRONY_OUT"
	fi
fi

# ---------------------------------------------------------------- NTP rates
#
# Condense /var/lib/dashboard/ntp_qps.hist (~95k lines, ~3.3MB, appended every
# 5s by ntp-qps-sample.service) down to three numbers.
#
# This lives here rather than in the CGI because parsing that file costs
# ~263ms — measured — which was the single largest item on the request path
# after module loading. Doing it here moves the cost onto a Nice=10 timer that
# was already running, and the CGI just reads three fields.
#
# RESET HANDLING is the reason this is a full scan and not a head/tail read.
# The nftables counter is lifetime-cumulative but resets to zero on nft reload
# or reboot. Averaging straight across a reset silently understates the rate
# for up to seven days — a real bug this box has already hit once (a reboot
# made the 7-day average read ~930 pkt/s when the true rate was ~4700-5600).
# A decrease between consecutive samples starts a new averaging window.
if [ -r "$QPS_HIST" ]; then
	if qps=$(awk '
		{
			ts = $1; pkts = $2
			if (pkts !~ /^[0-9]+$/) next
			if (!have || pkts < prev) { win_ts = ts; win_pkts = pkts; have = 1 }
			prev = pkts
			# keep the trailing pair for the instantaneous rate
			p_ts = l_ts; p_pkts = l_pkts
			l_ts = ts;   l_pkts = pkts
		}
		END {
			if (!have) exit 1
			span = l_ts - win_ts
			if (span > 0) printf "avg %.2f\navg_span %d\n", (l_pkts - win_pkts) / span, span
			dt = l_ts - p_ts
			if (dt > 0 && l_pkts >= p_pkts) printf "now %.2f\n", (l_pkts - p_pkts) / dt
		}
	' "$QPS_HIST" 2>/dev/null) && [ -n "$qps" ]; then
		tmp=$(mktemp /run/status/.qps.XXXXXX)
		{
			echo "sampled_at $now"
			echo "$qps"
		} >"$tmp"
		chmod 644 "$tmp"
		mv -f "$tmp" "$QPS_OUT"
	fi
fi

# -------------------------------------------------------------- disk usage
#
# TWO OUTPUTS WITH DIFFERENT LIFETIMES, which is the whole shape of this block:
#
#   /var/lib/status/disk.hist   PERSISTENT, append-only, KEPT FOREVER.
#                               "<ts> <used_kb> <total_kb> <avail_kb>"
#   /run/status/disk.txt        tmpfs, rewritten each sample: the current
#                               figures plus a downsampled 30-day series,
#                               which is all the status page ever reads.
#
# Everything else this script writes is on tmpfs precisely because / has a
# documented ENOSPC history (ntpset.txt) and sits around 80%. The history is
# the deliberate exception, so here is the arithmetic that makes it safe:
# one ~40-byte line every 5 minutes is 288 lines/day, ~4MB/year, growing
# strictly linearly with no compaction ever needed. A monitoring file that
# could itself fill the disk it monitors would be a genuinely stupid failure
# mode; 4MB/year is not that.
#
# The 5-minute cadence is enforced HERE rather than by a second timer, because
# this unit already wakes every 30 seconds and a whole systemd unit to run df
# would be more machinery than the job deserves. The interval is measured from
# the last line of the persistent history rather than from a stamp file, so a
# reboot resumes the cadence correctly instead of restarting it.
mkdir -p "$(dirname "$DISK_HIST")"

disk_last=0
if [ -s "$DISK_HIST" ]; then
	disk_last=$(tail -n 1 "$DISK_HIST" | cut -d' ' -f1)
	case $disk_last in '' | *[!0-9]*) disk_last=0 ;; esac
fi

# The -f test regenerates the condensed file after a reboot has wiped tmpfs,
# even when the next sample is not due yet, so the page is never blank for up
# to five minutes just because the box restarted.
if [ $((now - disk_last)) -ge "$DISK_INTERVAL" ] || [ ! -f "$DISK_OUT" ]; then
	# -P forces the POSIX one-line-per-filesystem format: without it a long
	# device name wraps onto its own line and the fields shift. -k fixes the
	# unit at 1KiB blocks regardless of the caller's environment.
	#
	# Note df's Used + Available does NOT equal Size: ext4 reserves ~5% for
	# root. All three are stored, so the page can show that reserve as its own
	# segment instead of pretending the gap is free space.
	if disk=$(df -P -k "$DISK_FS" 2>/dev/null | awk 'NR==2 {print $2, $3, $4}') && [ -n "$disk" ]; then
		read -r d_total d_used d_avail <<<"$disk"
		if [[ $d_total =~ ^[0-9]+$ && $d_used =~ ^[0-9]+$ && $d_avail =~ ^[0-9]+$ && $d_total -gt 0 ]]; then
			# Only append when a sample is actually due. The -f branch above
			# rebuilds the condensed file from existing history without
			# polluting it with an off-cadence extra row.
			if [ $((now - disk_last)) -ge "$DISK_INTERVAL" ]; then
				echo "$now $d_used $d_total $d_avail" >>"$DISK_HIST"
				chmod 644 "$DISK_HIST" 2>/dev/null || true
			fi

			# Downsample to one point per DISK_STEP, taking the MAXIMUM used in
			# each bucket. Max rather than mean because the interesting question
			# a disk graph answers is "how close did we come to full", and an
			# average would smooth away exactly the spike that matters.
			#
			# tail before awk is load-bearing: the history grows without bound,
			# so a full scan would get slower every day it runs. Reading a fixed
			# 12000 lines from the end keeps this at constant cost forever, and
			# covers ~41 days against a 30-day window.
			# Each point is "p <bucket> <ts> <used_kb>": the bucket it belongs
			# to, the ACTUAL TIME of the peak sample within that bucket, and
			# the value. Both timestamps are needed and they do different jobs:
			#
			#   bucket  a regular 2h grid, so the CGI can tell a missing bucket
			#           from a present one and break the line on real gaps.
			#   ts      where to actually plot the point. Plotting at the
			#           bucket floor instead misplaces a point by up to 2h,
			#           which is nothing across 30 days but is most of the
			#           width when the history is only a few hours old and the
			#           graph has compressed its axis to fit.
			disk_cut=$((now - DISK_WINDOW))
			if series=$(tail -n "$DISK_TAIL" "$DISK_HIST" 2>/dev/null | awk \
				-v cut="$disk_cut" -v step="$DISK_STEP" '
				$1 ~ /^[0-9]+$/ && $2 ~ /^[0-9]+$/ && $1 >= cut {
					b = int($1 / step) * step
					if (!(b in m) || $2 + 0 > m[b]) { m[b] = $2 + 0; t[b] = $1 + 0 }
				}
				END { for (b in m) printf "p %d %d %d\n", b, t[b], m[b] }
			' | sort -n -k2); then
				tmp=$(mktemp /run/status/.disk.XXXXXX)
				{
					echo "sampled_at $now"
					echo "used $d_used"
					echo "total $d_total"
					echo "avail $d_avail"
					echo "window $DISK_WINDOW"
					echo "step $DISK_STEP"
					# first_ts lets the page say "history began N ago" instead
					# of implying a full month of data it does not have yet.
					head -n 1 "$DISK_HIST" | awk '$1 ~ /^[0-9]+$/ {print "first_ts", $1}'
					echo "$series"
				} >"$tmp"
				chmod 644 "$tmp"
				mv -f "$tmp" "$DISK_OUT"
			fi
		fi
	fi
fi
