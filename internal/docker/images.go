package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"berth-agent/internal/config"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

func ListImagesHandler(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := context.Background()
		cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to create Docker client: %v", err), http.StatusInternalServerError)
			return
		}
		defer cli.Close()

		all := r.URL.Query().Get("all") == "true"

		images, err := cli.ImageList(ctx, types.ImageListOptions{
			All: all,
		})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to list images: %v", err), http.StatusInternalServerError)
			return
		}

		containers, err := cli.ContainerList(ctx, types.ContainerListOptions{
			All: true,
		})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to list containers: %v", err), http.StatusInternalServerError)
			return
		}

		imageUsage := make(map[string]int)
		for _, container := range containers {
			imageUsage[container.ImageID]++
		}

		var imageInfos []ImageInfo
		for _, img := range images {

			containerCount := imageUsage[img.ID]

			imageInfos = append(imageInfos, ImageInfo{
				ID:          img.ID,
				RepoTags:    img.RepoTags,
				RepoDigests: img.RepoDigests,
				Size:        img.Size,
				Created:     img.Created,
				Labels:      img.Labels,
				Containers:  int64(containerCount),
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(imageInfos)
	}
}

func DeleteImageHandler(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		imageID := r.PathValue("imageID")
		if imageID == "" {
			http.Error(w, "Image ID is required", http.StatusBadRequest)
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
		noPrune := r.URL.Query().Get("noprune") == "true"

		deletedImages, err := cli.ImageRemove(ctx, imageID, types.ImageRemoveOptions{
			Force:         force,
			PruneChildren: !noPrune,
		})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to delete image: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(deletedImages)
	}
}

func PruneImagesHandler(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := context.Background()
		cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to create Docker client: %v", err), http.StatusInternalServerError)
			return
		}
		defer cli.Close()

		dangling := r.URL.Query().Get("dangling")
		until := r.URL.Query().Get("until")

		pruneFilters := filters.NewArgs()
		if dangling != "" {
			pruneFilters.Add("dangling", dangling)
		}
		if until != "" {
			pruneFilters.Add("until", until)
		}

		report, err := cli.ImagesPrune(ctx, pruneFilters)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to prune images: %v", err), http.StatusInternalServerError)
			return
		}

		var deletedImages []string
		for _, img := range report.ImagesDeleted {
			if img.Deleted != "" {
				deletedImages = append(deletedImages, img.Deleted)
			}
			if img.Untagged != "" {
				deletedImages = append(deletedImages, img.Untagged)
			}
		}

		result := PruneResult{
			SpaceReclaimed: report.SpaceReclaimed,
			ImagesDeleted:  deletedImages,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}
