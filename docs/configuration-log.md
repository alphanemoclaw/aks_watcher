# Azure Workload Identity Setup Log

This document records the steps we took to configure Azure Workload Identity for the `aks-watcher` service. Sensitive information, such as the exact Subscription ID, Tenant ID, and Managed Identity Client ID, have been intentionally removed and replaced with placeholders to keep this secure.

## Environment Details

- **Target Azure Resource Group:** `aks_watcher`
- **Target AKS Cluster:** `aks_watcher_cluster`
- **Kubernetes Namespace:** `aks-watcher`
- **Kubernetes ServiceAccount:** `aks-watcher-backend`
- **Azure Managed Identity Name:** `aks-watcher-identity`

## Steps Executed

### 1. Enabled OIDC Issuer and Workload Identity
We first updated the AKS cluster to support Workload Identity and OIDC:
```bash
az aks update -g aks_watcher -n aks_watcher_cluster --enable-oidc-issuer --enable-workload-identity
```

### 2. Created the Managed Identity
We created a User-Assigned Managed Identity that the backend pods will use to authenticate to Azure:
```bash
az identity create -g aks_watcher -n aks-watcher-identity
```

### 3. Assigned the Reader Role
To allow the backend to read the AKS cluster list from the Azure Management API, we granted the Managed Identity the `Reader` role against the subscription:
```bash
az role assignment create --role Reader --assignee <CLIENT_ID> --scope /subscriptions/<SUBSCRIPTION_ID>
```

### 4. Created the Federated Credential
We established the trust relationship between the Azure Managed Identity and the Kubernetes ServiceAccount (`aks-watcher-backend`) using the cluster's OIDC issuer URL:
```bash
# 1. Fetched the OIDC Issuer URL:
# OIDC_ISSUER=$(az aks show -g aks_watcher -n aks_watcher_cluster --query oidcIssuerProfile.issuerURL -o tsv)

# 2. Linked the identities:
az identity federated-credential create \
  --name aks-watcher-backend \
  --identity-name aks-watcher-identity \
  --resource-group aks_watcher \
  --issuer <OIDC_ISSUER_URL> \
  --subject system:serviceaccount:aks-watcher:aks-watcher-backend
```

### 5. Configured the Kubernetes ServiceAccount
Finally, we annotated the ServiceAccount with the client ID (this is now templated with `<YOUR_MANAGED_IDENTITY_CLIENT_ID>` in `k8s/backend/serviceaccount.yaml` to hide our actual client ID).
