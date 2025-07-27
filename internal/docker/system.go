package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"berth-agent/internal/config"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

func PruneBuildCacheHandler(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := context.Background()
		cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to create Docker client: %v", err), http.StatusInternalServerError)
			return
		}
		defer cli.Close()

		all := r.URL.Query().Get("all") == "true"
		keepStorage := r.URL.Query().Get("keep-storage")

		opts := types.BuildCachePruneOptions{
			All: all,
		}

		if keepStorage != "" {
			if bytes, err := strconv.ParseInt(keepStorage, 10, 64); err == nil {
				opts.KeepStorage = bytes
			}
		}

		report, err := cli.BuildCachePrune(ctx, opts)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to prune build cache: %v", err), http.StatusInternalServerError)
			return
		}

		result := PruneResult{
			SpaceReclaimed: report.SpaceReclaimed,
			ImagesDeleted:  nil,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

func SystemPruneHandler(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := context.Background()
		cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to create Docker client: %v", err), http.StatusInternalServerError)
			return
		}
		defer cli.Close()

		all := r.URL.Query().Get("all") == "true"
		volumes := r.URL.Query().Get("volumes") == "true"

		pruneFilters := filters.NewArgs()

		containerReport, err := cli.ContainersPrune(ctx, pruneFilters)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to prune containers: %v", err), http.StatusInternalServerError)
			return
		}

		imagePruneFilters := filters.NewArgs()
		if !all {
			imagePruneFilters.Add("dangling", "true")
		}
		imageReport, err := cli.ImagesPrune(ctx, imagePruneFilters)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to prune images: %v", err), http.StatusInternalServerError)
			return
		}

		networkReport, err := cli.NetworksPrune(ctx, pruneFilters)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to prune networks: %v", err), http.StatusInternalServerError)
			return
		}

		var volumeReport types.VolumesPruneReport
		var volumesDeleted []string
		if volumes {
			volumeReport, err = cli.VolumesPrune(ctx, pruneFilters)
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to prune volumes: %v", err), http.StatusInternalServerError)
				return
			}
			volumesDeleted = volumeReport.VolumesDeleted
		}

		var imagesDeleted []string
		for _, img := range imageReport.ImagesDeleted {
			if img.Deleted != "" {
				imagesDeleted = append(imagesDeleted, img.Deleted)
			}
			if img.Untagged != "" {
				imagesDeleted = append(imagesDeleted, img.Untagged)
			}
		}

		totalSpaceReclaimed := containerReport.SpaceReclaimed + imageReport.SpaceReclaimed
		if volumes {
			totalSpaceReclaimed += volumeReport.SpaceReclaimed
		}

		result := SystemPruneResult{
			SpaceReclaimed:    totalSpaceReclaimed,
			ContainersDeleted: containerReport.ContainersDeleted,
			ImagesDeleted:     imagesDeleted,
			NetworksDeleted:   networkReport.NetworksDeleted,
			VolumesDeleted:    volumesDeleted,
			BuildCacheDeleted: nil,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

func GetSystemInfoHandler(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := context.Background()
		cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to create Docker client: %v", err), http.StatusInternalServerError)
			return
		}
		defer cli.Close()

		info, err := cli.Info(ctx)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to get system info: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
	}
}

func GetDiskUsageHandler(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := context.Background()
		cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to create Docker client: %v", err), http.StatusInternalServerError)
			return
		}
		defer cli.Close()

		diskUsage, err := cli.DiskUsage(ctx, types.DiskUsageOptions{})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to get disk usage: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(diskUsage)
	}
}
