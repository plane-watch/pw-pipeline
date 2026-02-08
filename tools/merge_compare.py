#!/usr/bin/env python3
"""Compare location-updates-enriched vs location-updates-enriched-merged.

Detects cases where a location update has route data before pw_router's merge
step but loses it afterward. Helps pinpoint if routes are being dropped during
the merge.
"""

import argparse
import asyncio
import json
import signal
import time
from collections import defaultdict

import nats

# icao -> {time, callsign, route_code, segments, raw}
seen_with_route = {}

# Stats
total_pre = 0
total_pre_with_route = 0
total_post = 0
total_post_with_route = 0
total_post_missing_route = 0
total_mismatches = 0
mismatch_callsigns = defaultdict(int)
flagged = set()  # (icao, callsign) already reported

CACHE_TTL = 300


async def main(server: str, interval: float, pre_topic: str, post_topic: str):
    global total_pre, total_pre_with_route
    global total_post, total_post_with_route, total_post_missing_route
    global total_mismatches

    shutdown = asyncio.Event()
    loop = asyncio.get_running_loop()
    for sig in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(sig, shutdown.set)

    try:
        nc = await asyncio.wait_for(nats.connect(server, drain_timeout=5), timeout=5)
    except (asyncio.TimeoutError, Exception) as e:
        print(f"Failed to connect to {server}: {e}")
        return

    print(f"Connected to {server}")
    print(f"Pre-merge:  {pre_topic}")
    print(f"Post-merge: {post_topic}")
    print(f"Cache TTL: {CACHE_TTL}s  |  Stats interval: {interval:.0f}s")
    print("-" * 100)

    async def on_pre(msg):
        global total_pre, total_pre_with_route
        total_pre += 1

        try:
            loc = json.loads(msg.data)
        except (json.JSONDecodeError, UnicodeDecodeError):
            return

        icao = loc.get("Icao", "")
        if not icao:
            return

        route_code = loc.get("RouteCode") or ""
        segments = loc.get("Segments") or []
        if not route_code and not segments:
            return

        total_pre_with_route += 1
        seen_with_route[icao] = {
            "time": time.monotonic(),
            "callsign": loc.get("CallSign", ""),
            "route_code": route_code,
            "segments": segments,
            "raw": msg.data.decode(errors="replace"),
        }

    async def on_post(msg):
        global total_post, total_post_with_route, total_post_missing_route
        global total_mismatches

        total_post += 1

        try:
            loc = json.loads(msg.data)
        except (json.JSONDecodeError, UnicodeDecodeError):
            return

        icao = loc.get("Icao", "")
        if not icao:
            return

        route_code = loc.get("RouteCode") or ""
        segments = loc.get("Segments") or []
        has_route = bool(route_code) or bool(segments)

        if has_route:
            total_post_with_route += 1
            return

        total_post_missing_route += 1

        cached = seen_with_route.get(icao)
        if not cached:
            return

        callsign = loc.get("CallSign", "") or cached["callsign"]
        key = (icao, callsign)
        if key in flagged:
            return
        flagged.add(key)

        total_mismatches += 1
        mismatch_callsigns[callsign] += 1
        age = time.monotonic() - cached["time"]
        print(
            f"DROPPED  icao={icao:<8s}  callsign={callsign:<12s}  "
            f"age={age:.1f}s"
        )
        print(f"  pre:  {cached['raw']}")
        print(f"  post: {msg.data.decode(errors='replace')}")

    await nc.subscribe(pre_topic, cb=on_pre)
    await nc.subscribe(post_topic, cb=on_post)

    print("Listening...\n")

    while not shutdown.is_set():
        try:
            await asyncio.wait_for(shutdown.wait(), timeout=interval)
            break
        except asyncio.TimeoutError:
            pass

        now = time.monotonic()

        # Expire old cache entries
        expired = [k for k, v in seen_with_route.items() if now - v["time"] > CACHE_TTL]
        for k in expired:
            del seen_with_route[k]

        print(
            f"STATS  "
            f"pre: total={total_pre} with_route={total_pre_with_route} "
            f"cached={len(seen_with_route)}  |  "
            f"post: total={total_post} with_route={total_post_with_route} "
            f"missing={total_post_missing_route}  |  "
            f"DROPPED={total_mismatches}"
        )

        if mismatch_callsigns:
            top = sorted(mismatch_callsigns.items(), key=lambda x: -x[1])[:15]
            cs_list = " ".join(f"{cs}({n})" for cs, n in top)
            print(f"  top dropped: {cs_list}")

        print()

    print_summary()

    try:
        await nc.drain()
    except Exception:
        pass
    if not nc.is_closed:
        try:
            await nc.close()
        except Exception:
            pass


def print_summary():
    print(f"\n{'=' * 100}")
    print("Session summary")
    print(f"{'=' * 100}")
    print(
        f"  pre-merge total:       {total_pre}\n"
        f"  pre-merge with route:  {total_pre_with_route}\n"
        f"  post-merge total:      {total_post}\n"
        f"  post-merge with route: {total_post_with_route}\n"
        f"  post-merge missing:    {total_post_missing_route}\n"
        f"  DROPPED:               {total_mismatches}"
    )
    if mismatch_callsigns:
        top = sorted(mismatch_callsigns.items(), key=lambda x: -x[1])[:25]
        print(f"\n  Top dropped callsigns:")
        for cs, count in top:
            print(f"    {cs:<12s} {count}")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(
        description="Compare pre/post merge location updates to find dropped routes"
    )
    parser.add_argument(
        "--server", default="nats://nats.int.plane.watch:4222", help="NATS server URL"
    )
    parser.add_argument(
        "--interval", type=float, default=10,
        help="Stats reporting interval in seconds (default 10)",
    )
    parser.add_argument(
        "--pre-topic", default="location-updates-enriched",
        help="Pre-merge topic (default: location-updates-enriched)",
    )
    parser.add_argument(
        "--post-topic", default="location-updates-enriched-merged",
        help="Post-merge topic (default: location-updates-enriched-merged)",
    )
    args = parser.parse_args()
    try:
        asyncio.run(main(args.server, args.interval, args.pre_topic, args.post_topic))
    except KeyboardInterrupt:
        print_summary()