package backup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tech-arch1tect/berth-agent/internal/logging"
	"go.uber.org/zap"
)

type RunPersistence struct {
	persistenceDir string
	logger         *logging.Logger
}

func NewRunPersistence(persistenceDir string, logger *logging.Logger) (*RunPersistence, error) {
	if err := os.MkdirAll(persistenceDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create backup persistence directory: %w", err)
	}
	return &RunPersistence{
		persistenceDir: persistenceDir,
		logger:         logger,
	}, nil
}

func (p *RunPersistence) PersistRun(run *Run) error {
	stackDir := filepath.Join(p.persistenceDir, run.StackName)
	if err := os.MkdirAll(stackDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup metadata directory for stack %s: %w", run.StackName, err)
	}

	data, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal backup run: %w", err)
	}

	filename := p.runFilename(run.StackName, run.ID)
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write backup run file: %w", err)
	}

	p.logger.Debug("persisted backup run metadata",
		zap.String("run_id", run.ID),
		zap.String("stack_name", run.StackName),
		zap.String("filename", filename),
	)
	return nil
}

func (p *RunPersistence) LoadRun(stackName, runID string) (*Run, error) {
	data, err := os.ReadFile(p.runFilename(stackName, runID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read backup run file: %w", err)
	}

	var run Run
	if err := json.Unmarshal(data, &run); err != nil {
		return nil, fmt.Errorf("failed to unmarshal backup run: %w", err)
	}
	return &run, nil
}

func (p *RunPersistence) LoadStackRuns(stackName string) ([]*Run, error) {
	stackDir := filepath.Join(p.persistenceDir, stackName)
	entries, err := os.ReadDir(stackDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*Run{}, nil
		}
		return nil, fmt.Errorf("failed to read backup metadata directory: %w", err)
	}

	var runs []*Run
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		run, err := p.LoadRun(stackName, strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			p.logger.Warn("failed to load backup run file",
				zap.String("stack_name", stackName),
				zap.String("filename", entry.Name()),
				zap.Error(err),
			)
			continue
		}
		if run != nil {
			runs = append(runs, run)
		}
	}
	return runs, nil
}

func (p *RunPersistence) DeleteRun(stackName, runID string) error {
	if err := os.Remove(p.runFilename(stackName, runID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete backup run file: %w", err)
	}
	p.logger.Debug("deleted backup run metadata",
		zap.String("run_id", runID),
		zap.String("stack_name", stackName),
	)
	return nil
}

func (p *RunPersistence) runFilename(stackName, runID string) string {
	return filepath.Join(p.persistenceDir, stackName, runID+".json")
}
