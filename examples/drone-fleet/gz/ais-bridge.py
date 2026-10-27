#!/usr/bin/env python3
"""Put the AIS ships and the incident's containers into the gz world.

    ais-bridge.py --fleet http://fleet-base:8080/api/fleet [--idle]

Not written yet (PR D): with --idle, or until then, it only waits.
"""

import argparse
import time


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--fleet", required=True)
    ap.add_argument("--idle", action="store_true")
    ap.parse_args()
    print("ais-bridge: idle", flush=True)
    while True:
        time.sleep(3600)


if __name__ == "__main__":
    main()
