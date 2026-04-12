# Azure Authentication Setup Log

This document records the steps taken to configure Azure authentication for the `aks-watcher` service running on a bare-metal Kubernetes cluster.

> **Note:** Workload Identity is AKS-specific and cannot be used on bare-metal clusters. The app uses a Service Principal with credentials injected via a Kubernetes Secret instead.

## Environment Details

- **Target Azure Resource Group:** `aks_watcher`
- **Observed AKS Cluster:** `aks_watcher_cluster`
- **Kubernetes Namespace:** `aks-watcher`
- **Hosting:** Bare-metal Kubernetes cluster (not AKS)

## Steps Executed

### 1. Created a Service Principal with Reader access

```bash
az ad sp create-for-rbac \
  --name aks-watcher \
  --role Reader \
  --scopes /subscriptions/<SUBSCRIPTION_ID>
```

This outputs `appId`, `password`, and `tenant` — these are the credentials used by the backend.

### 2. Created the Kubernetes Secret

The credentials are stored as a Kubernetes Secret and injected into the backend pod as environment variables. The secret is never committed to git.

```bash
kubectl create secret generic aks-watcher-azure-creds \
  --from-literal=AZURE_TENANT_ID=<tenant> \
  --from-literal=AZURE_CLIENT_ID=<appId> \
  --from-literal=AZURE_CLIENT_SECRET=<password> \
  -n aks-watcher
```

### 3. How it works at runtime

The backend uses `DefaultAzureCredential` from the Azure SDK. When `AZURE_TENANT_ID`, `AZURE_CLIENT_ID`, and `AZURE_CLIENT_SECRET` are present as environment variables, it automatically uses `EnvironmentCredential` (Service Principal auth) without any code change.

---

## Live Status Feature

Clusters can be manually marked as live (actively serving production traffic) through the dashboard UI. This data is not available from the Azure API and is managed locally.

### Storage

Live status is stored in a SQLite database (`modernc.org/sqlite` — pure Go, no CGo required). The database file is created automatically on first startup.

| Environment | DB location |
|---|---|
| Local development | `./aks-watcher.db` (current directory) |
| Kubernetes | `/data/aks-watcher.db` (mounted PersistentVolume) |

### Schema

```sql
CREATE TABLE live_status (
    cluster_name    TEXT NOT NULL,
    resource_group  TEXT NOT NULL,
    is_live         INTEGER NOT NULL DEFAULT 0,
    set_live_at     TEXT,
    planned_live_at TEXT,
    PRIMARY KEY (cluster_name, resource_group)
)
```

### Kubernetes PersistentVolumeClaim

The PVC is declared in `k8s/backend/pvc.yaml` and created automatically by ArgoCD on deploy. It requests 100Mi of storage with `ReadWriteOnce` access mode.

```bash
# Verify the PVC is bound after ArgoCD syncs
kubectl get pvc -n aks-watcher
```

### How the UI works

- Click any cluster card to open the live status modal
- Toggle "Production live" on/off
- Optionally set a planned go-live date
- "Set live on" date is recorded automatically the first time a cluster is marked live and never overwritten
- Saving calls `PUT /aks-watcher/api/clusters/live-status` and the card refreshes immediately
