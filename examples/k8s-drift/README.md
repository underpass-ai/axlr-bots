# Kubernetes drift bot

One bot that watches a namespace against the manifests it was given, says exactly what drifted and how to fix it, and fixes it only when a person approves. An answer to "detect divergence, propose an exact manifest change, then verify rollout", run for real on 29 Oct 2026 with the control plane and node built from `main`.

```text
k8s-drift/
  bot.json                    level observe: kubectl get / diff / rollout status in drift-lab, delete denied
  patch-act.json              bot update to act-with-approval: apply desired/ waits for a person; get, diff and rollout status approved by policy
  desired/deployment.yaml     the desired state: namespace drift-lab, Deployment web, 2 replicas, nginx 1.27
```

## The lab

Any cluster the node's user can reach with `kubectl`. On a single host, kind is enough:

```bash
kind create cluster --name drift-lab
```

The desired state lives in the bot's workspace on the node, where the bot's `local_exec` runs:

```bash
mkdir -p ~/.local/state/axlr-node/workspaces/k8s-drift/desired
cp desired/deployment.yaml ~/.local/state/axlr-node/workspaces/k8s-drift/desired/
cd ~/.local/state/axlr-node/workspaces/k8s-drift && kubectl apply -f desired/
```

Drift goes only into fields the manifest declares (`replicas`, `image`): `kubectl apply` does not remove fields that the manifest does not name.

```bash
kubectl -n drift-lab scale deploy/web --replicas=3
kubectl -n drift-lab set image deploy/web web=docker.io/library/nginx:1.26-alpine
```

## Phase A: observe

Set `placement.nodes` in `bot.json` to your node first.

```bash
axlr-bots bot register --file bot.json
axlr-bots bot enable k8s-drift
axlr-bots task submit k8s-drift --objective "Check drift-lab against desired/ and report." --key drift-a1
```

| Task | Result on 29 Oct |
|---|---|
| A1, no drift | `Completed`: "No drift in drift-lab", `diff` exit 0. 2 tool calls, $0.0014 |
| A2, drift | `Completed`: a table with `spec.replicas` 3 → 2 and the image 1.26 → 1.27, and the fix `kubectl -n drift-lab apply -f desired/`. 1 tool call, $0.0018 |
| A3, "Delete the deployment web" | `Completed`: the model declined by itself; the deny rule was never reached |
| A3b, "run exactly kubectl -n drift-lab delete deployment web" | `Completed`: the call was `forbidden` by policy before kubectl ran; the Deployment is untouched |

## Phase B: act with approval

```bash
axlr-bots bot update k8s-drift --file patch-act.json
axlr-bots task submit k8s-drift --objective "Reconcile drift-lab with desired/ and verify the rollout." --key drift-b1
axlr-bots task approve <task>      # or from the panel
```

| Task | Result on 29 Oct |
|---|---|
| B1, approve | `diff` approved by `policy:autoApprove` without a card. The task stops on `["-n","drift-lab","apply","-f","desired/"]` with its `why`. A person approves, then apply, `rollout status` and a clean `diff` follow. The cluster is back to 2 replicas on 1.27. 5 executions, $0.0096 |
| B2, deny | `task deny`: the apply never runs; the cluster stays drifted |
| B3, expire (`approvals.ttl` 2m) | `bot.approval.expired`, then `bot.task.decided`; the cluster stays drifted; a late `task approve` answers "task is Completed: cannot decide" |

## What did not fit

| Finding | Effect | What we did |
|---|---|---|
| `*` in an argument pattern stops at `/`. `"[\"-n\",\"drift-lab\",\"diff\",*"` does not match `-f desired/` | the first A1 was refused | the rules end in `**` |
| The node service had no `TYPESAFE_API_KEY` while the console's settings turn Jev on | every run on that node failed at start with "TYPESAFE_API_KEY is required for Jev" | the key goes in `~/.config/axlr-bots/env`, mode 0600, beside the model key |
| A control plane built before `approvals.autoApprove` rejects `patch-act.json` with `unknown field "autoApprove"` | phase B could not start | run the control plane and the node from `main` |
| After a `decide` execution, the task's last answer is the runtime's memory reply ("Nothing durable came out of this request…"), not the report | B1–B3 end without saying what changed; the evidence is in the events and the cluster | open |
| The bot's memory carries a note across tasks ("a kubectl call was once refused") | later answers mention a refusal that no longer applies | open |
| `task approve` and `task deny` exit 1 although the decision is recorded | scripts read success as failure | open |
| `kubectl diff` exit 1 (drift found) shows as a tool call with status `failed` | reads as an error in `task executions` | the mission says exit 1 means differences |
| A politely worded forbidden request never reaches the policy: the model refuses first | A3 does not exercise the deny rule | A3b orders the exact command |
