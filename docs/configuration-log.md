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
