package bulk

import (
	"berth-agent/internal/compose"
	"berth-agent/internal/config"
	"berth-agent/internal/stacks"
	"encoding/json"
	"log"
	"net/http"

	"github.com/compose-spec/compose-go/v2/types"
)

type StackWithStatus struct {
	Name               string                                  `json:"name"`
	Path               string                                  `json:"path"`
	Services           map[string]types.ServiceConfig          `json:"services,omitempty"`
	Networks           map[string]stacks.EnhancedNetworkConfig `json:"networks,omitempty"`
	Volumes            map[string]types.VolumeConfig           `json:"volumes,omitempty"`
	ParsedSuccessfully bool                                    `json:"parsed_successfully"`
	ServiceCount       int                                     `json:"service_count"`

	ServiceStatus *compose.ComposePsResponse `json:"service_status,omitempty"`
}

type BulkStacksWithStatusResponse struct {
	Stacks []StackWithStatus `json:"stacks"`
	Total  int               `json:"total"`
}

func BulkStacksWithStatusHandler(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		stacksList, err := stacks.ScanStacks(cfg.ComposeDirPath)
		if err != nil {
			log.Printf("Failed to scan stacks: %v", err)
			http.Error(w, "Failed to scan stacks: "+err.Error(), http.StatusInternalServerError)
			return
		}

		var stacksWithStatus []StackWithStatus

		for _, stack := range stacksList {
			stackWithStatus := StackWithStatus{
				Name:               stack.Name,
				Path:               stack.Path,
				Services:           stack.Services,
				Networks:           stack.Networks,
				Volumes:            stack.Volumes,
				ParsedSuccessfully: stack.ParsedSuccessfully,
				ServiceCount:       len(stack.Services),
			}

			if stack.ParsedSuccessfully {
				serviceStatus := getStackServiceStatus(cfg, stack.Name)
				stackWithStatus.ServiceStatus = serviceStatus
			}

			stacksWithStatus = append(stacksWithStatus, stackWithStatus)
		}

		response := BulkStacksWithStatusResponse{
			Stacks: stacksWithStatus,
			Total:  len(stacksWithStatus),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Printf("Failed to encode bulk response: %v", err)
		}
	}
}

func getStackServiceStatus(cfg *config.AppConfig, stackName string) *compose.ComposePsResponse {

	response, err := compose.GetStackServices(cfg, stackName)
	if err != nil {
		log.Printf("Failed to get service status for stack %s: %v", stackName, err)
		return nil
	}

	return response
}
