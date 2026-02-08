#!/usr/bin/env python3
"""Cross-reference enrichment replies with location updates to find dropped routes.

Subscribes to both:
  - v1.enrich.routes (request/reply) to capture successful enrichments
  - location-updates-enriched to see the final output

Logs any location update that is missing RouteCode/Segments despite having
a previously-seen non-empty enrichment reply for that callsign.
"""

import argparse
import asyncio
import json
import signal
import time
from collections import defaultdict

import nats

# Track in-flight enrichment requests: reply_subject -> (time, callsign)
pending_requests = {}

# Successful enrichments: callsign -> {time, reply_body}
enriched_cache = {}

# Stats
total_enrich_requests = 0
total_enrich_replies = 0
total_enrich_nonempty = 0
total_enrich_callsign_only = 0
total_location_updates = 0
total_location_with_route = 0
total_location_missing_route = 0
total_mismatches = 0  # had enrichment but location missing route
mismatch_callsigns = defaultdict(int)
flagged_callsigns = set()  # already reported, skip future hits

CACHE_TTL = 300  # expire enrichment cache entries after 5 minutes


async def main(server: str, interval: float, verbose: bool, location_topic: str):
    global total_enrich_requests, total_enrich_replies, total_enrich_nonempty
    global total_location_updates, total_location_with_route
    global total_location_missing_route, total_mismatches

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
    print(f"Watching: v1.enrich.routes (request/reply) + {location_topic}")
    print(f"Cache TTL: {CACHE_TTL}s  |  Stats interval: {interval:.0f}s")
    print("-" * 100)

    # --- Enrichment request/reply tracking ---

    async def on_enrich_request(msg):
        global total_enrich_requests
        if msg.reply:
            callsign = msg.data.decode(errors="replace").strip()
            pending_requests[msg.reply] = (time.monotonic(), callsign)
            total_enrich_requests += 1

    async def on_enrich_reply(msg):
        global total_enrich_replies, total_enrich_nonempty, total_enrich_callsign_only
        key = msg.subject
        if key not in pending_requests:
            return
        t_start, callsign = pending_requests.pop(key)
        total_enrich_replies += 1

        try:
            body = json.loads(msg.data)
        except (json.JSONDecodeError, UnicodeDecodeError):
            return

        if "error" in body:
            return

        route = body.get("Route", {})
        route_code = route.get("Code", "")
        segments = route.get("Segments") or []

        # Only cache if the enrichment actually returned route data
        if not route_code and not segments:
            return

        # Non-empty enrichment — cache it
        total_enrich_nonempty += 1
        enriched_cache[callsign] = {
            "time": time.monotonic(),
            "route_code": route_code,
            "segments": segments,
            "body": body,
        }

    # --- Location update checking ---

    async def on_location_update(msg):
        global total_location_updates, total_location_with_route
        global total_location_missing_route, total_mismatches

        total_location_updates += 1

        try:
            loc = json.loads(msg.data)
        except (json.JSONDecodeError, UnicodeDecodeError):
            return

        callsign = loc.get("CallSign", "")
        if not callsign:
            return

        has_route = bool(loc.get("RouteCode")) or bool(loc.get("Segments"))

        if has_route:
            total_location_with_route += 1
            return

        total_location_missing_route += 1

        # Check if we had a successful enrichment for this callsign
        cached = enriched_cache.get(callsign)
        if not cached:
            return

        # Mismatch: enrichment returned route data, but location update has none
        if callsign in flagged_callsigns:
            return
        flagged_callsigns.add(callsign)
        total_mismatches += 1
        mismatch_callsigns[callsign] += 1
        age = time.monotonic() - cached["time"]
        print(
            f"MISMATCH  icao={loc.get('Icao', '?'):<8s}  "
            f"callsign={callsign:<12s}  "
            f"enrich_age={age:.1f}s"
        )
        print(f"  enrich: {json.dumps(cached['body'], separators=(',', ':'))}")
        print(f"  location: {msg.data.decode(errors='replace')}")

    # Subscribe
    await nc.subscribe("v1.enrich.routes", cb=on_enrich_request)
    await nc.subscribe("_INBOX.>", cb=on_enrich_reply)
    await nc.subscribe(location_topic, cb=on_location_update)

    print("Listening...\n")

    while not shutdown.is_set():
        try:
            await asyncio.wait_for(shutdown.wait(), timeout=interval)
            break
        except asyncio.TimeoutError:
            pass

        now = time.monotonic()

        # Expire old cache entries
        expired = [k for k, v in enriched_cache.items() if now - v["time"] > CACHE_TTL]
        for k in expired:
            del enriched_cache[k]

        # Expire stale pending requests
        stale = [k for k, (t, _) in pending_requests.items() if now - t > 5.0]
        for k in stale:
            del pending_requests[k]

        print(
            f"STATS  "
            f"enrich: req={total_enrich_requests} reply={total_enrich_replies} "
            f"nonempty={total_enrich_nonempty} cached={len(enriched_cache)}  |  "
            f"location: total={total_location_updates} "
            f"with_route={total_location_with_route} "
            f"missing_route={total_location_missing_route}  |  "
            f"MISMATCHES={total_mismatches}"
        )

        if mismatch_callsigns:
            top = sorted(mismatch_callsigns.items(), key=lambda x: -x[1])[:15]
            cs_list = " ".join(f"{cs}({n})" for cs, n in top)
            print(f"  top mismatches: {cs_list}")

#         print()

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
        f"  enrich requests:       {total_enrich_requests}\n"
        f"  enrich replies:        {total_enrich_replies}\n"
        f"  enrich non-empty:      {total_enrich_nonempty}\n"
        f"  location updates:      {total_location_updates}\n"
        f"  location with route:   {total_location_with_route}\n"
        f"  location missing route:{total_location_missing_route}\n"
        f"  MISMATCHES:            {total_mismatches}"
    )
    if mismatch_callsigns:
        top = sorted(mismatch_callsigns.items(), key=lambda x: -x[1])[:25]
        print(f"\n  Top mismatch callsigns:")
        for cs, count in top:
            print(f"    {cs:<12s} {count}")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(
        description="Compare enrichment replies with location updates to find dropped routes"
    )
    parser.add_argument(
        "--server", default="nats://nats.int.plane.watch:4222", help="NATS server URL"
    )
    parser.add_argument(
        "--interval", type=float, default=10,
        help="Stats reporting interval in seconds (default 10)",
    )
    parser.add_argument(
        "--verbose", action="store_true",
        help="Print full JSON for mismatches",
    )
    parser.add_argument(
        "--topic", default="location-updates-enriched",
        help="NATS topic for location updates (default: location-updates-enriched)",
    )
    args = parser.parse_args()
    try:
        asyncio.run(main(args.server, args.interval, args.verbose, args.topic))
    except KeyboardInterrupt:
        print_summary()