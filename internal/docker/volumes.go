package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"berth-agent/internal/config"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
)

func ListVolumesHandler(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := context.Background()
		cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to create Docker client: %v", err), http.StatusInternalServerError)
			return
		}
		defer cli.Close()

		volumes, err := cli.VolumeList(ctx, volume.ListOptions{})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to list volumes: %v", err), http.StatusInternalServerError)
			return
		}

		var volumeInfos []VolumeInfo
		for _, vol := range volumes.Volumes {
			volumeInfos = append(volumeInfos, VolumeInfo{
				Name:       vol.Name,
				Driver:     vol.Driver,
				Mountpoint: vol.Mountpoint,
				Labels:     vol.Labels,
				Scope:      vol.Scope,
				CreatedAt:  vol.CreatedAt,
				Status:     vol.Status,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(volumeInfos)
	}
}

func DeleteVolumeHandler(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		volumeName := r.PathValue("volumeName")
		if volumeName == "" {
			http.Error(w, "Volume name is required", http.StatusBadRequest)
			return
		}

		ctx := context.Background()
		cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to create Docker client: %v", err), http.StatusInternalServerError)
			return
		}
		defer cli.Close()

		force := r.URL.Query().Get("force") == "true"

		err = cli.VolumeRemove(ctx, volumeName, force)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to delete volume: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "volume": volumeName})
	}
}

func PruneVolumesHandler(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := context.Background()
		cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to create Docker client: %v", err), http.StatusInternalServerError)
			return
		}
		defer cli.Close()

		report, err := cli.VolumesPrune(ctx, filters.Args{})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to prune volumes: %v", err), http.StatusInternalServerError)
			return
		}

		result := PruneResult{
			SpaceReclaimed: report.SpaceReclaimed,
			ImagesDeleted:  report.VolumesDeleted,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}
