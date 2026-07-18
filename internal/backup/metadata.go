package backup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/tech-arch1tect/berth-agent/internal/logging"
	"go.uber.org/zap"
)

type RunPersistence struct {
	persistenceDir string
	logger         *logging.Logger
	indexMu        sync.Mutex
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
	temp := filename + ".tmp"
	if err := os.WriteFile(temp, data, 0644); err != nil {
		return fmt.Errorf("failed to write backup run file: %w", err)
	}
	if err := os.Rename(temp, filename); err != nil {
		return fmt.Errorf("failed to write backup run file: %w", err)
	}

	p.updateIndex(run.StackName, func(summaries []RunSummary) []RunSummary {
		replaced := false
		summary := SummariseRun(run)
		for i := range summaries {
			if summaries[i].ID == run.ID {
				summaries[i] = summary
				replaced = true
				break
			}
		}
		if !replaced {
			summaries = append(summaries, summary)
		}
		return summaries
	})

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
			return nil, fmt.Errorf("backup run file %s for stack %s is unreadable: %w - if it is damaged beyond repair, remove it from the agent host and retry", filepath.Join(p.persistenceDir, stackName, entry.Name()), stackName, err)
		}
		if run != nil {
			runs = append(runs, run)
		}
	}
	return runs, nil
}

func (p *RunPersistence) HasRecordedSnapshots(stackName string) (bool, error) {
	runs, err := p.LoadStackRuns(stackName)
	if err != nil {
		return false, err
	}
	for _, run := range runs {
		for _, component := range run.Components {
			if component.SnapshotID != "" {
				return true, nil
			}
		}
	}
	return false, nil
}

func (p *RunPersistence) DeleteRun(stackName, runID string) error {
	if err := os.Remove(p.runFilename(stackName, runID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete backup run file: %w", err)
	}

	p.updateIndex(stackName, func(summaries []RunSummary) []RunSummary {
		kept := summaries[:0]
		for _, summary := range summaries {
			if summary.ID != runID {
				kept = append(kept, summary)
			}
		}
		return kept
	})

	p.logger.Debug("deleted backup run metadata",
		zap.String("run_id", runID),
		zap.String("stack_name", stackName),
	)
	return nil
}

func (p *RunPersistence) indexFilename(stackName string) string {
	return filepath.Join(p.persistenceDir, stackName+".index.json")
}

func (p *RunPersistence) RunSummaries(stackName string) ([]RunSummary, error) {
	p.indexMu.Lock()
	defer p.indexMu.Unlock()

	summaries, err := p.readIndex(stackName)
	if err == nil && !p.indexDrifted(stackName, len(summaries)) {
		return summaries, nil
	}
	return p.rebuildIndex(stackName)
}

func (p *RunPersistence) readIndex(stackName string) ([]RunSummary, error) {
	data, err := os.ReadFile(p.indexFilename(stackName))
	if err != nil {
		return nil, err
	}
	var summaries []RunSummary
	if err := json.Unmarshal(data, &summaries); err != nil {
		return nil, err
	}
	return summaries, nil
}

func (p *RunPersistence) indexDrifted(stackName string, indexed int) bool {
	entries, err := os.ReadDir(filepath.Join(p.persistenceDir, stackName))
	if err != nil {
		return indexed != 0
	}
	runFiles := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			runFiles++
		}
	}
	return runFiles != indexed
}

func (p *RunPersistence) rebuildIndex(stackName string) ([]RunSummary, error) {
	runs, err := p.LoadStackRuns(stackName)
	if err != nil {
		return nil, err
	}
	summaries := make([]RunSummary, 0, len(runs))
	for _, run := range runs {
		summaries = append(summaries, SummariseRun(run))
	}
	sortSummaries(summaries)
	p.writeIndex(stackName, summaries)
	return summaries, nil
}

func (p *RunPersistence) updateIndex(stackName string, mutate func([]RunSummary) []RunSummary) {
	p.indexMu.Lock()
	defer p.indexMu.Unlock()

	summaries, err := p.readIndex(stackName)
	if err != nil {
		if summaries, err = p.rebuildIndex(stackName); err != nil {
			p.dropIndex(stackName, err)
			return
		}
		return
	}
	summaries = mutate(summaries)
	sortSummaries(summaries)
	p.writeIndex(stackName, summaries)
}

func (p *RunPersistence) writeIndex(stackName string, summaries []RunSummary) {
	data, err := json.Marshal(summaries)
	if err != nil {
		p.dropIndex(stackName, err)
		return
	}
	temp := p.indexFilename(stackName) + ".tmp"
	if err := os.WriteFile(temp, data, 0644); err != nil {
		p.dropIndex(stackName, err)
		return
	}
	if err := os.Rename(temp, p.indexFilename(stackName)); err != nil {
		p.dropIndex(stackName, err)
	}
}

func (p *RunPersistence) dropIndex(stackName string, cause error) {
	p.logger.Warn("dropping backup summary index; it will be rebuilt on the next listing",
		zap.String("stack_name", stackName),
		zap.Error(cause),
	)
	if err := os.Remove(p.indexFilename(stackName)); err != nil && !os.IsNotExist(err) {
		p.logger.Error("failed to remove a possibly stale backup summary index",
			zap.String("stack_name", stackName),
			zap.Error(err),
		)
	}
}

func sortSummaries(summaries []RunSummary) {
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].StartedAt.After(summaries[j].StartedAt)
	})
}

func (p *RunPersistence) runFilename(stackName, runID string) string {
	return filepath.Join(p.persistenceDir, stackName, runID+".json")
}
