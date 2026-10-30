#!/usr/bin/env python3
"""The 36 chairs: one bot and one node each, in the team orchestra.

  chairs.py render <dir>    write the 36 definitions, from chair.json and the score service's sections
  chairs.py up [chair...]   start each chair's node (../stage/stage.sh), point its mcp.json at its
                            own URL of the score service, turn on Jev's tool in its settings.json,
                            register the bot (or update it) and enable it
  chairs.py down [chair...] stop each bot (paused, its tasks cancelled) and its node
  chairs.py list            the team as the control plane has it, by section

A chair's id is the score service's: <section>-<n> (vn1-1 … hn-2). Its node is
stage-<id>, dedicated; its MCP URL http://127.0.0.1:8790/mcp/<id>, so the score
knows who calls. Without chair names, every chair.

ORCHESTRA_RUNTIME=jev renders the chairs from chair-jev.json: Jev bots, whose
node runs the fixed program phrases → Jev → play → listen with no language
model (the node needs TYPESAFE_API_KEY in its environment file). Anything
else, or nothing, is chair.json: AXLR bots on Haiku that call the same tools.

Test: python3 -m unittest -v test_chairs
"""

import json
import os
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
CONFIG = os.path.join(HERE, "..", "score-service", "config.json")
STAGE = os.path.join(HERE, "..", "stage", "stage.sh")
SCORE_URL = os.environ.get("ORCHESTRA_SCORE_URL", "http://127.0.0.1:8790")
RUNTIME = os.environ.get("ORCHESTRA_RUNTIME", "axlr")
TEMPLATES = {"axlr": "chair.json", "jev": "chair-jev.json"}


def chairs(config):
    """Every chair as (id, section, n), in the score's order."""
    return [(f"{s['id']}-{n}", s, n) for s in config["sections"] for n in range(1, s["chairs"] + 1)]


def definition(template, section, n):
    """One chair's bot, from the template."""
    chair = f"{section['id']}-{n}"
    text = json.dumps(template)
    for key, value in (("stage-CHAIR", f"stage-{chair}"), ("CHAIR", chair), ("SECTIONKEY", section["id"]), ("SECTION", section["name"]),
                       ("TOTAL", str(section["chairs"])), ("FAMILY", section["family"]), ("chair N of", f"chair {n} of")):
        text = text.replace(key, value)
    bot = json.loads(text)
    bot["name"] = f"{section['name']} {n}"
    return bot


def load(runtime=None):
    """The score's sections and the chair template of the runtime (ORCHESTRA_RUNTIME when None)."""
    runtime = RUNTIME if runtime is None else runtime
    if runtime not in TEMPLATES:
        raise SystemExit(f"ORCHESTRA_RUNTIME must be one of {', '.join(TEMPLATES)}, not {runtime!r}")
    with open(CONFIG) as f:
        config = json.load(f)
    with open(os.path.join(HERE, TEMPLATES[runtime])) as f:
        template = json.load(f)
    return config, template


def cli(*args, check=True):
    out = subprocess.run(["axlr-bots", *args], capture_output=True, text=True)
    if check and out.returncode != 0:
        raise SystemExit(f"axlr-bots {' '.join(args)}: {out.stderr.strip() or out.stdout.strip()}")
    return out


def selected(config, names):
    every = chairs(config)
    if not names:
        return every
    known = {c[0]: c for c in every}
    missing = [n for n in names if n not in known]
    if missing:
        raise SystemExit(f"no such chair: {', '.join(missing)}")
    return [known[n] for n in names]


def up(config, template, names):
    picked = selected(config, names)
    subprocess.run([STAGE, "start", *[c[0] for c in picked]], check=True, stdout=subprocess.DEVNULL)
    for chair, section, n in picked:
        subprocess.run([STAGE, "mcp", chair, f"{SCORE_URL}/mcp/{chair}"], check=True, stdout=subprocess.DEVNULL)
        subprocess.run([STAGE, "settings", chair], check=True, stdout=subprocess.DEVNULL)
        bot = definition(template, section, n)
        with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as f:
            json.dump(bot, f)
        try:
            if cli("bot", "get", chair, check=False).returncode == 0:
                patch = {k: v for k, v in bot.items() if k not in ("id", "tenant", "name", "owner")}
                with open(f.name, "w") as g:
                    json.dump(patch, g)
                cli("bot", "update", chair, "--file", f.name)
            else:
                cli("bot", "register", "--file", f.name)
        finally:
            os.unlink(f.name)
        cli("bot", "enable", chair, check=False)
        print(f"{chair} up ({(bot.get('runtime') or {}).get('kind', 'axlr')})")


def down(config, names):
    picked = selected(config, names)
    for chair, _, _ in picked:
        cli("bot", "stop", chair, check=False)
    subprocess.run([STAGE, "stop", *[c[0] for c in picked]], check=True)


def listing(config):
    team = json.loads(cli("group", "get", "orchestra").stdout)
    members = {m["botId"]: m for m in team.get("members", [])}
    nodes = {n["id"]: n.get("status") for n in json.loads(cli("node", "list").stdout)}
    for s in config["sections"]:
        row = []
        for n in range(1, s["chairs"] + 1):
            chair = f"{s['id']}-{n}"
            m = members.get(chair)
            row.append(f"{n}:{(m or {}).get('status', '-')[:1]}{'' if nodes.get('stage-' + chair) == 'Online' else '!'}")
        enabled = sum(1 for n in range(1, s["chairs"] + 1) if (members.get(f"{s['id']}-{n}") or {}).get("status") == "Enabled")
        print(f"{s['name']:<11} {enabled:>2}/{s['chairs']:<2} " + " ".join(row))
    print("E Enabled, P Paused, R Registered, - not a member; ! its node is not Online")


def main(argv=None):
    argv = sys.argv[1:] if argv is None else argv
    if not argv:
        raise SystemExit(__doc__)
    config, template = load()
    cmd, rest = argv[0], argv[1:]
    if cmd == "render":
        os.makedirs(rest[0], exist_ok=True)
        for chair, section, n in chairs(config):
            with open(os.path.join(rest[0], f"{chair}.json"), "w") as f:
                json.dump(definition(template, section, n), f, indent=2)
        print(f"{len(chairs(config))} definitions in {rest[0]}")
    elif cmd == "up":
        up(config, template, rest)
    elif cmd == "down":
        down(config, rest)
    elif cmd == "list":
        listing(config)
    else:
        raise SystemExit(__doc__)


if __name__ == "__main__":
    main()
