# AKS Watcher

A lightweight dashboard that gives you a live overview of every AKS cluster in an Azure subscription. It queries the Azure Management API directly — no `kubectl`, no VPN, no private network access required.

## How it works

```
Browser
  └─► nginx (frontend container)
        ├─ serves the React SPA for all non-API paths
        └─► /aks-watcher/api/* proxied by the ingress to the Go backend
              └─► Azure Management API (via Service Principal)
                    └─► returns AKS cluster list
```

**Backend (`main.go`)** — A Go HTTP server that calls the Azure Resource Manager REST API to list AKS clusters. It uses `DefaultAzureCredential`, which works locally via `az login` and in Kubernetes via environment variables (Service Principal) — no code change needed between environments. Exposes two endpoints:

| Endpoint | Description |
|---|---|
| `GET /aks-watcher/api/clusters/summary` | Returns JSON array of all AKS clusters in scope |
| `GET /healthz` | Liveness/readiness probe — returns `ok` |

**Frontend (`frontend/`)** — A React + TypeScript single-page app built with Vite and styled with Tailwind CSS. It polls `/aks-watcher/api/clusters/summary` every 60 seconds and displays each cluster as a card showing name, region, Kubernetes version, power state, and provisioning state.

---

## Prerequisites

- An Azure subscription with at least one AKS cluster
- A Kubernetes cluster (bare-metal or cloud) with:
  - [nginx-ingress](https://kubernetes.github.io/ingress-nginx/) controller
  - [cert-manager](https://cert-manager.io/) with a `ClusterIssuer` named `letsencrypt-prod`
  - [ArgoCD](https://argo-cd.readthedocs.io/) (for GitOps deployment)
- An Azure Service Principal with `Reader` role on the target subscription

---

## Azure setup (one-time)

### 1. Create a Service Principal and grant it Reader access

```bash
az ad sp create-for-rbac \
  --name aks-watcher \
  --role Reader \
  --scopes /subscriptions/<SUBSCRIPTION_ID>
```

Save the output — you will need `appId`, `password`, and `tenant` in the next step.

### 2. Create the Kubernetes Secret

The secret is created directly on the cluster and is never stored in git.

```bash
kubectl create secret generic aks-watcher-azure-creds \
  --from-literal=AZURE_TENANT_ID=<tenant> \
  --from-literal=AZURE_CLIENT_ID=<appId> \
  --from-literal=AZURE_CLIENT_SECRET=<password> \
  -n aks-watcher
```

---

## Kubernetes setup

### 1. Fill in the placeholders

**`k8s/backend/configmap.yaml`** — set your subscription ID:
```yaml
AZURE_SUBSCRIPTION_ID: "<YOUR_SUBSCRIPTION_ID>"
# Optional: leave empty to scan the whole subscription
AZURE_RESOURCE_GROUP: ""
```

### 2. Register the ArgoCD Application

```bash
kubectl apply -f argocd/application.yaml -n argocd
```

ArgoCD will immediately sync the `k8s/` directory to the cluster, creating the namespace, deployments, services, and ingress. cert-manager will automatically provision a TLS certificate for the configured hostname.

---

## Using the pre-built images in another project

The images are published to GitHub Container Registry on every push to `main`:

| Image | Registry |
|---|---|
| Backend | `ghcr.io/alphanemoclaw/aks_watcher/backend:latest` |
| Frontend | `ghcr.io/alphanemoclaw/aks_watcher/frontend:latest` |

Tags available: `latest`, `sha-<short-sha>`, and semver (`1.2.3`, `1.2`, `1`) when a `v*.*.*` tag is pushed.

### Minimal docker-compose example

```yaml
services:
  backend:
    image: ghcr.io/alphanemoclaw/aks_watcher/backend:latest
    environment:
      AZURE_SUBSCRIPTION_ID: "<YOUR_SUBSCRIPTION_ID>"
      # MOCK_MODE: "true"   # uncomment to run without Azure credentials
    ports:
      - "8080:8080"

  frontend:
    image: ghcr.io/alphanemoclaw/aks_watcher/frontend:latest
    ports:
      - "80:80"
    # When not running behind a path-stripping ingress, set the API base URL:
    # The frontend is built with base '/aks-watcher/' — see note below.
```

> **Note on the frontend base path:** The image is built with Vite `base: '/aks-watcher/'`, meaning the app expects to be served at `/aks-watcher/`. If you serve it at a different path, rebuild the image with `--build-arg` or run locally with `npm run dev`.

### Running locally without Docker

```bash
# Terminal 1 — backend
AZURE_SUBSCRIPTION_ID=<sub-id> go run .
# Or without Azure credentials:
MOCK_MODE=true go run .

# Terminal 2 — frontend (Vite proxy rewrites /api → localhost:8080)
cd frontend
npm install
npm run dev
# Open http://localhost:5173/aks-watcher/
```

### Environment variables (backend)

| Variable | Required | Default | Description |
|---|---|---|---|
| `AZURE_SUBSCRIPTION_ID` | Yes* | — | Subscription to scan |
| `AZURE_RESOURCE_GROUP` | No | — | Restrict to one resource group |
| `AZURE_TENANT_ID` | Yes* | — | Service Principal tenant ID |
| `AZURE_CLIENT_ID` | Yes* | — | Service Principal app ID |
| `AZURE_CLIENT_SECRET` | Yes* | — | Service Principal password |
| `PORT` | No | `8080` | HTTP listen port |
| `MOCK_MODE` | No | `false` | Return fake data, skip Azure calls |

*Not required when `MOCK_MODE=true`.

---

## CI/CD

The repository uses GitHub Actions with an ArgoCD GitOps flow:

```
git push → CI builds & pushes image to GHCR
         → CI commits updated image SHA tag to k8s/<component>/deployment.yaml
         → ArgoCD detects the git change and syncs the cluster
```

Two workflows run independently, triggered only when their relevant files change:

- **`backend-ci.yml`** — triggered by changes to `*.go`, `go.mod`, `Dockerfile`
- **`frontend-ci.yml`** — triggered by changes to `frontend/**`

No cloud credentials are stored in GitHub — the image push uses the auto-provisioned `GITHUB_TOKEN` for GHCR.
