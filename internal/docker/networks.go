package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"berth-agent/internal/config"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

func ListNetworksHandler(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := context.Background()
		cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to create Docker client: %v", err), http.StatusInternalServerError)
			return
		}
		defer cli.Close()

		networks, err := cli.NetworkList(ctx, types.NetworkListOptions{})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to list networks: %v", err), http.StatusInternalServerError)
			return
		}

		var networkInfos []NetworkInfo
		for _, net := range networks {
			networkInfos = append(networkInfos, NetworkInfo{
				ID:       net.ID,
				Name:     net.Name,
				Driver:   net.Driver,
				Scope:    net.Scope,
				Internal: net.Internal,
				Labels:   net.Labels,
				Created:  net.Created.Format(time.RFC3339),
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(networkInfos)
	}
}

func DeleteNetworkHandler(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		networkID := r.PathValue("networkID")
		if networkID == "" {
			http.Error(w, "Network ID is required", http.StatusBadRequest)
			return
		}

		ctx := context.Background()
		cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to create Docker client: %v", err), http.StatusInternalServerError)
			return
		}
		defer cli.Close()

		err = cli.NetworkRemove(ctx, networkID)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to delete network: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "network": networkID})
	}
}

func PruneNetworksHandler(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := context.Background()
		cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to create Docker client: %v", err), http.StatusInternalServerError)
			return
		}
		defer cli.Close()

		report, err := cli.NetworksPrune(ctx, filters.Args{})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to prune networks: %v", err), http.StatusInternalServerError)
			return
		}

		result := PruneResult{
			SpaceReclaimed: 0,
			ImagesDeleted:  report.NetworksDeleted,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}
