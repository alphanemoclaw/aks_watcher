// Package main implements a REST API that queries the Azure Management plane
// to return a live summary of every AKS cluster in a subscription (or a
// single resource group).
//
// Because this uses standard Azure REST list operations — not kubectl or
// Run Command — responses are fast (< 1 s) and require no VPN or private
// network access.
//
// Required environment variables
// ───────────────────────────────
//   AZURE_SUBSCRIPTION_ID   – Azure subscription to scan
//
// Optional environment variables
// ───────────────────────────────
//   AZURE_RESOURCE_GROUP    – restrict results to one resource group
//   PORT                    – HTTP listen port (default: 8080)
//   MOCK_MODE               – set to "true" to return fake cluster data
//                             (skips all Azure API calls; useful for local UI dev)
//
// Authentication (DefaultAzureCredential tries these in order)
// ─────────────────────────────────────────────────────────────
//   Local  : environment variables → Azure CLI → VS Code → Azure PowerShell
//   Azure  : Workload Identity → Managed Identity
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v2"
)

// ────────────────────────────────────────────────────────────────────────────
// Domain types
// ────────────────────────────────────────────────────────────────────────────

// ClusterSummary is the per-cluster payload returned by the API.
// Field names use snake_case JSON keys for easy consumption by a React frontend.
type ClusterSummary struct {
	// Name is the Azure resource name of the cluster.
	Name string `json:"name"`

	// ResourceGroup is the resource group the cluster belongs to.
	ResourceGroup string `json:"resource_group"`

	// Location is the Azure region (e.g. "eastus", "westeurope").
	Location string `json:"location"`

	// KubernetesVersion is the version reported by the control plane
	// (e.g. "1.29.2").
	KubernetesVersion string `json:"kubernetes_version"`

	// ProvisioningState reflects the last ARM operation result
	// (e.g. "Succeeded", "Failed", "Updating").
	ProvisioningState string `json:"provisioning_state"`

	// PowerState reflects whether the cluster is running or deallocated
	// (e.g. "Running", "Stopped").
	PowerState string `json:"power_state"`
}

// ────────────────────────────────────────────────────────────────────────────
// Azure helpers
// ────────────────────────────────────────────────────────────────────────────

// newManagedClustersClient creates an authenticated armcontainerservice client.
// DefaultAzureCredential works both locally (via az login) and in Azure
// (via Managed Identity / Workload Identity) without any code change.
func newManagedClustersClient(subscriptionID string) (*armcontainerservice.ManagedClustersClient, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("create credential: %w", err)
	}

	client, err := armcontainerservice.NewManagedClustersClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("create ManagedClusters client: %w", err)
	}

	return client, nil
}

// listClusters fetches all AKS clusters visible to the credential.
// If resourceGroup is non-empty, only clusters in that group are returned;
// otherwise all clusters in the subscription are returned.
func listClusters(ctx context.Context, client *armcontainerservice.ManagedClustersClient, resourceGroup string) ([]ClusterSummary, error) {
	var summaries []ClusterSummary

	// Choose the pager based on whether a resource group filter is active.
	// Both pagers return the same *armcontainerservice.ManagedCluster objects,
	// so the extraction logic below is shared.
	var pager interface {
		More() bool
		NextPage(context.Context) (armcontainerservice.ManagedClustersClientListByResourceGroupResponse, error)
	}

	if resourceGroup != "" {
		pager = client.NewListByResourceGroupPager(resourceGroup, nil)
	} else {
		// NewListPager returns a different concrete type, handle both branches
		// with separate loops to keep type safety without reflection.
		subPager := client.NewListPager(nil)
		for subPager.More() {
			page, err := subPager.NextPage(ctx)
			if err != nil {
				return nil, fmt.Errorf("list clusters page: %w", err)
			}
			for _, c := range page.Value {
				summaries = append(summaries, extractSummary(c))
			}
		}
		return summaries, nil
	}

	// Resource-group-scoped pager path.
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list clusters by resource group page: %w", err)
		}
		for _, c := range page.Value {
			summaries = append(summaries, extractSummary(c))
		}
	}

	return summaries, nil
}

// extractSummary maps an armcontainerservice.ManagedCluster to a ClusterSummary.
// All pointer dereferences are nil-safe; unknown fields fall back to "unknown".
func extractSummary(c *armcontainerservice.ManagedCluster) ClusterSummary {
	s := ClusterSummary{
		Name:              deref(c.Name, "unknown"),
		Location:          deref(c.Location, "unknown"),
		ResourceGroup:     resourceGroupFromID(deref(c.ID, "")),
		KubernetesVersion: "unknown",
		ProvisioningState: "unknown",
		PowerState:        "unknown",
	}

	if c.Properties != nil {
		s.KubernetesVersion = deref(c.Properties.KubernetesVersion, "unknown")
		s.ProvisioningState = deref(c.Properties.ProvisioningState, "unknown")

		if c.Properties.PowerState != nil && c.Properties.PowerState.Code != nil {
			s.PowerState = string(*c.Properties.PowerState.Code)
		}
	}

	return s
}

// deref safely dereferences a *string, returning fallback when nil.
func deref(s *string, fallback string) string {
	if s == nil {
		return fallback
	}
	return *s
}

// resourceGroupFromID extracts the resource group name from a full Azure
// resource ID string.
// Example ID: /subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.ContainerService/managedClusters/{name}
func resourceGroupFromID(id string) string {
	// Walk the path segments looking for "resourcegroups" (case-insensitive).
	const marker = "resourcegroups"
	i := 0
	for i < len(id) {
		// Find next slash
		j := i
		for j < len(id) && id[j] != '/' {
			j++
		}
		segment := id[i:j]
		if equalFold(segment, marker) && j+1 < len(id) {
			// The segment after the marker is the resource group name.
			start := j + 1
			end := start
			for end < len(id) && id[end] != '/' {
				end++
			}
			return id[start:end]
		}
		i = j + 1
	}
	return "unknown"
}

// equalFold is a simple ASCII case-insensitive comparison to avoid importing strings.
func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// ────────────────────────────────────────────────────────────────────────────
// HTTP server
// ────────────────────────────────────────────────────────────────────────────

// corsMiddleware adds CORS headers so a React dev server (or any origin) can
// call this API.  Restrict Access-Control-Allow-Origin in production.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		// Browsers send a pre-flight OPTIONS before every cross-origin request.
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// handler bundles the dependencies needed by HTTP handlers.
type handler struct {
	client        *armcontainerservice.ManagedClustersClient
	resourceGroup string // empty → all resource groups in the subscription
	mockMode      bool   // when true, return fake data instead of calling Azure
}

// mockClusters is the fake dataset returned when MOCK_MODE=true.
// Edit these values to match your real environment's shape.
var mockClusters = []ClusterSummary{
	{
		Name:              "prod-aks-eastus",
		ResourceGroup:     "rg-production",
		Location:          "eastus",
		KubernetesVersion: "1.29.2",
		ProvisioningState: "Succeeded",
		PowerState:        "Running",
	},
	{
		Name:              "staging-aks-westeu",
		ResourceGroup:     "rg-staging",
		Location:          "westeurope",
		KubernetesVersion: "1.28.5",
		ProvisioningState: "Succeeded",
		PowerState:        "Running",
	},
	{
		Name:              "dev-aks-eastus",
		ResourceGroup:     "rg-development",
		Location:          "eastus",
		KubernetesVersion: "1.28.5",
		ProvisioningState: "Succeeded",
		PowerState:        "Stopped",
	},
	{
		Name:              "qa-aks-centralus",
		ResourceGroup:     "rg-qa",
		Location:          "centralus",
		KubernetesVersion: "1.27.9",
		ProvisioningState: "Failed",
		PowerState:        "Running",
	},
}

// handleClustersSummary is the main endpoint.
//
//	GET /api/clusters/summary
//
// Calls the Azure Management API live on each request.  The list operation
// is fast (< 1 s for most subscriptions) so no caching layer is needed.
func (h *handler) handleClustersSummary(w http.ResponseWriter, r *http.Request) {
	// In mock mode skip all Azure calls and return the hardcoded dataset.
	// Flip back to real mode by unsetting MOCK_MODE.
	if h.mockMode {
		log.Println("GET /api/clusters/summary [MOCK]")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockClusters)
		return
	}

	// Use a 30-second timeout — Azure list APIs are fast but we want a hard cap.
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	clusters, err := listClusters(ctx, h.client, h.resourceGroup)
	if err != nil {
		log.Printf("GET /api/clusters/summary: %v", err)
		http.Error(w, `{"error":"failed to list clusters"}`, http.StatusInternalServerError)
		return
	}

	// Return an empty array rather than null when there are no clusters.
	if clusters == nil {
		clusters = []ClusterSummary{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(clusters); err != nil {
		log.Printf("GET /api/clusters/summary: encode: %v", err)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Entry point
// ────────────────────────────────────────────────────────────────────────────

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mockMode := os.Getenv("MOCK_MODE") == "true"

	h := &handler{mockMode: mockMode}

	if mockMode {
		// Skip all Azure initialisation — no credentials or subscription needed.
		log.Printf("server: listening on :%s | mode: MOCK (no Azure calls)", port)
	} else {
		subscriptionID := os.Getenv("AZURE_SUBSCRIPTION_ID")
		if subscriptionID == "" {
			log.Fatal("AZURE_SUBSCRIPTION_ID is required (or set MOCK_MODE=true)")
		}

		resourceGroup := os.Getenv("AZURE_RESOURCE_GROUP") // optional

		client, err := newManagedClustersClient(subscriptionID)
		if err != nil {
			log.Fatalf("init: %v", err)
		}

		h.client = client
		h.resourceGroup = resourceGroup

		scope := "subscription " + subscriptionID
		if resourceGroup != "" {
			scope = "resource group " + resourceGroup
		}
		log.Printf("server: listening on :%s | mode: LIVE | scope: %s", port, scope)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/clusters/summary", h.handleClustersSummary)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	})

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      corsMiddleware(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 35 * time.Second, // slightly longer than the Azure call timeout
		IdleTimeout:  120 * time.Second,
	}

	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server: %v", err)
	}
}
