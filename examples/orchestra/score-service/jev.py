"""Jev, TypeSafe's judgement model, called by the score service itself.

The bots ask Jev through axlr_judge, from their own runs. The conductor's
hands have no bot behind them, so the service asks Jev directly, the way
AXLR's own client does (adapters/typesafe in AXLR): one request, a state and
typed questions, a pinned model, a bounded answer. Only choice questions are
used here. The key is read from TYPESAFE_API_KEY or from an env file on each
call and never appears in a log, an answer or an error.
"""

import json
import os
import time
import urllib.error
import urllib.request

ENDPOINT = "https://api.typesafe.ai/v1/systemone"
MAX_BODY = 256 << 10


class JevError(Exception):
    """Jev did not answer, or answered something that is not an answer to what was asked."""


class Jev:
    def __init__(self, model="jev-1.13.0", key_file="~/.config/axlr-bots/env", timeout=10.0, endpoint=ENDPOINT, opener=None):
        if not model or model.endswith("-latest"):
            raise ValueError("jev model must be a pinned version such as jev-1.13.0")
        self.model, self.key_file, self.timeout, self.endpoint = model, os.path.expanduser(key_file), timeout, endpoint
        self.opener = opener or urllib.request.urlopen

    def key(self):
        key = os.environ.get("TYPESAFE_API_KEY", "").strip()
        if not key and os.path.exists(self.key_file):
            with open(self.key_file) as f:
                for line in f:
                    if line.startswith("TYPESAFE_API_KEY="):
                        key = line.split("=", 1)[1].strip().strip("'\"")
        if not key or any(ch in key for ch in "\r\n"):
            raise JevError("no TYPESAFE_API_KEY for Jev")
        return key

    def choose(self, state, questions):
        """questions: {key: (instructions, [options])}. Returns {"model", "ms", "answers": {key: {choice, probabilities, confidence}}, "inputTokens"}."""
        body = json.dumps({"model": self.model, "state": state, "questions": {
            key: {"type": "choice", "instructions": instructions, "criteria": {o: None for o in options}}
            for key, (instructions, options) in questions.items()}}).encode()
        if len(body) > MAX_BODY:
            raise JevError("the request for Jev is too large")
        request = urllib.request.Request(self.endpoint, data=body, method="POST")
        request.add_header("Authorization", "Bearer " + self.key())
        request.add_header("Content-Type", "application/json")
        started = time.time()
        try:
            with self.opener(request, timeout=self.timeout) as response:
                data = response.read(MAX_BODY + 1)
        except urllib.error.HTTPError as exc:
            raise JevError(f"TypeSafe HTTP {exc.code}") from None
        except (urllib.error.URLError, OSError, TimeoutError):
            raise JevError("TypeSafe did not answer") from None
        ms = round((time.time() - started) * 1000)
        if len(data) > MAX_BODY:
            raise JevError("the answer from TypeSafe is too large")
        try:
            wire = json.loads(data)
        except ValueError:
            raise JevError("the answer from TypeSafe is not JSON") from None
        if wire.get("model") != self.model:
            raise JevError("TypeSafe answered with another model than the one asked")
        answers = wire.get("answers") or {}
        if set(answers) != set(questions):
            raise JevError("TypeSafe's answers do not match the questions sent")
        out = {}
        for key, (_, options) in questions.items():
            a = answers[key]
            probabilities = a.get("probabilities") or {}
            if a.get("type") != "choice" or a.get("choice") not in options or set(probabilities) != set(options) \
                    or not all(isinstance(p, (int, float)) and 0 <= p <= 1 for p in probabilities.values()):
                raise JevError(f"TypeSafe's answer to {key} is not a choice among the options")
            out[key] = {"choice": a["choice"], "probabilities": {o: float(probabilities[o]) for o in options},
                        "confidence": a.get("confidence")}
        return {"model": wire["model"], "ms": ms, "answers": out, "inputTokens": (wire.get("usage") or {}).get("input_tokens")}
