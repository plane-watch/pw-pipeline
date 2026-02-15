#!/usr/bin/env python3
"""Tap NATS enrichment request/reply traffic and report timing outliers."""

import argparse
import asyncio
import json
import math
import re
import signal
import time
from collections import defaultdict

import nats

# 3 letters followed by digits = airline callsign (e.g. QFA646, UAL123)
AIRLINE_CALLSIGN_RE = re.compile(r"^[A-Z]{3}\d+$")

# Track in-flight requests: reply_subject -> (request_time, payload)
pending = {}
stats = defaultdict(list)

THRESHOLD_MS = 100  # report anything over this

# Rolling metrics
max_pending = 0
total_requests = 0
total_replies = 0
total_lost = 0
total_empty = 0
total_empty_airline = 0
total_errors = 0
total_nonempty = 0
window_start = 0.0
window_requests = 0
sample_counter = 0
empty_airline_callsigns = defaultdict(int)  # callsign -> count (all time)
window_empty_airline = defaultdict(int)     # callsign -> count (this window)


def calc_percentile(sorted_vals, p):
    idx = int(len(sorted_vals) * p / 100)
    return sorted_vals[min(idx, len(sorted_vals) - 1)]


def calc_stdev(vals):
    if len(vals) < 2:
        return 0.0
    avg = sum(vals) / len(vals)
    variance = sum((v - avg) ** 2 for v in vals) / (len(vals) - 1)
    return math.sqrt(variance)


def print_window_stats(label, vals, threshold):
    if not vals:
        return
    s = sorted(vals)
    avg = sum(s) / len(s)
    sd = calc_stdev(s)
    over = sum(1 for v in s if v > threshold)
    print(
        f"STATS [{label}]  n={len(s)}  "
        f"avg={avg:.1f}ms  stdev={sd:.1f}ms  "
        f"min={s[0]:.1f}  p50={calc_percentile(s, 50):.1f}  "
        f"p95={calc_percentile(s, 95):.1f}  p99={calc_percentile(s, 99):.1f}  "
        f"max={s[-1]:.1f}ms  "
        f">{threshold:.0f}ms={over} ({100 * over / len(s):.1f}%)"
    )


def print_summary(threshold):
    print(f"\n{'=' * 100}")
    print(f"Session summary")
    print(f"{'=' * 100}")
    print(
        f"  total requests:    {total_requests}\n"
        f"  total replies:     {total_replies}\n"
        f"  total lost:        {total_lost}\n"
        f"  total nonempty:    {total_nonempty}\n"
        f"  total empty:       {total_empty}\n"
        f"  empty (airline):   {total_empty_airline}\n"
        f"  total errors:      {total_errors}\n"
        f"  max outstanding:   {max_pending}"
    )
    if empty_airline_callsigns:
        top = sorted(
            empty_airline_callsigns.items(), key=lambda x: -x[1]
        )[:25]
        print(f"\n  Top empty airline callsigns:")
        for cs, count in top:
            print(f"    {cs:<12s} {count}")
    if stats["all"]:
        print()
        print_window_stats("all time", stats["all"], threshold)


async def main(server: str, threshold: float, interval: float, sample_every: int):
    global max_pending, total_requests, total_replies, total_lost
    global window_start, window_requests

    shutdown = asyncio.Event()
    loop = asyncio.get_running_loop()
    for sig in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(sig, shutdown.set)

    try:
        nc = await asyncio.wait_for(nats.connect(server, drain_timeout=5), timeout=5)
    except (asyncio.TimeoutError, Exception) as e:
        print(f"Failed to connect to {server}: {e}")
        return
    window_start = time.monotonic()

    print(f"Connected to {server}")
    print(f"Outlier threshold: {threshold:.0f}ms  |  stats interval: {interval:.0f}s")
    print("-" * 100)

    async def on_request(msg):
        global max_pending, total_requests, window_requests
        if msg.reply:
            pending[msg.reply] = (time.monotonic(), msg.data.decode())
            total_requests += 1
            window_requests += 1
            cur = len(pending)
            if cur > max_pending:
                max_pending = cur

    async def on_reply(msg):
        global total_replies, total_empty, total_empty_airline, total_errors
        global total_nonempty, sample_counter
        key = msg.subject
        if key in pending:
            t_start, payload = pending.pop(key)
            elapsed_ms = (time.monotonic() - t_start) * 1000
            stats["all"].append(elapsed_ms)
            stats["window"].append(elapsed_ms)
            total_replies += 1

            # Classify the reply
            reply_tag = ""
            try:
                body = json.loads(msg.data)
                if "error" in body:
                    reply_tag = "ERROR"
                    total_errors += 1
                elif not body.get("Route", {}).get("CallSign"):
                    reply_tag = "EMPTY"
                    total_empty += 1
                    if AIRLINE_CALLSIGN_RE.match(payload):
                        total_empty_airline += 1
                        empty_airline_callsigns[payload] += 1
                        window_empty_airline[payload] += 1
                else:
                    total_nonempty += 1
                    if sample_every > 0:
                        sample_counter += 1
                        if sample_counter >= sample_every:
                            sample_counter = 0
                            print(
                                f"SAMPLE {elapsed_ms:7.1f}ms  req={payload:<20s}  "
                                f"{json.dumps(body, separators=(',', ':'))}"
                            )
            except (json.JSONDecodeError, UnicodeDecodeError):
                reply_tag = "BAD_JSON"
                total_errors += 1

            if elapsed_ms > threshold:
                print(
                    f"SLOW  {elapsed_ms:8.1f}ms  req={payload:<20s}  "
                    f"reply_bytes={len(msg.data)}  pending={len(pending)}"
                    f"{'  [' + reply_tag + ']' if reply_tag else ''}"
                )
            elif reply_tag in ("ERROR", "BAD_JSON"):
                print(
                    f"ERR   {elapsed_ms:8.1f}ms  req={payload:<20s}  "
                    f"[{reply_tag}] {msg.data[:200].decode(errors='replace')}"
                )

    # Tap requests (non-queue-group so we get copies, not steal them)
    await nc.subscribe("v1.enrich.routes", cb=on_request)
    # Tap replies on inbox subjects
    await nc.subscribe("_INBOX.>", cb=on_reply)

    print("Listening for v1.enrich.routes traffic...\n")

    while not shutdown.is_set():
        try:
            await asyncio.wait_for(shutdown.wait(), timeout=interval)
            break  # shutdown was set
        except asyncio.TimeoutError:
            pass  # interval elapsed, print stats

        now = time.monotonic()

        # Message rate
        elapsed_s = now - window_start
        rate = window_requests / elapsed_s if elapsed_s > 0 else 0

        # Window stats
        print_window_stats(f"{interval:.0f}s window", stats["window"], threshold)
        print(
            f"RATE  {rate:.1f} req/s  |  "
            f"pending={len(pending)}  max_pending={max_pending}  "
            f"total: req={total_requests} reply={total_replies} "
            f"lost={total_lost} nonempty={total_nonempty} empty={total_empty} "
            f"empty_airline={total_empty_airline} err={total_errors}"
        )

        # Empty airline callsigns this window
        if window_empty_airline:
            top_w = sorted(
                window_empty_airline.items(), key=lambda x: -x[1]
            )[:10]
            cs_list = " ".join(f"{cs}({n})" for cs, n in top_w)
            print(f"EMPTY airline callsigns: {cs_list}")

        # Expire stale pending entries (no reply after 5s = lost)
        expired = [
            k for k, (t, _) in pending.items() if now - t > 5.0
        ]
        for k in expired:
            t_start, payload = pending.pop(k)
            elapsed_ms = (now - t_start) * 1000
            total_lost += 1
            print(
                f"LOST  {elapsed_ms:8.0f}ms  req={payload:<20s}  "
                f"(no reply received)"
            )

        # Reset window
        stats["window"] = []
        window_requests = 0
        window_start = now
        window_empty_airline.clear()

        print()

    # Clean shutdown
    print_summary(threshold)
    try:
        await nc.drain()
    except Exception:
        pass
    if not nc.is_closed:
        try:
            await nc.close()
        except Exception:
            pass


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Tap NATS enrichment traffic")
    parser.add_argument(
        "--server", default="nats://nats.int.plane.watch:4222", help="NATS server URL"
    )
    parser.add_argument(
        "--threshold", type=float, default=THRESHOLD_MS,
        help=f"Report requests slower than this (ms, default {THRESHOLD_MS})",
    )
    parser.add_argument(
        "--interval", type=float, default=10,
        help="Stats reporting interval in seconds (default 10)",
    )
    parser.add_argument(
        "--sample", type=int, default=0,
        help="Print every Nth non-empty reply (0 = disabled, default 0)",
    )
    args = parser.parse_args()
    try:
        asyncio.run(main(args.server, args.threshold, args.interval, args.sample))
    except KeyboardInterrupt:
        print_summary(args.threshold)