#!/bin/sh
# Live demo on one host, user-level (no root): build and install the
# control plane and the node agent as systemd --user services, with the
# control plane listening for nodes over TLS on 127.0.0.1:7878 beside
# its socket; create the fleet CA, start the control plane, enroll the
# local node through the TLS path with a one-time token (so the
# installed node uses mTLS, as a node on another host would), start it
# and show it Online with its certificate; register the example bot,
# submit a generic task and watch the node run it, read the report; then
# the approval path with a harmless write in the bot's workspace; then
# restart both services and show the node, the bots and the tasks are
# still there; then the automatic triggers: a timer and a webhook on the
# example bot, the webhook fired with curl against the operator address
# with the trigger's secret, both tasks and their executions, the same
# delivery again (no second task), and a restart that fires nothing
# twice. The installation stays (DEMO_UNINSTALL=1 removes it).
#
#   export OPENROUTER_API_KEY=...                  # the installer copies it into the env file
#   AXLR_TUI=/path/to/axlr-tui sh examples/demo.sh
set -eu
here=$(cd "$(dirname "$0")" && pwd)
: "${AXLR_TUI:?set AXLR_TUI to an axlr-tui with --exec}"
: "${OPENROUTER_API_KEY:?export OPENROUTER_API_KEY for the bots' model}"
prefix=${DEMO_PREFIX:-$HOME/.local}
state=$HOME/.local/state/axlr-bots
nodestate=$HOME/.local/state/axlr-node
envfile=$HOME/.config/axlr-bots/env
build=$(mktemp -d)
ub="$prefix/bin/axlr-bots --state $state"
node="$prefix/bin/axlr-node"
control=${DEMO_CONTROL:-https://127.0.0.1:7878}
listen=${DEMO_LISTEN:-127.0.0.1:7878}
api=${DEMO_API_LISTEN:-127.0.0.1:7879}
ctl="systemctl --user"
stamp=$(date -u +%Y%m%dT%H%M%SZ)

say() { printf '\n## %s\n' "$*"; }
wait_for_socket() {
  i=0
  while [ ! -S "$state/axlr-bots.sock" ]; do
    i=$((i + 1)); [ $i -gt 50 ] && { echo "the control plane did not start:"; $ctl status axlr-bots.service --no-pager | tail -5; exit 1; }
    sleep 0.2
  done
}
wait_for_node() {
  i=0
  while ! $ub node list 2>/dev/null | grep -q '"status": "Online"'; do
    i=$((i + 1)); [ $i -gt 100 ] && { echo "the node did not come online:"; $ctl status axlr-node.service --no-pager | tail -5; exit 1; }
    sleep 0.2
  done
}
wait_for_task() {
  i=0
  while :; do
    status=$($ub task get "$1" | sed -n 's/.*"status": "\([A-Za-z]*\)".*/\1/p' | head -1)
    case "$status" in Completed|Failed|WaitingApproval) break ;; esac
    i=$((i + 1)); [ $i -gt 300 ] && { echo "timed out; task is $status"; break; }
    sleep 2
  done
  echo "task $1 is $status after about $((i * 2))s"
}
task_id() { sed -n 's/.*"id": *"\(task-[^"]*\)".*/\1/p' | head -1; }
last_execution() { $ub task get "$1" | sed -n 's/.*"exec-\([^"]*\)".*/exec-\1/p' | tail -1; }
cleanup() {
  rm -rf "$build"
  if [ "${DEMO_UNINSTALL:-0}" = 1 ]; then
    say "uninstall (DEMO_UNINSTALL=1)"
    "$prefix/bin/axlr-bots" uninstall --prefix "$prefix" | grep -E '"mode"|disable' || true
  fi
}
trap cleanup EXIT

say "0. build and install (user-level: ~/.local, ~/.config, systemctl --user; nodes on $listen and operators on $api, both TLS)"
GOFLAGS=-p=2 go build -o "$build/axlr-bots" "$here/../cmd/axlr-bots"
GOFLAGS=-p=2 go build -o "$build/axlr-node" "$here/../cmd/axlr-node"
"$build/axlr-bots" install --prefix "$prefix" --axlr-bin "$AXLR_TUI" --listen "$listen" --api-listen "$api" --control "$control" | grep -E '"mode"|"binary"|"nodeBinary"|"envFile"|"socket"|"listen"|"apiListen"|"control"|\.service|ca init|api.token|linger'
grep -q '^OPENROUTER_API_KEY=' "$envfile" || echo "WARNING: $envfile has no OPENROUTER_API_KEY line (it existed before); add it and restart the services"

say "1. the fleet CA (once per fleet; kept across runs)"
if [ -f "$state/ca/ca.key" ]; then echo "(the CA exists: $state/ca)"; else $ub ca init | grep -E '"dir"|"subject"|"notAfter"'; fi
$ub ca show | grep -E '"fingerprint"'

say "2. start the control plane; enroll this host as a node over TLS with a one-time token; start the node"
$ctl enable --now axlr-bots.service
$ctl restart axlr-bots.service
wait_for_socket
if [ -f "$nodestate/pki/node.crt" ] && [ "${DEMO_REENROLL:-0}" != 1 ]; then
  echo "(the node is enrolled: $nodestate/pki; DEMO_REENROLL=1 enrolls it again and rotates its certificate)"
else
  minted=$($ub node enroll-token --node-id "$(hostname | tr 'A-Z' 'a-z' | cut -d. -f1)" --ttl 10m)
  printf '%s\n' "$minted" | sed 's/"token": "axlr1\.[^"]*"/"token": "axlr1.<one line, shown once; consumed below>"/'
  token=$(printf '%s\n' "$minted" | sed -n 's/.*"token": "\([^"]*\)".*/\1/p')
  "$node" enroll --control "$control" --token "$token" --state "$nodestate" | grep -E '"nodeId"|"control"|"serial"|"notAfter"|"state"'
fi
$ctl enable --now axlr-node.service
$ctl restart axlr-node.service
wait_for_node
$ub node list | grep -E '"id"|"hostName"|"status"|"axlrVersion"|"policySupported"|"host"|"certificateSerial"'
journalctl --user -u axlr-node.service -n 2 --no-pager -o cat | grep -E 'control plane|registered'

say "3. register the example bot (observe: AXLR's read tools, manual trigger, any node)"
$ub bot register --file "$here/bot.json" >/dev/null 2>&1 || echo "(bot already registered: the state survived an earlier run)"
$ub bot enable bot >/dev/null 2>&1 || true
$ub bot get bot | grep -E '"id"|"level"|"status"|"type"'

say "4. a generic task: the control plane assigns it to the node over TLS, the node runs it with AXLR"
task=$($ub task submit bot --objective "List the files in your workspace and say how many there are." --key "demo-list-$stamp" | task_id)
wait_for_task "$task"
execution=$(last_execution "$task")
$ub execution get "$execution" | grep -E '"outcome"|"nodeId"|"enforced"|"runtimeLevel"|"requests"|"toolCalls"|"costUsd"|"failureReason"' | sort -u
echo "report:"
$ub task report "$task" | sed 's/^/    /'

say "5. the approval path: a write runs only after task approve"
$ub bot register --file "$here/bot-act-with-approval.json" >/dev/null 2>&1 || true
$ub bot enable bot-act-with-approval >/dev/null 2>&1 || true
write=$($ub task submit bot-act-with-approval --objective "Create a file named hello-$stamp.txt in your workspace containing the single word hello, then say what you did." --key "demo-write-$stamp" | task_id)
wait_for_task "$write"
$ub task get "$write" | grep -E '"status"'
$ub execution get "$(last_execution "$write")" | grep -E '"outcome"|"nodeId"|"tool"|"callId"|"arguments"' | head -6
ls "$nodestate/workspaces/bot-act-with-approval/hello-$stamp.txt" 2>/dev/null && echo "UNEXPECTED: the file exists before the approval" || echo "the file does not exist yet: nothing ran without approval"
echo "approving..."
$ub task approve "$write" | grep -E '"status"|"kind"|"nodeId"' | head -3
wait_for_task "$write"
$ub task report "$write" | sed 's/^/    /'
ls -la "$nodestate/workspaces/bot-act-with-approval/hello-$stamp.txt" && cat "$nodestate/workspaces/bot-act-with-approval/hello-$stamp.txt"

say "6. restart both services: node, bots and tasks are still there"
$ctl restart axlr-bots.service axlr-node.service
wait_for_socket
wait_for_node
$ub node list | grep -E '"id"|"status"|"certificateSerial"'
$ub bot list | grep -E '"id"|"status"'
$ub task get "$task" | grep -E '"id"|"status"'
$ub task get "$write" | grep -E '"id"|"status"'
echo "service logs (last lines):"
journalctl --user -u axlr-bots.service -n 3 --no-pager -o cat
journalctl --user -u axlr-node.service -n 4 --no-pager -o cat

say "7. automatic triggers: a timer every 10 minutes and a webhook on the example bot"
secret=$(head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')
patch="$build/triggers.json"
cat > "$patch" <<EOF
{"triggers": [
  {"type": "manual"},
  {"type": "timer", "name": "clock", "every": "10m", "objective": "Say the current time of this host (the trigger fired at {{.firedAt}})."},
  {"type": "webhook", "name": "deploy", "secret": "$secret", "objective": "Handle event {{.type}} for {{.service}}: say in one sentence what you would check first, without running anything."}
]}
EOF
$ub bot update bot --file "$patch" | grep -E '"type"|"name"|"every"|"secret"|"objective"'
echo "(the secret is generated per run and shown redacted; the timer is armed and fires at once)"
webhook() { curl -sS --cacert "$state/ca/ca.crt" -H "X-Axlr-Secret: $secret" -H "Idempotency-Key: deploy-$stamp" -H 'Content-Type: application/json' -d '{"type": "deploy", "service": "web"}' "https://$api/v1/triggers/bot/deploy"; }
echo "curl --cacert $state/ca/ca.crt -H 'X-Axlr-Secret: <secret>' -H 'Idempotency-Key: deploy-$stamp' -d '{\"type\": \"deploy\", \"service\": \"web\"}' https://$api/v1/triggers/bot/deploy"
webhook | tr ',' '\n' | grep -E '"created"|"id"|"objective"|"idempotencyKey"|"trigger"' | head -5
webhook_task=$(webhook | task_id)
echo "the same delivery again: $(webhook | tr ',' '\n' | grep -E '"created"' | head -1 | tr -d ' {'), same task $webhook_task"
curl -sS -o /dev/null -w 'without the secret: HTTP %{http_code}\n' --cacert "$state/ca/ca.crt" -d '{"type": "deploy", "service": "web"}' "https://$api/v1/triggers/bot/deploy"
curl -sS -o /dev/null -w 'operator route without the token: HTTP %{http_code}\n' --cacert "$state/ca/ca.crt" "https://$api/v1/bots"
curl -sS -o /dev/null -w 'operator route with the token: HTTP %{http_code}\n' --cacert "$state/ca/ca.crt" -H "Authorization: Bearer $(cat "$state/api.token")" "https://$api/v1/bots"
timer_task=""
i=0
while [ -z "$timer_task" ]; do
  timer_task=$($ub bot events bot --limit 40 | tr -d '\n' | sed 's/}, *{/}\n{/g' | grep '"bot.trigger.fired"' | grep '"type": *"timer"' | sed -n 's/.*"taskId": *"\(task-[^"]*\)".*/\1/p' | tail -1)
  i=$((i + 1)); [ $i -gt 50 ] && { echo "the timer did not fire"; break; }
  [ -z "$timer_task" ] && sleep 0.2
done
echo "timer task: $timer_task"
wait_for_task "$webhook_task"
wait_for_task "$timer_task"
for t in "$webhook_task" "$timer_task"; do
  $ub task get "$t" | grep -E '"id"|"objective"|"status"|"idempotencyKey"|"correlationId"|"type"|"name"' | head -8
  $ub task executions "$t" | grep -E '"id"|"outcome"|"nodeId"|"traceId"|"carried"|"costUsd"' | head -6
  echo "report:"; $ub task report "$t" | sed 's/^/    /'
done
echo "timer state (the next fire is persisted):"; grep -E '"schedule"|"nextFireAt"|"lastScheduledAt"|"lastTaskId"' "$state/timers/bot~clock.json"

say "8. restart both services: the timer fires nothing twice, the webhook delivery stays one task"
$ctl restart axlr-bots.service axlr-node.service
wait_for_socket
wait_for_node
sleep 3
echo "timer tasks of the bot: $(grep -l '"idempotencyKey": "timer:clock:' "$state"/tasks/*.json | wc -l) (one slot, one task)"
echo "tasks for delivery deploy-$stamp: $(grep -l "\"id\": \"$webhook_task\"" "$state"/tasks/*.json | wc -l)"
echo "trigger fires in the bot's events: $($ub bot events bot --limit 40 | tr -d '\n' | sed 's/}, *{/}\n{/g' | grep -c '"bot.trigger.fired"') (three: the webhook, its redelivery, the timer)"
journalctl --user -u axlr-bots.service -n 4 --no-pager -o cat | grep -E 'serving|timer|token' || true

say "installed and running"
echo "check:     $ctl status axlr-bots.service axlr-node.service"
echo "uninstall: $prefix/bin/axlr-bots uninstall [--purge]"
