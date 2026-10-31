#!/usr/bin/env bash
# The stage: one axlr-node per chair on this host, beside the control plane.
#
# A node runs one execution at a time (adapters/nodeagent/agent.go), so a
# section that enters together needs one node per chair. Each node talks to
# the control plane over its Unix socket (no enrollment, no TLS, no port),
# is dedicated (it runs only the bots placed on it) and has its own HOME, so
# its AXLR reads its own ~/.config/axlr/mcp.json and never the host's.
#
#   stage.sh start  <node>...   start the nodes that are not running
#   stage.sh stop   [<node>...] stop them (all of the stage's without names)
#   stage.sh status             each node's pid, or "stopped"
#   stage.sh mcp    <node> <url> write the node's mcp.json: one plugin, the score
#   stage.sh settings <node>    write the node's AXLR settings.json: Jev's tool
#                               (axlr_judge) on, a turn long enough for a movement
#
# A node's id is stage-<node>; its labels app=stage,chair=<node>. Its state is
# $AXLR_STAGE/<node> (default ~/.local/state/axlr-stage), mode 0700: AXLR
# refuses a diagnostic directory another account may write. The model key
# comes from $AXLR_STAGE_ENV (default ~/.config/axlr-bots/env), read by the
# node's shell and never printed.
set -euo pipefail

STAGE=${AXLR_STAGE:-$HOME/.local/state/axlr-stage}
ENV_FILE=${AXLR_STAGE_ENV:-$HOME/.config/axlr-bots/env}
SOCKET=${AXLR_BOTS_SOCKET:-$HOME/.local/state/axlr-bots/axlr-bots.sock}
NODE_BIN=${AXLR_NODE_BIN:-axlr-node}

pid_of() { pgrep -f -- "--state $STAGE/$1 " || true; }

start() {
  mkdir -p "$STAGE" && chmod 700 "$STAGE"
  for n in "$@"; do
    if [ -n "$(pid_of "$n")" ]; then echo "$n running"; continue; fi
    mkdir -p "$STAGE/$n" && chmod 700 "$STAGE/$n"
    (
      set -a; [ -r "$ENV_FILE" ] && . "$ENV_FILE"; set +a
      HOME="$STAGE/$n" setsid "$NODE_BIN" --state "$STAGE/$n" \
        --control "unix://$SOCKET" --node-id "stage-$n" --dedicated \
        --labels "app=stage,chair=$n" >>"$STAGE/$n/node.log" 2>&1 < /dev/null &
    )
    echo "$n started"
  done
}

stop() {
  local names=("$@")
  [ ${#names[@]} -eq 0 ] && mapfile -t names < <(ls "$STAGE" 2>/dev/null)
  for n in "${names[@]}"; do
    p=$(pid_of "$n"); [ -z "$p" ] && continue
    kill $p && echo "$n stopped"
  done
}

status() {
  for d in "$STAGE"/*/; do
    n=$(basename "$d"); p=$(pid_of "$n")
    echo "$n ${p:-stopped}"
  done
}

mcp() {
  local n=$1 url=$2 dir="$STAGE/$1/.config/axlr"
  mkdir -p "$dir/plugins"
  printf '{"manifest_version": 1, "id": "score", "url": "%s", "allow_tools": ["*"]}\n' "$url" >"$dir/plugins/score.json"
  printf '{"version": 1, "plugins": [{"manifest": "%s", "name": "score", "description": "the score service: this chair'"'"'s part", "purpose": "tools", "approval": "auto"}]}\n' "$dir/plugins/score.json" >"$dir/mcp.json"
  echo "$n mcp $url"
}

# Jev's key is TYPESAFE_API_KEY, read from the environment file with the
# model's; AXLR refuses to start with the tool on and no key.
settings() {
  local n=$1 dir="$STAGE/$1/.config/axlr"
  mkdir -p "$dir"
  printf '{"jev": {"tool": true, "final_check": false, "model": "jev-1.13.0", "timeout_ms": 20000}, "turn_tool_calls": 400}\n' >"$dir/settings.json"
  chmod 600 "$dir/settings.json"
  echo "$n settings: jev tool on"
}

cmd=${1:-status}; shift || true
case $cmd in
  start) start "$@" ;;
  stop) stop "$@" ;;
  status) status ;;
  mcp) mcp "$@" ;;
  settings) settings "$@" ;;
  *) echo "usage: stage.sh start|stop|status|mcp|settings" >&2; exit 2 ;;
esac
