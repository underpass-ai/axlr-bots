#!/usr/bin/env bash
# The orchestra on a cluster, from nothing to a cue: installs the chart and
# makes what it does not hold, from the control plane it starts.
#
#   TYPESAFE_API_KEY=… bootstrap.sh [--namespace orchestra] [--release orchestra]
#                                    [--values file.yaml] [--enable ob-1,vc-1]
#
# Run it where kubectl and helm reach the cluster, from the repository. In
# order: the chart (the broker waits for the fleet CA, the score service for
# the operator token, the nodes for their tokens); Jev's key as a Secret;
# the fleet CA as a ConfigMap (the nodes pin it, the broker checks clients
# against it); the operator token as a Secret; one enrollment token per
# chair not yet enrolled; the 36 chairs registered as Jev bots
# (chairs/chair-jev.json); the chairs named by --enable enabled. Secrets are
# piped, never printed or put on a command line. Running it again changes
# only what is missing.
set -euo pipefail

ns=orchestra release=orchestra values=() enable=""
while [ $# -gt 0 ]; do
  case $1 in
    --namespace) ns=$2; shift 2 ;;
    --release) release=$2; shift 2 ;;
    --values) values+=(-f "$2"); shift 2 ;;
    --enable) enable=$2; shift 2 ;;
    *) echo "usage: bootstrap.sh [--namespace ns] [--release name] [--values file] [--enable chair,chair]" >&2; exit 2 ;;
  esac
done

here=$(cd "$(dirname "$0")" && pwd)
orchestra=$here/../../orchestra
cp=$release-cp
k() { kubectl -n "$ns" "$@"; }
in_cp() { k exec -i "$cp-0" -c control-plane -- "$@"; }
cli() { in_cp /usr/local/bin/axlr-bots --state /var/lib/axlr-bots "$@"; }

kubectl get namespace "$ns" >/dev/null 2>&1 || kubectl create namespace "$ns"

echo "1. Jev's key"
if ! k get secret orchestra-jev >/dev/null 2>&1; then
  : "${TYPESAFE_API_KEY:?TYPESAFE_API_KEY is needed once, for the Secret orchestra-jev}"
  printf 'TYPESAFE_API_KEY=%s\n' "$TYPESAFE_API_KEY" | k create secret generic orchestra-jev --from-env-file=/dev/stdin >/dev/null
fi

echo "2. The chart"
helm dependency update "$here" >/dev/null
helm upgrade --install "$release" "$here" -n "$ns" "${values[@]}" >/dev/null
k rollout status "statefulset/$cp" --timeout=180s

echo "3. The fleet CA, for the nodes and the broker"
if ! k get configmap orchestra-fleet-ca >/dev/null 2>&1; then
  cli ca show | python3 -c 'import json,sys; sys.stdout.write(json.load(sys.stdin)["certificate"])' \
    | k create configmap orchestra-fleet-ca --from-file=ca.crt=/dev/stdin >/dev/null
fi

echo "4. The operator's token, for the score service"
if ! k get secret orchestra-operator >/dev/null 2>&1; then
  in_cp /busybox/cat /var/lib/axlr-bots/api.token | k create secret generic orchestra-operator --from-file=token=/dev/stdin >/dev/null
fi

echo "5. One enrollment token per chair not yet enrolled"
chairs=$(python3 -c 'import json,sys; c=json.load(open(sys.argv[1])); print(" ".join(f"{s["id"]}-{n}" for s in c["sections"] for n in range(1, s["chairs"]+1)))' \
  "$orchestra/score-service/config.json")
known=$(cli node list | python3 -c 'import json,sys; print(" ".join(n["id"] for n in json.load(sys.stdin)))')
tokens=$(mktemp -d)
trap 'rm -rf "$tokens"' EXIT
for chair in $chairs; do
  node=stage-$chair
  case " $known " in *" $node "*) continue ;; esac
  cli node enroll-token --node-id "$node" --labels team=orchestra --ttl 24h \
    | python3 -c 'import json,sys; sys.stdout.write(json.load(sys.stdin)["token"])' >"$tokens/$node"
done
if [ -n "$(ls "$tokens")" ]; then
  k create secret generic orchestra-enrollment --from-file="$tokens" --dry-run=client -o yaml | k apply -f - >/dev/null
  echo "   $(ls "$tokens" | wc -l) tokens (24 h)"
fi

echo "6. The chairs as Jev bots"
bots=$(mktemp -d)
ORCHESTRA_RUNTIME=jev python3 "$orchestra/chairs/chairs.py" render "$bots" >/dev/null
registered=$(cli bot list | python3 -c 'import json,sys; print(" ".join(b["id"] for b in json.load(sys.stdin)))')
for file in "$bots"/*.json; do
  id=$(basename "$file" .json)
  case " $registered " in *" $id "*) continue ;; esac
  in_cp /busybox/sh -c 'cat > /tmp/bot.json' <"$file"
  cli bot register --file /tmp/bot.json >/dev/null
done
rm -rf "$bots"

if [ -n "$enable" ]; then
  echo "7. Enabled: $enable"
  for id in ${enable//,/ }; do cli bot enable "$id" >/dev/null; done
fi

echo "Done. The nodes enroll within a minute; then:"
echo "  kubectl -n $ns exec $cp-0 -c control-plane -- axlr-bots --state /var/lib/axlr-bots node list"
