"""A small Prometheus registry, standard library only.

Counters, gauges and histograms with labels, and families computed when
Prometheus asks (a callback: the take's bar, the chairs per section), in the
text exposition format 0.0.4 that every Prometheus scrapes. The service
needs nothing else, so it does not take prometheus_client as a dependency.

  REGISTRY = Registry()
  calls = REGISTRY.counter("x_calls_total", "Calls.", ("tool",))
  calls.labels(tool="play").inc()
  REGISTRY.render()    # the body of GET /metrics

A label value is any text (escaped as the format asks); keep the values to a
bounded set, as Prometheus keeps one series per combination.
"""

import math
import os
import re
import resource
import threading
import time

CONTENT_TYPE = "text/plain; version=0.0.4; charset=utf-8"
# Seconds, from a millisecond to half a minute: an HTTP round trip, a Jev
# reading, a chair's listen (it waits up to 20 s).
SECONDS = (0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.15, 0.2, 0.25, 0.3, 0.4, 0.5, 0.75, 1, 1.5, 2, 2.5, 5, 10, 20, 30)


_NUMBER = re.compile(r"[-+]?\d+(?:[.,]\d+)?")


def reason(text, limit=80):
    """A label value from free text (a reason, an error, a why): its numbers as N, at most limit characters,
    so "budget_exceeded: 128 tool calls" and "…: 129 tool calls" are one series."""
    return _NUMBER.sub("N", " ".join(str(text or "").split()))[:limit] or "none"


def number(value):
    if value is None or (isinstance(value, float) and math.isnan(value)):
        return "NaN"
    if value == math.inf:
        return "+Inf"
    if value == -math.inf:
        return "-Inf"
    if isinstance(value, bool):
        return "1" if value else "0"
    if isinstance(value, int) or float(value).is_integer():
        return str(int(value))
    return repr(float(value))


def escape_label(value):
    return str(value).replace("\\", "\\\\").replace("\n", "\\n").replace('"', '\\"')


def escape_help(text):
    return str(text).replace("\\", "\\\\").replace("\n", "\\n")


def label_text(names, values, extra=()):
    pairs = list(zip(names, values)) + list(extra)
    if not pairs:
        return ""
    return "{" + ",".join(f'{k}="{escape_label(v)}"' for k, v in pairs) + "}"


class Family:
    """One metric name, its help, its label names and one child per label values."""

    kind = "untyped"

    def __init__(self, name, help_text, labels=()):
        if not name or not all(c.isalnum() or c in "_:" for c in name) or name[0].isdigit():
            raise ValueError(f"not a metric name: {name!r}")
        self.name, self.help, self.label_names = name, help_text, tuple(labels)
        self.lock, self.children = threading.Lock(), {}

    def labels(self, *values, **named):
        if named:
            if values or set(named) != set(self.label_names):
                raise ValueError(f"{self.name} takes the labels {self.label_names}")
            values = tuple(named[k] for k in self.label_names)
        elif len(values) != len(self.label_names):
            raise ValueError(f"{self.name} takes the labels {self.label_names}")
        key = tuple("" if v is None else str(v) for v in values)
        with self.lock:
            child = self.children.get(key)
            if child is None:
                child = self.children[key] = self.child()
            return child

    def _only(self):
        if self.label_names:
            raise ValueError(f"{self.name} has labels {self.label_names}: use .labels()")
        return self.labels()

    def clear(self):
        with self.lock:
            self.children.clear()

    def samples(self):
        """[(suffix, label values, extra label pairs, value)], sorted by label values."""
        with self.lock:
            items = sorted(self.children.items())
        out = []
        for key, child in items:
            out += [(suffix, key, extra, value) for suffix, extra, value in child.samples()]
        return out

    def render(self):
        lines = [f"# HELP {self.name} {escape_help(self.help)}", f"# TYPE {self.name} {self.kind}"]
        for suffix, key, extra, value in self.samples():
            lines.append(f"{self.name}{suffix}{label_text(self.label_names, key, extra)} {number(value)}")
        return lines


class _Value:
    def __init__(self):
        self.value, self.lock = 0.0, threading.Lock()

    def inc(self, amount=1):
        with self.lock:
            self.value += amount

    def set(self, value):
        with self.lock:
            self.value = float(value)

    def dec(self, amount=1):
        with self.lock:
            self.value -= amount

    def get(self):
        with self.lock:
            return self.value

    def samples(self):
        return [("", (), self.get())]


class _CounterValue(_Value):
    def inc(self, amount=1):
        if amount < 0:
            raise ValueError("a counter only goes up")
        super().inc(amount)

    def set(self, value):
        raise ValueError("a counter only goes up: inc it")

    dec = set


class Counter(Family):
    kind = "counter"

    def __init__(self, name, help_text, labels=()):
        if not name.endswith("_total"):
            raise ValueError(f"a counter's name ends with _total: {name}")
        super().__init__(name, help_text, labels)

    def child(self):
        return _CounterValue()

    def inc(self, amount=1):
        self._only().inc(amount)

    def get(self, *values, **named):
        return self.labels(*values, **named).get()


class Gauge(Family):
    kind = "gauge"

    def child(self):
        return _Value()

    def set(self, value):
        self._only().set(value)

    def inc(self, amount=1):
        self._only().inc(amount)

    def dec(self, amount=1):
        self._only().dec(amount)

    def get(self, *values, **named):
        return self.labels(*values, **named).get()


class _Buckets:
    def __init__(self, bounds):
        self.bounds, self.counts, self.sum, self.count = bounds, [0] * len(bounds), 0.0, 0
        self.lock = threading.Lock()

    def observe(self, value):
        value = float(value)
        with self.lock:
            self.sum += value
            self.count += 1
            for i, bound in enumerate(self.bounds):
                if value <= bound:
                    self.counts[i] += 1
                    break

    def samples(self):
        with self.lock:
            counts, total, count = list(self.counts), self.sum, self.count
        out, running = [], 0
        for bound, n in zip(self.bounds, counts):
            running += n
            out.append(("_bucket", (("le", number(bound)),), running))
        out.append(("_bucket", (("le", "+Inf"),), count))
        out.append(("_sum", (), total))
        out.append(("_count", (), count))
        return out


class Histogram(Family):
    kind = "histogram"

    def __init__(self, name, help_text, labels=(), buckets=SECONDS):
        if "le" in labels:
            raise ValueError("le is the histogram's own label")
        super().__init__(name, help_text, labels)
        self.bounds = tuple(sorted(float(b) for b in buckets if b != math.inf))

    def child(self):
        return _Buckets(self.bounds)

    def observe(self, value):
        self._only().observe(value)


class Computed(Family):
    """A family whose samples a function gives when Prometheus asks: [(label values, value)]."""

    def __init__(self, name, help_text, labels, kind, fn):
        super().__init__(name, help_text, labels)
        self.kind, self.fn = kind, fn

    def samples(self):
        out = []
        for values, value in self.fn() or ():
            values = tuple("" if v is None else str(v) for v in values)
            if len(values) != len(self.label_names):
                raise ValueError(f"{self.name} takes the labels {self.label_names}")
            out.append(("", values, (), value))
        return sorted(out, key=lambda s: s[1])


class Registry:
    def __init__(self):
        self.lock, self.families = threading.Lock(), {}

    def _add(self, family):
        with self.lock:
            if family.name in self.families:
                raise ValueError(f"{family.name} is registered already")
            self.families[family.name] = family
        if not family.label_names and not isinstance(family, Computed):
            family.labels()  # a family without labels is 0 from the start, not absent until its first event
        return family

    def counter(self, name, help_text, labels=()):
        return self._add(Counter(name, help_text, labels))

    def gauge(self, name, help_text, labels=()):
        return self._add(Gauge(name, help_text, labels))

    def histogram(self, name, help_text, labels=(), buckets=SECONDS):
        return self._add(Histogram(name, help_text, labels, buckets))

    def computed(self, name, help_text, labels, fn, kind="gauge"):
        """fn() → [(label values, value)], called on each scrape; an error there leaves the family empty."""
        return self._add(Computed(name, help_text, labels, kind, fn))

    def names(self):
        """Every sample name the registry can expose, histograms with their _bucket, _sum and _count."""
        with self.lock:
            families = list(self.families.values())
        out = set()
        for f in families:
            out.add(f.name)
            if f.kind == "histogram":
                out |= {f.name + "_bucket", f.name + "_sum", f.name + "_count"}
        return out

    def render(self):
        with self.lock:
            families = sorted(self.families.values(), key=lambda f: f.name)
        lines = []
        for f in families:
            try:
                lines += f.render()
            except Exception as exc:  # noqa: BLE001  one family failing never hides the others
                lines += [f"# HELP {f.name} {escape_help(f.help)}", f"# TYPE {f.name} {f.kind}",
                          f"# {f.name} not collected: {escape_help(type(exc).__name__)}"]
        return ("\n".join(lines) + "\n").encode()


def process_metrics(registry, started=None):
    """The process's own: start time, CPU seconds, resident memory, threads (Prometheus' usual names)."""
    started = time.time() if started is None else started
    page = os.sysconf("SC_PAGE_SIZE") if hasattr(os, "sysconf") else 4096

    def cpu():
        r = resource.getrusage(resource.RUSAGE_SELF)
        return [((), r.ru_utime + r.ru_stime)]

    def rss():
        try:
            with open("/proc/self/statm") as f:
                return [((), int(f.read().split()[1]) * page)]
        except OSError:
            return [((), resource.getrusage(resource.RUSAGE_SELF).ru_maxrss * 1024)]

    registry.computed("process_start_time_seconds", "When the process started, in seconds since the epoch.", (), lambda: [((), started)])
    registry.computed("process_cpu_seconds_total", "User and system CPU time the process spent, in seconds.", (), cpu, kind="counter")
    registry.computed("process_resident_memory_bytes", "Resident memory of the process, in bytes.", (), rss)
    registry.computed("process_threads", "Python threads alive in the process.", (), lambda: [((), threading.active_count())])
