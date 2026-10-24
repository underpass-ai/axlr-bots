#!/bin/sh
# Phase 0 demo of the stub control plane: register a bot, "restart" (every
# step is a new process over the same state directory), submit a task, run
# it through AXLR, read the result, resubmit the same key and show that no
# second execution happens.
#
#   UNDERPASS_AXLR_BIN=<axlr-tui with --exec> sh examples/phase0-demo.sh
#
# Without UNDERPASS_AXLR_BIN it uses examples/fake-axlr-tui, which needs
# no model provider.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
state=${UNDERPASS_STATE:-$(mktemp -d)}
bin=${UNDERPASS_AXLR_BIN:-$here/fake-axlr-tui}
ub="go run $here/../cmd/underpass-bots --state $state --axlr-bin $bin"
key=${DEMO_KEY:-demo-2026-10-24-001}

echo "# state: $state"
echo "# axlr:  $bin"
echo
echo "## 1. register"
$ub bot register --file "$here/bot.json" | grep -E '"id"|"status"'
echo
echo "## 2. restart: a new process reads the bot back and enables it"
$ub bot enable bot | grep -E '"id"|"status"'
echo
echo "## 3. submit"
submit=$($ub task submit bot --objective "List the files in your workspace and say how many there are" --key "$key")
echo "$submit" | grep -E '"created"|"id"|"status"' | head -3
task=$(echo "$submit" | sed -n 's/.*"id": "\(task-[^"]*\)".*/\1/p' | head -1)
echo
echo "## 4. run"
set +e
run=$($ub task run "$task")
code=$?
set -e
echo "$run" | grep -E '"outcome"|"answer"|"failureReason"|"costUsd"|"requests"' | head -5
echo "exit code: $code (0 completed, 3 decision required, 1 failed)"
execution=$(echo "$run" | sed -n 's/.*"id": "\(exec-[^"]*\)".*/\1/p' | head -1)
echo
echo "## 5. read the result, not the trace"
$ub execution get "$execution" | grep -E '"outcome"|"answer"|"tracePath"'
echo
echo "## 6. resubmit the same key: same task, no second execution"
$ub task submit bot --objective "List the files in your workspace and say how many there are" --key "$key" | grep -E '"created"|"id"|"status"' | head -3
set +e
$ub task run "$task"
echo "second run exit code: $? (refused)"
set -e
echo
echo "executions on disk: $(ls "$state/executions" | wc -l)"
echo "events: $(wc -l < "$state/events.jsonl")  audit entries: $(wc -l < "$state/audit.jsonl")"
