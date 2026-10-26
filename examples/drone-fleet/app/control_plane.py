"""The fleet base's client of the control plane's operator API.

The base fires the dispatcher's and the logistics bot's webhooks when the
sea changes (an incident, new sightings, a station running low), reads the
tasks an incident started to add up what their models cost, and relays a
person's yes or no from the phone page. It talks HTTPS with the fleet's CA
and the operator token, both from a Secret mounted read-only; the token is
read from its file on each call and never logged.
"""

import json
import logging
import ssl
import urllib.error
import urllib.parse
import urllib.request

log = logging.getLogger("control-plane")


class APIError(Exception):
    def __init__(self, status, message):
        super().__init__(f"{status}: {message}")
        self.status = status
        self.message = message


class ControlPlane:
    def __init__(self, url, ca_file, token_file, webhook_secret_file=""):
        self.url = url.rstrip("/")
        self.ca_file = ca_file
        self.token_file = token_file
        self.webhook_secret_file = webhook_secret_file
        self._ctx = None

    @property
    def enabled(self):
        return bool(self.url and self.token_file)

    def _context(self):
        if self._ctx is None:
            self._ctx = ssl.create_default_context(cafile=self.ca_file or None)
        return self._ctx

    @staticmethod
    def _read(path):
        with open(path) as f:
            return f.read().strip()

    def _call(self, method, path, body=None, headers=None, timeout=10, raw=False):
        data = None if body is None else (body if isinstance(body, bytes) else json.dumps(body).encode())
        request = urllib.request.Request(self.url + path, data=data, method=method)
        if data is not None:
            request.add_header("Content-Type", "application/json")
        for k, v in (headers or {}).items():
            request.add_header(k, v)
        try:
            with urllib.request.urlopen(request, timeout=timeout, context=self._context()) as r:
                payload = r.read()
                return (payload, dict(r.headers)) if raw else (json.loads(payload) if payload else None)
        except urllib.error.HTTPError as exc:
            try:
                message = json.loads(exc.read()).get("error", exc.reason)
            except Exception:
                message = exc.reason
            raise APIError(exc.code, message) from None

    def _auth(self):
        return {"Authorization": "Bearer " + self._read(self.token_file)}

    def get(self, path, **kw):
        return self._call("GET", path, headers=self._auth(), **kw)

    def post(self, path, body=None):
        return self._call("POST", path, body=body if body is not None else {}, headers=self._auth())

    def fire(self, bot, trigger, event, key):
        """Fire a webhook trigger with its secret; the answer names the task."""
        headers = {"X-Axlr-Secret": self._read(self.webhook_secret_file), "Idempotency-Key": key}
        return self._call("POST", f"/v1/triggers/{urllib.parse.quote(bot)}/{urllib.parse.quote(trigger)}", body=event, headers=headers)

    def tasks_since(self, since_iso, limit=200):
        return self.get(f"/v1/tasks?since={urllib.parse.quote(since_iso)}&limit={limit}") or []

    def waiting(self):
        return self.get("/v1/tasks?status=WaitingApproval&limit=50") or []

    def task(self, task_id):
        return self.get(f"/v1/tasks/{urllib.parse.quote(task_id)}")

    def executions(self, task_id):
        return self.get(f"/v1/tasks/{urllib.parse.quote(task_id)}/executions") or []

    def execution(self, exec_id):
        return self.get(f"/v1/executions/{urllib.parse.quote(exec_id)}")

    def artifact(self, exec_id, name):
        return self.get(f"/v1/executions/{urllib.parse.quote(exec_id)}/artifacts/{urllib.parse.quote(name)}", raw=True)

    def decide(self, task_id, decision, call_id):
        if decision not in ("approve", "deny"):
            raise ValueError("decision must be approve or deny")
        query = "?call=" + urllib.parse.quote(call_id) if call_id else ""
        return self.post(f"/v1/tasks/{urllib.parse.quote(task_id)}/{decision}{query}")
