package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/tech-arch1tect/berth-agent/internal/logging"
	"github.com/tech-arch1tect/berth-agent/internal/validation"
	"go.uber.org/zap"
)

type RunPersistence struct {
	persistenceDir      string
	persistenceIdentity os.FileInfo
	logger              *logging.Logger
	indexMu             sync.Mutex
}

func NewRunPersistence(persistenceDir string, logger *logging.Logger) (*RunPersistence, error) {
	if persistenceDir == "" {
		return nil, fmt.Errorf("backup persistence directory is not configured")
	}
	absolute, err := filepath.Abs(persistenceDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve backup persistence directory: %w", err)
	}
	if err := os.MkdirAll(absolute, 0755); err != nil {
		return nil, fmt.Errorf("failed to create backup persistence directory: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve backup persistence directory: %w", err)
	}
	if filepath.Dir(resolved) == resolved {
		return nil, fmt.Errorf("backup persistence directory must not be the filesystem root")
	}
	identity, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect backup persistence directory: %w", err)
	}
	if !identity.IsDir() {
		return nil, fmt.Errorf("backup persistence path is not a directory")
	}
	filesystemRoot, err := os.Stat(string(filepath.Separator))
	if err != nil {
		return nil, fmt.Errorf("failed to inspect filesystem root: %w", err)
	}
	if os.SameFile(filesystemRoot, identity) {
		return nil, fmt.Errorf("backup persistence directory must not be the filesystem root")
	}
	return &RunPersistence{
		persistenceDir:      resolved,
		persistenceIdentity: identity,
		logger:              logger,
	}, nil
}

func validateRunID(runID string) error {
	parsed, err := uuid.Parse(runID)
	if err != nil || parsed.String() != runID {
		return fmt.Errorf("invalid backup run id %q", runID)
	}
	return nil
}

func (p *RunPersistence) stackDirectory(stackName string) (string, error) {
	if err := validation.ValidateStackName(stackName); err != nil {
		return "", fmt.Errorf("invalid backup stack name: %w", err)
	}
	return filepath.Join(p.persistenceDir, stackName), nil
}

func (p *RunPersistence) runFilename(stackName, runID string) (string, error) {
	stackDir, err := p.stackDirectory(stackName)
	if err != nil {
		return "", err
	}
	if err := validateRunID(runID); err != nil {
		return "", err
	}
	return filepath.Join(stackDir, runID+".json"), nil
}

func (p *RunPersistence) indexFilename(stackName string) (string, error) {
	if err := validation.ValidateStackName(stackName); err != nil {
		return "", fmt.Errorf("invalid backup stack name: %w", err)
	}
	return filepath.Join(p.persistenceDir, stackName+".index.json"), nil
}

func (p *RunPersistence) PersistRun(run *Run) error {
	filename, err := p.runFilename(run.StackName, run.ID)
	if err != nil {
		return err
	}
	stackDir := filepath.Dir(filename)
	if err := os.MkdirAll(stackDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup metadata directory for stack %s: %w", run.StackName, err)
	}

	data, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal backup run: %w", err)
	}

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

func decodeRun(source io.Reader, stackName, runID string) (*Run, error) {
	if err := validateRunID(runID); err != nil {
		return nil, fmt.Errorf("invalid backup run filename: %w", err)
	}
	decoder := json.NewDecoder(source)
	var run Run
	if err := decoder.Decode(&run); err != nil {
		return nil, fmt.Errorf("failed to decode backup run: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("backup run contains trailing data")
	}
	if run.ID != runID {
		return nil, fmt.Errorf("backup run id %q does not match filename %q", run.ID, runID)
	}
	if run.StackName != stackName {
		return nil, fmt.Errorf("backup run stack %q does not match directory %q", run.StackName, stackName)
	}
	if !knownRunStatus(run.Status) {
		return nil, fmt.Errorf("backup run has unknown status %q", run.Status)
	}
	return &run, nil
}

func (p *RunPersistence) LoadRun(stackName, runID string) (*Run, error) {
	filename, err := p.runFilename(stackName, runID)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read backup run file: %w", err)
	}
	defer file.Close()
	return decodeRun(file, stackName, runID)
}

func (p *RunPersistence) LoadStackRuns(stackName string) ([]*Run, error) {
	stackDir, err := p.stackDirectory(stackName)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(stackDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*Run{}, nil
		}
		return nil, fmt.Errorf("failed to read backup metadata directory: %w", err)
	}

	var runs []*Run
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if !entry.Type().IsRegular() {
			return nil, fmt.Errorf("backup run file %s for stack %s is unreadable: unexpected entry type - if it is damaged beyond repair, remove it from the agent host and retry", filepath.Join(p.persistenceDir, stackName, entry.Name()), stackName)
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
	filename, err := p.runFilename(stackName, runID)
	if err != nil {
		return err
	}
	if err := os.Remove(filename); err != nil && !os.IsNotExist(err) {
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

	p.discardStackIfEmpty(stackName)

	p.logger.Debug("deleted backup run metadata",
		zap.String("run_id", runID),
		zap.String("stack_name", stackName),
	)
	return nil
}

func (p *RunPersistence) discardStackIfEmpty(stackName string) {
	dir, err := p.stackDirectory(stackName)
	if err != nil {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) > 0 {
		return
	}
	if err := os.Remove(dir); err != nil {
		p.logger.Warn("failed to remove the empty metadata directory of a stack with no backups left",
			zap.String("stack_name", stackName),
			zap.Error(err),
		)
		return
	}
	indexFilename, err := p.indexFilename(stackName)
	if err != nil {
		return
	}
	if err := os.Remove(indexFilename); err != nil && !os.IsNotExist(err) {
		p.logger.Warn("failed to remove the summary index of a stack with no backups left",
			zap.String("stack_name", stackName),
			zap.Error(err),
		)
	}
}

func (p *RunPersistence) RunSummaries(stackName string) ([]RunSummary, error) {
	p.indexMu.Lock()
	defer p.indexMu.Unlock()

	summaries, err := p.readIndex(stackName)
	if err == nil && p.indexMatchesRunFiles(stackName, summaries) {
		return summaries, nil
	}
	return p.rebuildIndex(stackName)
}

func (p *RunPersistence) readIndex(stackName string) ([]RunSummary, error) {
	filename, err := p.indexFilename(stackName)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	var summaries []RunSummary
	if err := decoder.Decode(&summaries); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("backup summary index contains trailing data")
	}
	return summaries, nil
}

func (p *RunPersistence) indexMatchesRunFiles(stackName string, summaries []RunSummary) bool {
	stackDir, err := p.stackDirectory(stackName)
	if err != nil {
		return false
	}
	entries, err := os.ReadDir(stackDir)
	if err != nil {
		return os.IsNotExist(err) && len(summaries) == 0
	}
	indexed := make(map[string]struct{}, len(summaries))
	for _, summary := range summaries {
		if summary.StackName != stackName || validateRunID(summary.ID) != nil || !knownRunStatus(summary.Status) {
			return false
		}
		if _, exists := indexed[summary.ID]; exists {
			return false
		}
		indexed[summary.ID] = struct{}{}
	}
	files := make(map[string]struct{})
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if !entry.Type().IsRegular() {
			return false
		}
		runID := strings.TrimSuffix(entry.Name(), ".json")
		if validateRunID(runID) != nil {
			return false
		}
		files[runID] = struct{}{}
	}
	if len(indexed) != len(files) {
		return false
	}
	for runID := range files {
		if _, exists := indexed[runID]; !exists {
			return false
		}
	}
	return true
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
	if err != nil || !p.indexMatchesRunFiles(stackName, summaries) {
		if _, err = p.rebuildIndex(stackName); err != nil {
			p.dropIndex(stackName, err)
		}
		return
	}
	summaries = mutate(summaries)
	sortSummaries(summaries)
	p.writeIndex(stackName, summaries)
}

func (p *RunPersistence) writeIndex(stackName string, summaries []RunSummary) {
	filename, err := p.indexFilename(stackName)
	if err != nil {
		p.dropIndex(stackName, err)
		return
	}
	data, err := json.Marshal(summaries)
	if err != nil {
		p.dropIndex(stackName, err)
		return
	}
	temp := filename + ".tmp"
	if err := os.WriteFile(temp, data, 0644); err != nil {
		p.dropIndex(stackName, err)
		return
	}
	if err := os.Rename(temp, filename); err != nil {
		p.dropIndex(stackName, err)
	}
}

func (p *RunPersistence) dropIndex(stackName string, cause error) {
	p.logger.Warn("dropping backup summary index; it will be rebuilt on the next listing",
		zap.String("stack_name", stackName),
		zap.Error(cause),
	)
	filename, err := p.indexFilename(stackName)
	if err != nil {
		return
	}
	if err := os.Remove(filename); err != nil && !os.IsNotExist(err) {
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
