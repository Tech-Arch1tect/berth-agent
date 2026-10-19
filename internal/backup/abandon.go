package backup

import (
	"context"
	"fmt"
	"io/fs"
	"os"

	"go.uber.org/zap"
)

type AbandonBackupStorageResult struct {
	Complete bool         `json:"complete"`
	Before   HistoryState `json:"before"`
	After    HistoryState `json:"after"`
	Errors   []string     `json:"errors"`
}

func (s *Service) AbandonBackupStorage(ctx context.Context) (AbandonBackupStorageResult, error) {
	if !s.historyGate.TryLock() {
		return AbandonBackupStorageResult{}, ErrRepositoryBusy
	}
	defer s.historyGate.Unlock()
	result, err := s.persistence.abandonHistory(ctx)
	if err != nil {
		s.logger.Error("failed to abandon backup metadata", zap.Error(err))
	}
	return result, err
}

func (p *RunPersistence) abandonHistory(ctx context.Context) (AbandonBackupStorageResult, error) {
	p.indexMu.Lock()
	defer p.indexMu.Unlock()

	root, err := p.openVerifiedHistoryRoot()
	if err != nil {
		return AbandonBackupStorageResult{}, err
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return AbandonBackupStorageResult{}, fmt.Errorf("failed to read backup persistence entries: %w", err)
	}
	before, err := readHistoryInventory(root.FS())
	if err != nil {
		return AbandonBackupStorageResult{}, err
	}
	result := AbandonBackupStorageResult{
		Before: before.State(),
		Errors: []string{},
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			result.Errors = append(result.Errors, "backup history abandonment was cancelled before all entries were attempted")
			break
		}
		if err := root.RemoveAll(entry.Name()); err != nil {
			p.recordRemovalError(entry.Name(), err, &result)
		}
	}

	remaining, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return AbandonBackupStorageResult{}, fmt.Errorf("failed to read remaining backup persistence entries: %w", err)
	}
	after, err := readHistoryInventory(root.FS())
	if err != nil {
		return AbandonBackupStorageResult{}, err
	}
	result.After = after.State()
	current, err := os.Stat(p.persistenceDir)
	if err != nil || !os.SameFile(p.persistenceIdentity, current) {
		result.Errors = append(result.Errors, "backup history location changed during abandonment")
	}
	result.Complete = result.After.Empty && len(remaining) == 0 && len(result.Errors) == 0
	return result, nil
}

func (p *RunPersistence) recordRemovalError(name string, err error, result *AbandonBackupStorageResult) {
	p.logger.Error("failed to remove backup history entry", zap.String("entry", name), zap.Error(err))
	result.Errors = append(result.Errors, fmt.Sprintf("backup history entry %q could not be removed", name))
}

func (p *RunPersistence) openVerifiedHistoryRoot() (*os.Root, error) {
	root, err := os.OpenRoot(p.persistenceDir)
	if err != nil {
		return nil, fmt.Errorf("failed to open the backup persistence directory: %w", err)
	}
	actual, err := fs.Stat(root.FS(), ".")
	if err != nil {
		_ = root.Close()
		return nil, fmt.Errorf("failed to verify the backup persistence directory: %w", err)
	}
	if !os.SameFile(p.persistenceIdentity, actual) {
		_ = root.Close()
		return nil, fmt.Errorf("backup persistence directory changed after agent startup")
	}
	return root, nil
}
