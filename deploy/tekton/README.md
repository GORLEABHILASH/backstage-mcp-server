# Tekton CI

This directory ships the Tekton Pipeline that builds and promotes
`backstage-mcp-server`.

## Pipeline shape

```
git-clone ──► golang-test ──► buildah ──► update-image-tag
                ▲                            │
                │                            └─► git push (values-dev.yaml)
            compute-tag (short SHA)                       │
                                                          ▼
                                                     Argo CD auto-sync
```

## Prerequisites

- A cluster with **Tekton Pipelines** installed
- (Optional, for webhooks) **Tekton Triggers** installed
- The two well-known catalog tasks installed in the pipeline namespace:
  ```bash
  tkn hub install task git-clone
  tkn hub install task buildah
  ```

## Install the pipeline

```bash
# Custom Tasks
oc apply -f deploy/tekton/tasks/golang-test.yaml
oc apply -f deploy/tekton/tasks/update-image-tag.yaml

# Pipeline + RBAC
oc apply -f deploy/tekton/pipeline.yaml
oc apply -f deploy/tekton/rbac.yaml

# Optional: webhook-driven runs
oc apply -f deploy/tekton/triggers.yaml
```

### Required Secrets

| Secret | Type | What it holds |
|---|---|---|
| `registry-creds` | `kubernetes.io/dockerconfigjson` | pull/push to your container registry |
| `git-credentials` | `kubernetes.io/basic-auth` | push the bump commit back to the GitOps branch |
| `github-webhook` | generic | shared secret matching the GitHub webhook (triggers only) |

```bash
oc create secret docker-registry registry-creds \
  --docker-server=ghcr.io --docker-username=$GH_USER --docker-password=$GH_TOKEN

oc create secret generic git-credentials \
  --type=kubernetes.io/basic-auth \
  --from-literal=username=$GH_USER --from-literal=password=$GH_TOKEN

oc annotate secret git-credentials tekton.dev/git-0=https://github.com
```

## Run the pipeline manually

```bash
oc create -f deploy/tekton/pipelinerun.yaml
tkn pr logs -f
```

## Webhook-driven runs (GitHub)

1. `oc apply -f deploy/tekton/triggers.yaml` — installs an EventListener
2. Expose the EventListener Service via a Route / Ingress
3. Add a webhook on the GitHub repo:
   - Payload URL: the EventListener's public URL
   - Content type: `application/json`
   - Secret: same value as the `github-webhook` Secret in the cluster
   - Events: **just** the `push` event

A push to `main` then spawns a PipelineRun, which on success commits a new image tag into `values-dev.yaml`. Argo CD's dev Application sees the commit and rolls the Deployment to the new image — fully automated, with the entire trail visible in git.
