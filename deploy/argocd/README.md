# Argo CD bootstrap

Two equivalent ways to deploy this app via Argo CD:

## Option A — discrete Applications (recommended for portfolio clarity)

```bash
oc apply -f deploy/argocd/application-dev.yaml      # dev: auto-sync, auto-prune, self-heal
oc apply -f deploy/argocd/application-prod.yaml     # prod: auto-sync, no prune, no self-heal
```

Result: two Argo CD `Application` resources, each pinned to one `values-<env>.yaml`.

## Option B — single ApplicationSet

```bash
oc apply -f deploy/argocd/applicationset.yaml
```

Fans out the same template per environment via a `list` generator. Pick **one** of A or B; combining them produces duplicate Applications.

## What Argo CD watches

| Source path | Why it's there |
|---|---|
| `deploy/helm/backstage-mcp-server/` | Helm chart base (Phase 3) |
| `values.yaml` | Shared base values |
| `values-dev.yaml` | Rewritten by the Tekton pipeline on every green build of `main` |
| `values-prod.yaml` | Hand-edited via promotion PR |

The dev Application has `automated.selfHeal: true` so any drift (an `oc edit` on the live Deployment, a deleted ConfigMap) is reconciled back to the chart. Prod sets `selfHeal: false` so unexpected drift surfaces an `OutOfSync` status instead of being silently overwritten.
