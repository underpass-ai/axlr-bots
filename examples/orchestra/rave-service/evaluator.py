"""The pattern evaluator: Strudel in a Node process, one batch of patterns at a time.

A bot writes a pattern as text; before it sounds, the service hands it to
strudel/evaluate.mjs, which transpiles it as the Strudel REPL does, runs it in
a vm context that holds Strudel and nothing else, and queries the cycles asked
for (a cycle is one bar). What comes back is the events with an onset in
those cycles, as Strudel built them: the sounds, the notes (as MIDI numbers
too), the controls. The service then checks the sounds exist and the density
is sane. A pattern that does not parse, that is not a pattern, that names
what a pattern never needs, or that takes too long is refused with the
reason, and the bot reads it as the tool's error. The process is one-shot:
a runaway pattern dies with it, never with the music.
"""

import json
import os
import subprocess

HERE = os.path.dirname(os.path.abspath(__file__))
STRUDEL = os.path.join(HERE, "strudel")


class Evaluator:
    def __init__(self, node="node", timeout=8.0, cwd=STRUDEL, max_old_space_mb=512):
        self.node, self.timeout, self.cwd = node, timeout, cwd
        self.argv = [node, f"--max-old-space-size={max_old_space_mb}", "--import", "./register.mjs", "evaluate.mjs"]

    def evaluate(self, requests):
        """One result per request, in order: {id, ok, haps, sounds, perCycle} or {id, ok: False, error}."""
        if not requests:
            return []
        try:
            done = subprocess.run(self.argv, cwd=self.cwd, input=json.dumps({"requests": requests}).encode(),
                                  capture_output=True, timeout=self.timeout)
        except subprocess.TimeoutExpired:
            return [{"id": r.get("id"), "ok": False, "error": f"the evaluator took longer than {self.timeout:g} s"} for r in requests]
        except FileNotFoundError:
            return [{"id": r.get("id"), "ok": False, "error": f"no {self.node}: the evaluator needs Node"} for r in requests]
        if done.returncode != 0:
            why = (done.stderr or b"").decode(errors="replace").strip().splitlines()
            why = why[-1] if why else f"exit {done.returncode}"
            return [{"id": r.get("id"), "ok": False, "error": f"the evaluator died: {why[:200]}"} for r in requests]
        try:
            results = json.loads(done.stdout.decode())["results"]
        except (ValueError, KeyError):
            return [{"id": r.get("id"), "ok": False, "error": "the evaluator answered nothing readable"} for r in requests]
        return results

    def pattern(self, code, from_cycle, cycles, max_haps=None):
        """One pattern over [from_cycle, from_cycle + cycles)."""
        request = {"id": "p", "code": code, "from": from_cycle, "to": from_cycle + cycles}
        if max_haps is not None:
            request["maxHaps"] = max_haps
        return self.evaluate([request])[0]
