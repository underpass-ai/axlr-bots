#!/usr/bin/env python3
"""The orchestra's three Grafana dashboards, written as JSON for Grafana's file provisioning.

  Conductor   what the hands did: readings per gesture, Jev's latency, levels
              pending, kept and reverted, the sections' gains, the expression
  Orchestra   the take: bar, chairs playing per section, gains, characters,
              cues, late phrases, and the chairs' tasks from the control plane
  Pipeline    the path of a gesture: hands → publish → echo → delivery, the bus
              to the sound, Jev's cost, the WebSocket, the render loop

Run it after changing a panel; the JSON in grafana/dashboards is what Grafana
reads, and the score service's tests check that every metric a panel asks
for is one the service or the team exporter exposes, and that the files are
this script's output:

  python3 dashboards.py            # writes grafana/dashboards/*.json
  python3 dashboards.py --check    # exits 1 if a file is not up to date
"""

import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.join(HERE, "grafana", "dashboards")
DS = {"type": "prometheus", "uid": "prometheus"}

# The house palette (docs/brand.md): copper, gold, sand, cream, brown, with
# brass and verdigris (copper's patina) for the sections that need a colour more.
COPPER, GOLD, SAND, CREAM, BROWN, BRASS, PATINA, RED, GREY = (
    "#B85C38", "#D97B2D", "#C9B08C", "#F1E6D2", "#9C7A57", "#E8C25A", "#5FA8A0", "#D9534F", "#8E8E8E")
SECTIONS = {"vn1": COPPER, "vn2": GOLD, "va": SAND, "vc": CREAM, "cb": BROWN, "ob": PATINA, "hn": BRASS}
CHARACTERS = {"dolce": SAND, "cantabile": GOLD, "brillante": BRASS, "appassionato": COPPER, "plain": GREY, "nothing": GREY}
LEVEL_KINDS = {"pending": GOLD, "kept": PATINA, "reverted": RED}

J = 'job="$job"'
TEAM = 'team="$team"'


class Board:
    """Panels laid out left to right, row by row, on Grafana's 24-column grid."""

    def __init__(self):
        self.panels, self.x, self.y, self.row_h, self.next_id = [], 0, 0, 0, 1

    def row(self, title):
        self.newline()
        self.panels.append({"type": "row", "title": title, "id": self._id(), "collapsed": False, "panels": [],
                            "gridPos": {"x": 0, "y": self.y, "w": 24, "h": 1}})
        self.y += 1

    def newline(self):
        if self.x:
            self.x, self.y, self.row_h = 0, self.y + self.row_h, 0

    def add(self, panel, w, h):
        if self.x + w > 24:
            self.newline()
        panel["id"] = self._id()
        panel["gridPos"] = {"x": self.x, "y": self.y, "w": w, "h": h}
        self.panels.append(panel)
        self.x += w
        self.row_h = max(self.row_h, h)
        return panel

    def _id(self):
        self.next_id += 1
        return self.next_id - 1


def target(expr, legend="", instant=False, ref=0):
    return {"refId": chr(65 + ref), "datasource": DS, "expr": expr, "legendFormat": legend or "__auto",
            "instant": instant, "range": not instant, "editorMode": "code"}


def targets(queries, instant=False):
    out = []
    for i, q in enumerate(queries):
        expr, legend = (q, "") if isinstance(q, str) else q
        out.append(target(expr, legend, instant, i))
    return out


def colors(by):
    """Overrides: a series whose name starts with a key takes its colour."""
    return [{"matcher": {"id": "byRegexp", "options": f"^{k}(\\b|$| ).*"}, "properties": [{"id": "color", "value": {"mode": "fixed", "fixedColor": c}}]}
            for k, c in by.items()]


def timeseries(title, queries=(), unit="short", description="", stack=False, bars=False, minimum=None, maximum=None, overrides=None,
               fill=12, decimals=None, step=False):
    """step: a state sampled every scrape (chairs, gains), drawn as it is held, not smoothed between samples."""
    custom = {"drawStyle": "bars" if bars else "line", "lineWidth": 2, "fillOpacity": 70 if bars else fill, "showPoints": "never",
              "spanNulls": True, "lineInterpolation": "stepAfter" if step else "smooth" if not bars else "linear", "gradientMode": "opacity",
              "stacking": {"mode": "normal" if stack else "none", "group": "A"}, "axisSoftMin": 0, "barAlignment": 0}
    defaults = {"unit": unit, "color": {"mode": "palette-classic"}, "custom": custom}
    if minimum is not None:
        defaults["min"] = minimum
    if maximum is not None:
        defaults["max"] = maximum
    if decimals is not None:
        defaults["decimals"] = decimals
    return {"type": "timeseries", "title": title, "description": description, "datasource": DS, "targets": targets(queries),
            "fieldConfig": {"defaults": defaults, "overrides": overrides or []},
            "options": {"legend": {"displayMode": "list", "placement": "bottom", "showLegend": True},
                        "tooltip": {"mode": "multi", "sort": "desc"}}}


def stat(title, queries, unit="short", description="", color=COPPER, text="value", mappings=None, decimals=None, graph=False, thresholds=None,
         overrides=None, size=None):
    defaults = {"unit": unit, "color": {"mode": "fixed", "fixedColor": color} if not thresholds else {"mode": "thresholds"},
                "mappings": mappings or []}
    if thresholds:
        defaults["thresholds"] = {"mode": "absolute", "steps": [{"color": c, "value": v} for v, c in thresholds]}
    if decimals is not None:
        defaults["decimals"] = decimals
    return {"type": "stat", "title": title, "description": description, "datasource": DS, "targets": targets(queries, instant=not graph),
            "fieldConfig": {"defaults": defaults, "overrides": overrides or []},
            "options": {"reduceOptions": {"calcs": ["lastNotNull"], "fields": "", "values": False}, "textMode": text,
                        **({"text": {"titleSize": 12, "valueSize": size}} if size else {}),
                        "colorMode": "value", "graphMode": "area" if graph else "none", "justifyMode": "center", "orientation": "auto",
                        "showPercentChange": False, "wideLayout": True}}


def bargauge(title, queries, unit="short", description="", maximum=None, overrides=None, color=COPPER, decimals=None):
    defaults = {"unit": unit, "min": 0, "color": {"mode": "fixed", "fixedColor": color}}
    if decimals is not None:
        defaults["decimals"] = decimals
    if maximum is not None:
        defaults["max"] = maximum
    return {"type": "bargauge", "title": title, "description": description, "datasource": DS, "targets": targets(queries, instant=True),
            "fieldConfig": {"defaults": defaults, "overrides": overrides or []},
            "options": {"displayMode": "basic", "orientation": "horizontal", "valueMode": "color", "showUnfilled": True, "namePlacement": "left",
                        "sizing": "auto", "minVizHeight": 12, "maxVizHeight": 28,
                        "reduceOptions": {"calcs": ["lastNotNull"], "fields": "", "values": False}}}


def quantiles(metric, by="", selector=J, legend=""):
    """p50 and p95 of a histogram, per by."""
    group = f"le{', ' + by if by else ''}"
    name = legend or (f"{{{{{by}}}}}" if by else metric.split("_", 1)[1])
    return [(f"histogram_quantile(0.5, sum by ({group}) (rate({metric}_bucket{{{selector}}}[$__rate_interval])))", f"p50 {name}"),
            (f"histogram_quantile(0.95, sum by ({group}) (rate({metric}_bucket{{{selector}}}[$__rate_interval])))", f"p95 {name}")]


def p50(metric, selector=J, window="5m"):
    return f"histogram_quantile(0.5, sum by (le) (rate({metric}_bucket{{{selector}}}[{window}])))"


def per_minute(metric, by, selector=J, extra=""):
    inner = f"{selector}, {extra}" if extra else selector
    return f"sum by ({by}) (rate({metric}{{{inner}}}[$__rate_interval])) * 60"


STATUS = [{"type": "value", "options": {"0": {"text": "—"}}}]
DRY = [{"type": "value", "options": {"1": {"text": "dry run", "color": GOLD}, "0": {"text": "publishing", "color": PATINA}}}]
UP = [{"type": "value", "options": {"1": {"text": "up", "color": PATINA}, "0": {"text": "down", "color": RED}}}]


def take_stats(b):
    b.add(stat("Status", [(f"max by (status) (orchestra_status{{{J}}}) == 1", "{{status}}")], text="name",
               description="The take's status: idle, playing (a count-in too), stopped (stop.all) or ended."), 4, 4)
    b.add(stat("Take", [f"orchestra_take{{{J}}}"], decimals=0), 3, 4)
    b.add(stat("Bar", [f"orchestra_bar{{{J}}}"], decimals=0, color=GOLD, description="The bar the music is at (during a count-in, the bar it starts at)."), 3, 4)
    b.add(bargauge("Progress", [(f"orchestra_bar{{{J}}} / orchestra_bars{{{J}}}", "of the movement")], unit="percentunit", maximum=1), 5, 4)
    b.add(stat("Tempo", [f"orchestra_bpm{{{J}}}"], unit="none", decimals=0, color=SAND, graph=True, description="Beats per minute."), 3, 4)
    b.add(stat("Count-in", [f"orchestra_count_in_bars{{{J}}}"], decimals=0, color=CREAM, description="Bars of count-in left."), 3, 4)
    b.add(stat("Hands", [f"orchestra_hands_dry_run{{{J}}}"], mappings=DRY, description="Dry run: Jev reads the hands, nothing is published."), 3, 4)


def variables():
    return {"list": [
        {"name": "job", "label": "Score service", "type": "query", "datasource": DS, "definition": "label_values(orchestra_bpm, job)",
         "query": {"query": "label_values(orchestra_bpm, job)", "refId": "job"}, "refresh": 2, "sort": 1, "includeAll": False, "multi": False,
         "current": {"text": "score", "value": "score"},
         "description": "score: the one the headset conducts (127.0.0.1:8790); score-test: a test instance (127.0.0.1:8795)"},
        {"name": "team", "label": "Team", "type": "query", "datasource": DS, "definition": "label_values(axlr_team_records_total, team)",
         "query": {"query": "label_values(axlr_team_records_total, team)", "refId": "team"}, "refresh": 2, "sort": 1, "includeAll": False,
         "multi": False, "current": {"text": "orchestra", "value": "orchestra"}},
    ]}


def dashboard(uid, title, description, board):
    board.newline()
    return {"uid": uid, "title": title, "description": description, "tags": ["orchestra"], "editable": False, "graphTooltip": 1,
            "schemaVersion": 39, "version": 1, "refresh": "5s", "time": {"from": "now-15m", "to": "now"}, "timezone": "browser",
            "timepicker": {"refresh_intervals": ["5s", "10s", "30s", "1m"]}, "fiscalYearStartMonth": 0, "liveNow": False,
            "templating": variables(), "annotations": {"list": []},
            "links": [{"type": "dashboards", "tags": ["orchestra"], "asDropdown": False, "includeVars": True, "keepTime": True,
                       "title": "Orchestra", "icon": "external link", "targetBlank": False}],
            "panels": board.panels}


def conductor():
    b = Board()
    take_stats(b)
    b.row("The hands")
    b.add(timeseries("Readings per minute, by gesture", [(per_minute("orchestra_hands_gestures_total", "gesture"), "{{gesture}}")], stack=True, bars=True,
                     description="Each hand Jev read, after the threshold (nothing included), per minute.",
                     overrides=colors({"open": GOLD, "down": SAND, "fist": COPPER, "hush": PATINA, "point": BRASS, "nothing": GREY})), 8, 8)
    b.add(timeseries("Headset gestures per minute", [(per_minute("orchestra_hands_native_total", "gesture"), "{{gesture}}")], stack=True, bars=True,
                     description="The headset's own gestures, read by the headset, not by Jev: taps, swipes, pinches, the double pinch, beats."), 8, 8)
    b.add(timeseries("Jev's latency", quantiles("orchestra_jev_seconds", "kind"), unit="s",
                     description="How long Jev took to answer the service: posture (the hands) and expression (the motion)."), 8, 8)
    b.add(timeseries("Under the threshold, per minute", [(per_minute("orchestra_hands_under_threshold_total", "choice"), "{{choice}}")], bars=True,
                     description="A gesture Jev chose with less than the threshold's probability: read as nothing."), 8, 7)
    b.add(timeseries("Jev's probability for the gesture read", [
        (f"histogram_quantile(0.5, sum by (le, choice) (rate(orchestra_hands_choice_probability_bucket{{{J}}}[$__rate_interval])))", "p50 {{choice}}")],
        unit="percentunit", minimum=0, maximum=1), 8, 7)
    b.add(bargauge("Why nothing was sent", [(f"topk(8, sum by (source, why) (increase(orchestra_hands_not_sent_total{{{J}}}[$__range])))",
                                             "{{source}}: {{why}}")], description="In the time range: readings that sent nothing, and why.", decimals=0), 8, 7)
    b.row("Levels: a flat hand's level is provisional until a double pinch keeps it")
    b.add(timeseries("Levels pending, kept, reverted", [(f"sum by (kind, reason) (increase(orchestra_hands_levels_total{{{J}}}[$__rate_interval]))",
                                                          "{{kind}} · {{reason}}")], bars=True, stack=True,
                     overrides=colors(LEVEL_KINDS), description="Each provisional level, and how it ended: kept (double pinch, pinch released) or reverted (why)."), 8, 8)
    b.add(timeseries("Provisional and kept level per group", step=True, queries=[(f"orchestra_hands_level_pending{{{J}}}", "pending {{group}} ({{side}})"),
                                                               (f"orchestra_hands_level_kept{{{J}}}", "kept {{group}}")],
                     unit="percentunit", minimum=0, maximum=1, overrides=colors({"pending": GOLD, "kept": PATINA})), 8, 8)
    b.add(stat("Double pinches", [(f"sum(increase(orchestra_hands_native_total{{{J}, gesture=\"pinch-double\"}}[$__range])) or vector(0)", "in the range")],
               decimals=0, color=PATINA, description="Double pinches in the time range (each keeps the levels waiting)."), 4, 4)
    b.add(stat("Kept · reverted", [(f"sum(increase(orchestra_hands_levels_total{{{J}, kind=\"kept\"}}[$__range])) or vector(0)", "kept"),
                                   (f"sum(increase(orchestra_hands_levels_total{{{J}, kind=\"reverted\"}}[$__range])) or vector(0)", "reverted")],
               decimals=0, color=GOLD, text="value_and_name"), 4, 4)
    b.add(stat("Hands conducting a level now", [(f"count(orchestra_hands_engaged{{{J}}}) or vector(0)", "hands")], decimals=0, color=SAND), 4, 4)
    b.add(stat("Last expression", [(f"max by (character, group) (orchestra_expression_current{{{J}}})", "{{character}} · {{group}}")], text="name",
               color=COPPER), 4, 4)
    b.row("Gains and the expression")
    b.add(timeseries("Gain per section (as it sounds)", step=True, queries=[(f"orchestra_section_gain_now{{{J}}}", "{{section}}")], unit="percentunit", minimum=0, maximum=1,
                     overrides=colors(SECTIONS), fill=0, description="Each section's gain on its way to the one the conductor set, 0 to 1."), 12, 8)
    b.add(timeseries("Expression: chairs per character", step=True, queries=[(f"sum by (character) (orchestra_section_character{{{J}, character!=\"plain\"}})",
                                                           "{{character}}")], stack=True, overrides=colors(CHARACTERS), decimals=0,
                     description="How many chairs play each character (the plain voice left out)."), 12, 8)
    b.add(timeseries("Expression read per minute", [(per_minute("orchestra_expression_reads_total", "character"), "{{character}}")], bars=True, stack=True,
                     overrides=colors(CHARACTERS), description="Jev's reading of the motion after the threshold (nothing included)."), 12, 7)
    b.add(timeseries("Expression changes and publishes per minute", [
        (per_minute("orchestra_expression_changes_total", "character"), "changed {{character}}"),
        (per_minute("orchestra_hands_events_total", "mode", extra='source="motion"'), "sent ({{mode}})")], bars=True), 12, 7)
    return dashboard("orchestra-conductor", "Conductor", "What the conductor's hands did, as the score service read them.", b)


def orchestra():
    b = Board()
    take_stats(b)
    b.add(stat("Chairs playing", [(f"sum(orchestra_chairs{{{J}, state=\"playing\"}})", "playing")], decimals=0, color=COPPER, graph=True), 4, 4)
    b.add(stat("Chairs held", [(f"sum(orchestra_chairs_held{{{J}}})", "held")], decimals=0, color=GOLD,
               description="Faded out (a fist, a stop.<section>) until a cue names them."), 4, 4)
    b.add(stat("Late phrases", [(f"(sum(increase(orchestra_chair_plays_total{{{J}, late=\"yes\"}}[$__range])) or vector(0)) / sum(increase(orchestra_chair_plays_total{{{J}}}[$__range]))",
                                 "late")], unit="percentunit", decimals=0,
               thresholds=[(None, PATINA), (0.1, GOLD), (0.3, RED)], description="Share of the phrases asked for in the range that began late."), 4, 4)
    b.add(stat("Phrases begun", [(f"sum(increase(orchestra_phrases_begun_total{{{J}}}[$__range])) or vector(0)", "begun")], decimals=0, color=SAND), 4, 4)
    b.add(stat("Cues", [(f"sum(increase(orchestra_cues_total{{{J}}}[$__range])) or vector(0)", "cues")], decimals=0, color=CREAM), 4, 4)
    b.add(stat("Cuts", [(f"sum(increase(orchestra_cuts_total{{{J}}}[$__range])) or vector(0)", "stop.all")], decimals=0, color=RED), 4, 4)
    b.row("The chairs")
    b.add(timeseries("Chairs playing per section", step=True, queries=[(f"sum by (section) (orchestra_chairs{{{J}, state=\"playing\"}})", "{{section}}")], stack=True,
                     overrides=colors(SECTIONS), decimals=0), 12, 8)
    b.add(bargauge("Chairs now, per state", [(f"sum by (state) (orchestra_chairs{{{J}}})", "{{state}}"), (f"sum(orchestra_chairs_held{{{J}}})", "held")],
                   overrides=colors({"playing": COPPER, "scheduled": GOLD, "waiting": GREY, "stopped": RED, "held": SAND})), 6, 8)
    b.add(bargauge("Characters now", [(f"orchestra_section_character{{{J}, character!=\"plain\"}} > 0", "{{section}} {{character}}")],
                   description="Chairs per section and character, the plain voice left out."), 6, 8)
    b.add(timeseries("Gain per section: set (dashed) and as it sounds", step=True, queries=[(f"orchestra_section_gain{{{J}}}", "set {{section}}"),
                                                                         (f"orchestra_section_gain_now{{{J}}}", "{{section}}")],
                     unit="percentunit", minimum=0, maximum=1, fill=0,
                     overrides=colors(SECTIONS) + [{"matcher": {"id": "byRegexp", "options": "^set .*"},
                                                    "properties": [{"id": "custom.lineStyle", "value": {"fill": "dash", "dash": [6, 6]}},
                                                                   {"id": "custom.lineWidth", "value": 1}]}]
                     + [{"matcher": {"id": "byRegexp", "options": f"^set {k}$"}, "properties": [{"id": "color", "value": {"mode": "fixed", "fixedColor": c}}]}
                        for k, c in SECTIONS.items()]), 12, 8)
    b.add(timeseries("Cues per target", [(f"sum by (target) (increase(orchestra_cues_total{{{J}}}[$__rate_interval]))", "{{target}}")], bars=True, stack=True,
                     description="Cues the orchestra applied, from the bus or the hands, per section, family or all.", decimals=0), 12, 8)
    b.add(timeseries("Phrases begun per minute", [(per_minute("orchestra_phrases_begun_total", "section"), "{{section}}")], stack=True, bars=True,
                     overrides=colors(SECTIONS)), 8, 7)
    b.add(timeseries("Late phrases per minute", [(per_minute("orchestra_chair_plays_total", "section", extra='late="yes"'), "{{section}}")], bars=True,
                     stack=True, overrides=colors(SECTIONS), description="Phrases joined after their first bar."), 8, 7)
    b.add(timeseries("Bars missed per minute", [(per_minute("orchestra_chair_late_bars_total", "section"), "{{section}}")], bars=True, stack=True,
                     overrides=colors(SECTIONS), description="The sum of lateBars: the bars the late phrases did not sound."), 8, 7)
    b.add(timeseries("The chairs' MCP calls per second", [(f"sum by (tool) (rate(orchestra_chair_calls_total{{{J}}}[$__rate_interval]))", "{{tool}}"),
                                                          (f"sum by (tool) (rate(orchestra_chair_calls_total{{{J}, outcome!=\"ok\"}}[$__rate_interval]))",
                                                           "refused {{tool}}")], unit="reqps"), 12, 7)
    b.add(timeseries("How far ahead a phrase is asked for", quantiles("orchestra_play_lead_seconds", "", legend="lead"), unit="s",
                     description="inSeconds of each play: under a bar, and the chair risks coming in late."), 12, 7)
    b.row("The chairs' tasks, from the control plane")
    b.add(bargauge("Task failures by reason", [(f"sum by (reason) (axlr_team_task_failures_total{{{TEAM}}})", "{{reason}}")], color=RED, decimals=0,
                   description="Since the exporter started, with the timeline's newest 1,000 records it read then."), 8, 8)
    b.add(timeseries("Task events per minute", [(per_minute("axlr_team_tasks_total", "state", TEAM), "{{state}}")], bars=True,
                     overrides=colors({"submitted": SAND, "started": GOLD, "completed": PATINA, "failed": RED, "cancelled": GREY})), 8, 8)
    b.add(stat("Tasks open", [(f"axlr_team_tasks_open{{{TEAM}}}", "{{state}}")], text="value_and_name", decimals=0, color=GOLD), 4, 4)
    b.add(stat("Bots", [(f"axlr_team_bots{{{TEAM}}}", "{{status}}")], text="value_and_name", decimals=0, color=PATINA), 4, 4)
    b.add(stat("Chairs' cost", [(f"sum(increase(axlr_team_cost_usd_total{{{TEAM}}}[$__range])) or vector(0)", "in the range")], unit="currencyUSD", decimals=4,
               color=SAND), 4, 4)
    b.add(stat("Messages to tasks", [(f"sum(increase(axlr_team_task_messages_total{{{TEAM}}}[$__range])) or vector(0)", "in the range")], decimals=0, color=CREAM), 4, 4)
    b.add(bargauge("Budget used by each chair's last run", [(f"topk(12, max by (bot) (axlr_team_budget_used_ratio{{{TEAM}}}))", "{{bot}}")],
                   unit="percentunit", maximum=1, color=GOLD,
                   description="The largest share of its tool calls, model or Jev calls or seconds a chair's last execution used: 100 % is the budget hit."), 8, 8)
    b.add(timeseries("Tool and Jev calls per execution", [
        (f"histogram_quantile(0.5, sum by (le) (rate(axlr_team_execution_tool_calls_bucket{{{TEAM}}}[10m])))", "p50 tool calls"),
        (f"histogram_quantile(0.5, sum by (le) (rate(axlr_team_execution_model_calls_bucket{{{TEAM}}}[10m])))", "p50 model or Jev calls")], decimals=0), 8, 8)
    return dashboard("orchestra-take", "Orchestra", "The take and the chairs: the score service's state and the control plane's tasks.", b)


def pipeline():
    b = Board()
    b.add(stat("Publish p50", [(p50("orchestra_bus_publish_seconds"), "publish")], unit="s", color=COPPER,
               description="From the decision to the control plane's answer (the last 5 min)."), 4, 4)
    b.add(stat("Echo p50", [(p50("orchestra_bus_echo_seconds"), "echo")], unit="s", color=GOLD,
               description="From a publish's start to its echo on the service's long poll."), 4, 4)
    b.add(stat("Delivery p50", [(p50("orchestra_bus_delivery_seconds"), "delivery")], unit="s", color=SAND,
               description="From a publish's start to its delivery to a chair (the control plane's createdAt)."), 4, 4)
    b.add(stat("Jev p50", [(p50("orchestra_jev_seconds"), "Jev")], unit="s", color=PATINA), 4, 4)
    b.add(stat("Headsets connected", [(f"orchestra_ws_connections{{{J}}}", "now")], decimals=0, color=CREAM), 4, 4)
    b.add(stat("Targets up", [('up{job=~"score.*|team"}', "{{job}}")], text="value_and_name", mappings=UP, color=PATINA, size=20,
               description="score: the live service (8790); score-test: the test instance (8795); team: the exporter (8797)."), 4, 4)
    b.row("A gesture's path: hands → publish → echo → the chairs")
    b.add(timeseries("Publish, echo and delivery", quantiles("orchestra_bus_publish_seconds", "kind", legend="publish {{kind}}")
                     + quantiles("orchestra_bus_echo_seconds", "kind", legend="echo {{kind}}")
                     + quantiles("orchestra_bus_delivery_seconds", "mode", legend="delivery {{mode}}"), unit="s",
                     description="All on gx10's clock: publish = the POST; echo = back on the service's long poll; delivery = the control plane's "
                                 "bot.event.heard for each chair, from the publish's start."), 12, 9)
    b.add(timeseries("The bus to the sound", quantiles("orchestra_bus_to_sound_seconds", "what"), unit="s",
                     description="From an operator's stop on the bus (its createdAt) to the sound doing it: cut (stop.all) or hold."), 6, 9)
    b.add(timeseries("The control plane: fan-out and task start", quantiles("axlr_team_fanout_seconds", "", TEAM, "fan-out")
                     + quantiles("axlr_team_task_start_seconds", "", TEAM, "task start"), unit="s",
                     description="Fan-out: from an event's first delivery to its published record. Task start: submitted → started."), 6, 9)
    b.add(timeseries("Publishes per minute", [(per_minute("orchestra_bus_publishes_total", "name", extra='outcome="ok"'), "{{name}}"),
                                              (per_minute("orchestra_bus_publishes_total", "name", extra='outcome="error"'), "error {{name}}")], bars=True,
                     stack=True), 8, 7)
    b.add(timeseries("Deliveries per minute", [(per_minute("orchestra_bus_deliveries_total", "mode"), "the service's events, {{mode}}"),
                                               (per_minute("axlr_team_deliveries_total", "mode", TEAM), "all the team's, {{mode}}")], bars=True), 8, 7)
    b.add(timeseries("The bus as the service reads it", [(per_minute("orchestra_bus_records_total", "type"), "{{type}}"),
                                                         (per_minute("orchestra_bus_follow_errors_total", "reason"), "error {{reason}}")],
                     description="Records per minute on the team's timeline, and errors following it (retried after 2 s)."), 8, 7)
    b.row("Jev")
    b.add(timeseries("Jev's cost per minute", [(per_minute("orchestra_jev_cost_usd_total", "kind"), "service: {{kind}}"),
                                               (f"sum(rate(axlr_team_cost_usd_total{{{TEAM}}}[$__rate_interval])) * 60", "chairs (control plane)")],
                     unit="currencyUSD", decimals=5, stack=False,
                     description="The service's: Jev's input tokens at the configured price (jev-1.13.0: $0.042 per million). The chairs': the control plane's costUsd."), 8, 7)
    b.add(timeseries("Jev's calls per minute", [(per_minute("orchestra_jev_calls_total", "kind, outcome"), "{{kind}} {{outcome}}")], bars=True, stack=True), 8, 7)
    b.add(stat("Jev in the range", [(f"sum(increase(orchestra_jev_cost_usd_total{{{J}}}[$__range])) or vector(0)", "service"),
                                    (f"sum(increase(orchestra_jev_input_tokens_total{{{J}}}[$__range])) or vector(0)", "input tokens")],
               text="value_and_name", unit="currencyUSD", decimals=4, color=PATINA, size=26,
               overrides=[{"matcher": {"id": "byName", "options": "input tokens"},
                           "properties": [{"id": "unit", "value": "short"}, {"id": "decimals", "value": 0}]}]), 4, 7)
    b.add(bargauge("Jev's errors", [(f"sum by (kind, reason) (increase(orchestra_jev_errors_total{{{J}}}[$__range]))", "{{kind}}: {{reason}}")], color=RED, decimals=0), 4, 7)
    b.row("The headset's link and the sound")
    b.add(timeseries("WebSocket messages per second", [(f"sum by (direction, type) (rate(orchestra_ws_messages_total{{{J}}}[$__rate_interval]))",
                                                        "{{direction}} {{type}}")], unit="reqps"), 8, 7)
    b.add(timeseries("HTTP responses per second", [(f"sum by (route, status) (rate(orchestra_http_responses_total{{{J}}}[$__rate_interval]))",
                                                    "{{route}} {{status}}")], unit="reqps"), 8, 7)
    b.add(timeseries("Render time per block", [
        (f"histogram_quantile(0.5, sum by (le) (rate(orchestra_render_block_seconds_bucket{{{J}}}[$__rate_interval])))", "p50"),
        (f"histogram_quantile(0.99, sum by (le) (rate(orchestra_render_block_seconds_bucket{{{J}}}[$__rate_interval])))", "p99"),
        (f"max(orchestra_render_lag_seconds{{{J}}})", "behind the clock")], unit="s",
        description="A block is 10 ms of audio (441 frames at 44.1 kHz): rendering it takes less, or the sound falls behind.",
        overrides=colors({"p50": PATINA, "p99": GOLD, "behind": RED})), 8, 7)
    b.add(timeseries("Late blocks, resets and losses per second", [(f"rate(orchestra_render_late_blocks_total{{{J}}}[$__rate_interval])", "late blocks"),
                                                                    (f"rate(orchestra_render_resets_total{{{J}}}[$__rate_interval])", "clock resets"),
                                                                    (f"rate(orchestra_audio_dropped_blocks_total{{{J}}}[$__rate_interval])", "lost by a slow listener")],
                     description="Late: rendered after its time on the wall clock (a listener's buffer drains); reset: more than a second behind."), 8, 7)
    b.add(timeseries("Sound listeners", step=True, queries=[(f"orchestra_audio_listeners{{{J}}}", "/audio.wav")], decimals=0), 8, 7)
    b.add(timeseries("The processes", [(f'rate(process_cpu_seconds_total{{job=~"$job|team"}}[$__rate_interval])', "CPU {{job}}"),
                                       (f'process_resident_memory_bytes{{job=~"$job|team"}} / 2^30', "GiB {{job}}")],
                     description="CPU cores used and resident memory of the score service and the exporter."), 8, 7)
    return dashboard("orchestra-pipeline", "Pipeline", "From a gesture to the chairs and the sound: latencies, Jev's cost, the link, the render loop.", b)


def build():
    return {"conductor.json": conductor(), "orchestra.json": orchestra(), "pipeline.json": pipeline()}


def text(d):
    return json.dumps(d, indent=2, ensure_ascii=False) + "\n"


def main(argv=None):
    argv = sys.argv[1:] if argv is None else argv
    stale = []
    for name, d in build().items():
        path = os.path.join(OUT, name)
        current = open(path, encoding="utf-8").read() if os.path.exists(path) else None
        if current != text(d):
            stale.append(name)
            if "--check" not in argv:
                os.makedirs(OUT, exist_ok=True)
                with open(path, "w", encoding="utf-8") as f:
                    f.write(text(d))
    if "--check" in argv and stale:
        print("not up to date: " + ", ".join(stale) + " (run python3 dashboards.py)")
        return 1
    print(("wrote " if stale else "up to date: ") + ", ".join(stale or build()))
    return 0


if __name__ == "__main__":
    sys.exit(main())
