package stacks

import (
	"berth-agent/internal/config"
	"encoding/json"
	"net/http"
)

func ListStacks(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stacks, err := ScanStacks(cfg.ComposeDirPath)
		if err != nil {
			http.Error(w, "Failed to scan stacks: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stacks)
	}
}
