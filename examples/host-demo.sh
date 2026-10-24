#!/bin/sh
# Host demo of the SRE bot, in user mode (no root): install the control
# plane as a systemd --user service, start it, register the bot, break a
# throwaway unit that has the OnFailure drop-in, watch the diagnosis task
# appear and run, read the report, restart the control plane and show the
# bot and the task are still there, then remove everything.
#
#   export OPENROUTER_API_KEY=...                  # the installer copies it into the env file
#   UNDERPASS_AXLR_BIN=/path/to/axlr-tui sh examples/host-demo.sh
#
# The axlr-tui needs --exec; with --policy the bot's rules are enforced
# by AXLR, without it they are only recorded and every command waits for
# a person. DEMO_KEEP=1 keeps the installation at the end.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
: "${UNDERPASS_AXLR_BIN:?set UNDERPASS_AXLR_BIN to an axlr-tui with --exec}"
: "${OPENROUTER_API_KEY:?export OPENROUTER_API_KEY for the bot's model}"
prefix=${DEMO_PREFIX:-$HOME/.local}
state=$HOME/.local/state/underpass-bots
envfile=$HOME/.config/underpass-bots/env
unit=${DEMO_UNIT:-underpass-demo-broken}
unitdir=$HOME/.config/systemd/user
build=$(mktemp -d)
ub="$prefix/bin/underpass-bots --state $state"
ctl="systemctl --user"

say() { printf '\n## %s\n' "$*"; }
wait_for_socket() {
  i=0
  while [ ! -S "$state/underpass-bots.sock" ]; do
    i=$((i + 1)); [ $i -gt 50 ] && { echo "the service did not start: $($ctl status underpass-bots.service --no-pager | tail -5)"; exit 1; }
    sleep 0.2
  done
}
cleanup() {
  say "cleanup"
  $ctl stop "$unit.service" 2>/dev/null || true
  $ctl reset-failed "$unit.service" 2>/dev/null || true
  rm -rf "$unitdir/$unit.service" "$unitdir/$unit.service.d"
  if [ "${DEMO_KEEP:-0}" = 1 ]; then
    $ctl daemon-reload
    echo "kept: $prefix/bin/underpass-bots, the units, $envfile and $state"
  else
    "$build/underpass-bots" uninstall --user --prefix "$prefix" | grep -E '"mode"|"commands"' || true
    echo "kept the state ($state) and the env file ($envfile); remove them with: underpass-bots uninstall --user --purge"
  fi
  rm -rf "$build"
}
trap cleanup EXIT

say "0. build and install (user mode)"
GOFLAGS=-p=2 go build -o "$build/underpass-bots" "$here/../cmd/underpass-bots"
"$build/underpass-bots" install --user --prefix "$prefix" --axlr-bin "$UNDERPASS_AXLR_BIN" | grep -E '"mode"|"binary"|"envFile"|"socket"|units|\.service'
grep -q '^OPENROUTER_API_KEY=' "$envfile" || echo "WARNING: $envfile has no OPENROUTER_API_KEY line (it existed before); add it and restart the service"

say "1. start the service and register the bot"
$ctl enable --now underpass-bots.service
wait_for_socket
$ub bot register --file "$here/sre-host-bot.json" >/dev/null 2>&1 || echo "(bot already registered: the state survived an earlier run)"
$ub bot enable sre-host-bot >/dev/null 2>&1 || true
$ub bot get sre-host-bot | grep -E '"id"|"level"|"status"'

say "2. a throwaway unit that fails, with the OnFailure drop-in attached"
mkdir -p "$unitdir/$unit.service.d"
cat > "$unitdir/$unit.service" <<UNIT
[Unit]
Description=Underpass demo: a unit that fails on start

[Service]
Type=oneshot
ExecStart=/bin/sh -c 'echo "demo-app: cannot open /etc/demo/app.conf: No such file or directory" >&2; echo "demo-app: refusing to start without a configuration" >&2; exit 1'
UNIT
cat > "$unitdir/$unit.service.d/underpass-bots.conf" <<DROPIN
[Unit]
OnFailure=underpass-bots-alert@%N.service
DROPIN
cat "$unitdir/$unit.service.d/underpass-bots.conf"
$ctl daemon-reload
$ctl start "$unit.service" || echo "$unit.service failed, as intended (exit $?)"
sleep 2

say "3. the task the alert created (the same alert again names the same task)"
alert=$($ub alert --unit "$unit" --user)
echo "$alert" | grep -E '"created"|"id"|"status"|"idempotencyKey"' | head -4
task=$(echo "$alert" | sed -n 's/.*"id": "\(task-[^"]*\)".*/\1/p' | head -1)
echo "what the OnFailure alert unit logged:"
journalctl --user -u "underpass-bots-alert@$unit.service" --no-pager -o cat | grep -E '"created"|Finished' | tail -2

say "4. the service runs it through AXLR (policy: observe = read-only host commands)"
i=0
while :; do
  status=$($ub task get "$task" | sed -n 's/.*"status": "\([A-Za-z]*\)".*/\1/p' | head -1)
  case "$status" in Completed|Failed|WaitingApproval) break ;; esac
  i=$((i + 1)); [ $i -gt 360 ] && { echo "timed out waiting; task is $status"; break; }
  sleep 2
done
echo "task $task is $status after about $((i * 2))s"
execution=$($ub task get "$task" | sed -n 's/.*"exec-\([^"]*\)".*/exec-\1/p' | tail -1)
$ub execution get "$execution" | grep -E '"outcome"|"enforced"|"runtimeLevel"|"requests"|"toolCalls"|"costUsd"|"failureReason"'
echo "commands the bot ran (from the execution's tool calls):"
$ub execution get "$execution" | grep -E '"name"|"status"' | paste - - | sed 's/[",]//g; s/  */ /g' | sort | uniq -c
echo "report file: $($ub task report --path "$task")"

say "5. the report"
$ub task report "$task"

say "6. restart the control plane: the bot and the task are still there"
$ctl restart underpass-bots.service
wait_for_socket
$ub bot get sre-host-bot | grep -E '"id"|"status"'
$ub task get "$task" | grep -E '"id"|"status"'
echo "service log (last lines):"
journalctl --user -u underpass-bots.service -n 4 --no-pager -o cat
