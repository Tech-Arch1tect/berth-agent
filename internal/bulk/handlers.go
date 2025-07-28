package bulk

import (
	"berth-agent/internal/compose"
	"berth-agent/internal/config"
	"berth-agent/internal/stacks"
	"encoding/json"
	"log"
	"net/http"
	"sync"

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
		var mu sync.Mutex
		var wg sync.WaitGroup

		resultChan := make(chan StackWithStatus, len(stacksList))

		for _, stack := range stacksList {
			wg.Add(1)
			go func(s stacks.Stack) {
				defer wg.Done()

				stackWithStatus := StackWithStatus{
					Name:               s.Name,
					Path:               s.Path,
					Services:           s.Services,
					Networks:           s.Networks,
					Volumes:            s.Volumes,
					ParsedSuccessfully: s.ParsedSuccessfully,
					ServiceCount:       len(s.Services),
				}

				if s.ParsedSuccessfully {
					response, err := compose.GetStackServices(cfg, s.Name)
					if err != nil {
						log.Printf("Failed to get service status for stack %s: %v", s.Name, err)
					} else {
						stackWithStatus.ServiceStatus = response
					}
				}

				resultChan <- stackWithStatus
			}(stack)
		}

		go func() {
			wg.Wait()
			close(resultChan)
		}()

		for result := range resultChan {
			mu.Lock()
			stacksWithStatus = append(stacksWithStatus, result)
			mu.Unlock()
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
