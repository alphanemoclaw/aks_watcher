// Package main implements a REST API that queries the Azure Management plane
// to return a live summary of every AKS cluster in a subscription (or a
// single resource group), enriched with manually-managed live status data
// stored in a local SQLite database.
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
//   DB_PATH                 – path to the SQLite database file (default: /data/aks-watcher.db)
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v2"
	_ "modernc.org/sqlite"
)

// ────────────────────────────────────────────────────────────────────────────
// Domain types
// ────────────────────────────────────────────────────────────────────────────

// ClusterSummary is the per-cluster payload returned by the API.
type ClusterSummary struct {
	Name              string  `json:"name"`
	ResourceGroup     string  `json:"resource_group"`
	Location          string  `json:"location"`
	KubernetesVersion string  `json:"kubernetes_version"`
	ProvisioningState string  `json:"provisioning_state"`
	PowerState        string  `json:"power_state"`
	IsLive            bool    `json:"is_live"`
	SetLiveAt         *string `json:"set_live_at"`
	PlannedLiveAt     *string `json:"planned_live_at"`
}

// LiveStatusRequest is the payload for PUT /aks-watcher/api/clusters/live-status.
type LiveStatusRequest struct {
	ClusterName   string  `json:"cluster_name"`
	ResourceGroup string  `json:"resource_group"`
	IsLive        bool    `json:"is_live"`
	PlannedLiveAt *string `json:"planned_live_at"`
}

// ────────────────────────────────────────────────────────────────────────────
// SQLite helpers
// ────────────────────────────────────────────────────────────────────────────

func initDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS live_status (
			cluster_name    TEXT NOT NULL,
			resource_group  TEXT NOT NULL,
			is_live         INTEGER NOT NULL DEFAULT 0,
			set_live_at     TEXT,
			planned_live_at TEXT,
			PRIMARY KEY (cluster_name, resource_group)
		)
	`)
	if err != nil {
		return nil, fmt.Errorf("create table: %w", err)
	}

	return db, nil
}

// fetchLiveStatuses returns a map keyed by "cluster_name|resource_group".
func fetchLiveStatuses(db *sql.DB) (map[string]ClusterSummary, error) {
	rows, err := db.Query(`SELECT cluster_name, resource_group, is_live, set_live_at, planned_live_at FROM live_status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]ClusterSummary)
	for rows.Next() {
		var name, rg string
		var isLive int
		var setLiveAt, plannedLiveAt sql.NullString
		if err := rows.Scan(&name, &rg, &isLive, &setLiveAt, &plannedLiveAt); err != nil {
			return nil, err
		}
		s := ClusterSummary{IsLive: isLive == 1}
		if setLiveAt.Valid {
			s.SetLiveAt = &setLiveAt.String
		}
		if plannedLiveAt.Valid {
			s.PlannedLiveAt = &plannedLiveAt.String
		}
		out[name+"|"+rg] = s
	}
	return out, rows.Err()
}

// upsertLiveStatus inserts or updates a live_status row.
func upsertLiveStatus(db *sql.DB, req LiveStatusRequest) error {
	var setLiveAt *string

	// Fetch the existing row to preserve set_live_at if already set.
	var existing sql.NullString
	_ = db.QueryRow(
		`SELECT set_live_at FROM live_status WHERE cluster_name = ? AND resource_group = ?`,
		req.ClusterName, req.ResourceGroup,
	).Scan(&existing)

	if req.IsLive {
		if existing.Valid && existing.String != "" {
			// Already had a set_live_at — keep it.
			setLiveAt = &existing.String
		} else {
			// First time being marked live — record now.
			now := time.Now().UTC().Format(time.RFC3339)
			setLiveAt = &now
		}
	}
	// If is_live is false, set_live_at is cleared.

	_, err := db.Exec(`
		INSERT INTO live_status (cluster_name, resource_group, is_live, set_live_at, planned_live_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(cluster_name, resource_group) DO UPDATE SET
			is_live         = excluded.is_live,
			set_live_at     = excluded.set_live_at,
			planned_live_at = excluded.planned_live_at
	`, req.ClusterName, req.ResourceGroup, boolToInt(req.IsLive), setLiveAt, req.PlannedLiveAt)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ────────────────────────────────────────────────────────────────────────────
// Azure helpers
// ────────────────────────────────────────────────────────────────────────────

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

func listClusters(ctx context.Context, client *armcontainerservice.ManagedClustersClient, resourceGroup string) ([]ClusterSummary, error) {
	var summaries []ClusterSummary

	if resourceGroup != "" {
		pager := client.NewListByResourceGroupPager(resourceGroup, nil)
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

func deref(s *string, fallback string) string {
	if s == nil {
		return fallback
	}
	return *s
}

func resourceGroupFromID(id string) string {
	const marker = "resourcegroups"
	i := 0
	for i < len(id) {
		j := i
		for j < len(id) && id[j] != '/' {
			j++
		}
		segment := id[i:j]
		if equalFold(segment, marker) && j+1 < len(id) {
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

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type handler struct {
	client        *armcontainerservice.ManagedClustersClient
	resourceGroup string
	mockMode      bool
	db            *sql.DB
}

var mockClusters = []ClusterSummary{
	{Name: "prod-aks-eastus", ResourceGroup: "rg-production", Location: "eastus", KubernetesVersion: "1.29.2", ProvisioningState: "Succeeded", PowerState: "Running"},
	{Name: "staging-aks-westeu", ResourceGroup: "rg-staging", Location: "westeurope", KubernetesVersion: "1.28.5", ProvisioningState: "Succeeded", PowerState: "Running"},
	{Name: "dev-aks-eastus", ResourceGroup: "rg-development", Location: "eastus", KubernetesVersion: "1.28.5", ProvisioningState: "Succeeded", PowerState: "Stopped"},
	{Name: "qa-aks-centralus", ResourceGroup: "rg-qa", Location: "centralus", KubernetesVersion: "1.27.9", ProvisioningState: "Failed", PowerState: "Running"},
}

// handleClustersSummary merges Azure cluster data with live status from SQLite.
//
//	GET /aks-watcher/api/clusters/summary
func (h *handler) handleClustersSummary(w http.ResponseWriter, r *http.Request) {
	var clusters []ClusterSummary

	if h.mockMode {
		log.Println("GET /aks-watcher/api/clusters/summary [MOCK]")
		clusters = make([]ClusterSummary, len(mockClusters))
		copy(clusters, mockClusters)
	} else {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		var err error
		clusters, err = listClusters(ctx, h.client, h.resourceGroup)
		if err != nil {
			log.Printf("GET /aks-watcher/api/clusters/summary: %v", err)
			http.Error(w, `{"error":"failed to list clusters"}`, http.StatusInternalServerError)
			return
		}
	}

	if clusters == nil {
		clusters = []ClusterSummary{}
	}

	// Enrich with live status from SQLite.
	statuses, err := fetchLiveStatuses(h.db)
	if err != nil {
		log.Printf("fetchLiveStatuses: %v", err)
		// Non-fatal: return clusters without live status rather than failing.
	} else {
		for i, c := range clusters {
			if s, ok := statuses[c.Name+"|"+c.ResourceGroup]; ok {
				clusters[i].IsLive = s.IsLive
				clusters[i].SetLiveAt = s.SetLiveAt
				clusters[i].PlannedLiveAt = s.PlannedLiveAt
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(clusters); err != nil {
		log.Printf("encode clusters: %v", err)
	}
}

// handleLiveStatus upserts the live status for a single cluster.
//
//	PUT /aks-watcher/api/clusters/live-status
func (h *handler) handleLiveStatus(w http.ResponseWriter, r *http.Request) {
	var req LiveStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if req.ClusterName == "" || req.ResourceGroup == "" {
		http.Error(w, `{"error":"cluster_name and resource_group are required"}`, http.StatusBadRequest)
		return
	}

	if err := upsertLiveStatus(h.db, req); err != nil {
		log.Printf("upsertLiveStatus: %v", err)
		http.Error(w, `{"error":"failed to save live status"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ────────────────────────────────────────────────────────────────────────────
// Entry point
// ────────────────────────────────────────────────────────────────────────────

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./aks-watcher.db"
	}

	mockMode := os.Getenv("MOCK_MODE") == "true"

	db, err := initDB(dbPath)
	if err != nil {
		log.Fatalf("init db: %v", err)
	}
	defer db.Close()

	h := &handler{mockMode: mockMode, db: db}

	if mockMode {
		log.Printf("server: listening on :%s | mode: MOCK (no Azure calls) | db: %s", port, dbPath)
	} else {
		subscriptionID := os.Getenv("AZURE_SUBSCRIPTION_ID")
		if subscriptionID == "" {
			log.Fatal("AZURE_SUBSCRIPTION_ID is required (or set MOCK_MODE=true)")
		}
		resourceGroup := os.Getenv("AZURE_RESOURCE_GROUP")

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
		log.Printf("server: listening on :%s | mode: LIVE | scope: %s | db: %s", port, scope, dbPath)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /aks-watcher/api/clusters/summary", h.handleClustersSummary)
	mux.HandleFunc("PUT /aks-watcher/api/clusters/live-status", h.handleLiveStatus)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	})

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      corsMiddleware(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 35 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server: %v", err)
	}
}
