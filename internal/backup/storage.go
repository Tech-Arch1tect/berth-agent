package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/tech-arch1tect/berth-agent/internal/validation"
)

type HistoryState struct {
	Empty                 bool `json:"empty"`
	StackCount            int  `json:"stack_count"`
	RecordCount           int  `json:"record_count"`
	UnreadableRecordCount int  `json:"unreadable_record_count"`
}

type historyRecord struct {
	stackName string
	name      string
	path      string
	run       *Run
	err       error
}

type historyInventory struct {
	records []historyRecord
}

func (i historyInventory) State() HistoryState {
	stacks := make(map[string]struct{})
	unreadable := 0
	for _, record := range i.records {
		if record.stackName != "" {
			stacks[record.stackName] = struct{}{}
		}
		if record.err != nil {
			unreadable++
		}
	}
	return HistoryState{
		Empty:                 len(i.records) == 0,
		StackCount:            len(stacks),
		RecordCount:           len(i.records),
		UnreadableRecordCount: unreadable,
	}
}

func (p *RunPersistence) historyInventory() (historyInventory, error) {
	entries, err := os.ReadDir(p.persistenceDir)
	if err != nil {
		return historyInventory{}, fmt.Errorf("failed to read backup history: %w", err)
	}

	inventory := historyInventory{records: []historyRecord{}}
	for _, entry := range entries {
		if entry.IsDir() {
			records := p.stackHistoryRecords(entry.Name())
			inventory.records = append(inventory.records, records...)
			continue
		}
		if derivedHistoryIndex(entry) {
			continue
		}
		inventory.records = append(inventory.records, historyRecord{
			name: entry.Name(),
			path: filepath.Join(p.persistenceDir, entry.Name()),
			err:  fmt.Errorf("unexpected entry in the backup history root"),
		})
	}
	sort.Slice(inventory.records, func(i, j int) bool {
		if inventory.records[i].stackName == inventory.records[j].stackName {
			return inventory.records[i].name < inventory.records[j].name
		}
		return inventory.records[i].stackName < inventory.records[j].stackName
	})
	return inventory, nil
}

func derivedHistoryIndex(entry os.DirEntry) bool {
	if !entry.Type().IsRegular() {
		return false
	}
	name := entry.Name()
	var stackName string
	switch {
	case strings.HasSuffix(name, ".index.json.tmp"):
		stackName = strings.TrimSuffix(name, ".index.json.tmp")
	case strings.HasSuffix(name, ".index.json"):
		stackName = strings.TrimSuffix(name, ".index.json")
	default:
		return false
	}
	return stackName != "" && validation.ValidateStackName(stackName) == nil
}

func (p *RunPersistence) stackHistoryRecords(stackName string) []historyRecord {
	stackDir := filepath.Join(p.persistenceDir, stackName)
	entries, err := os.ReadDir(stackDir)
	if err != nil {
		return []historyRecord{{
			stackName: stackName,
			name:      stackName,
			path:      stackDir,
			err:       fmt.Errorf("failed to read backup history for stack %s: %w", stackName, err),
		}}
	}

	stackErr := validation.ValidateStackName(stackName)
	records := make([]historyRecord, 0, len(entries))
	for _, entry := range entries {
		record := historyRecord{
			stackName: stackName,
			name:      entry.Name(),
			path:      filepath.Join(stackDir, entry.Name()),
		}
		switch {
		case stackErr != nil:
			record.err = fmt.Errorf("invalid backup history stack name: %w", stackErr)
		case !entry.Type().IsRegular():
			record.err = fmt.Errorf("unexpected entry in the stack backup history")
		case !strings.HasSuffix(entry.Name(), ".json"):
			record.err = fmt.Errorf("unexpected file in the stack backup history")
		default:
			record.run, record.err = readHistoryRun(record.path, stackName, strings.TrimSuffix(entry.Name(), ".json"))
		}
		records = append(records, record)
	}
	return records
}

func readHistoryRun(filename, stackName, runID string) (*Run, error) {
	if _, err := uuid.Parse(runID); err != nil {
		return nil, fmt.Errorf("invalid backup run filename: %w", err)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read backup run: %w", err)
	}
	var run Run
	if err := json.Unmarshal(data, &run); err != nil {
		return nil, fmt.Errorf("failed to decode backup run: %w", err)
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

func knownRunStatus(status RunStatus) bool {
	switch status {
	case StatusRunning, StatusCompleted, StatusFailed, StatusInterrupted:
		return true
	default:
		return false
	}
}

type OperationReservation interface {
	CreateBackup(context.Context, string, string, CreateOptions, ProgressWriter) error
	RestoreBackup(context.Context, string, string, RestoreOptions, ProgressWriter) error
	Release()
}

type operationReservation struct {
	service *Service
	once    sync.Once
}

func (s *Service) ReserveOperation() (OperationReservation, error) {
	if !s.historyGate.TryRLock() {
		return nil, ErrRepositoryBusy
	}
	return &operationReservation{service: s}, nil
}

func (r *operationReservation) CreateBackup(ctx context.Context, stackName, stackPath string, opts CreateOptions, writer ProgressWriter) error {
	return r.service.createBackup(ctx, stackName, stackPath, opts, writer)
}

func (r *operationReservation) RestoreBackup(ctx context.Context, stackName, stackPath string, opts RestoreOptions, writer ProgressWriter) error {
	return r.service.restoreBackup(ctx, stackName, stackPath, opts, writer)
}

func (r *operationReservation) Release() {
	r.once.Do(r.service.historyGate.RUnlock)
}

func (s *Service) reserveHistoryRead() (func(), error) {
	if !s.historyGate.TryRLock() {
		return nil, ErrRepositoryBusy
	}
	return s.historyGate.RUnlock, nil
}

func (s *Service) BackupHistory() (HistoryState, error) {
	if !s.historyGate.TryLock() {
		return HistoryState{}, ErrRepositoryBusy
	}
	defer s.historyGate.Unlock()

	inventory, err := s.persistence.historyInventory()
	if err != nil {
		return HistoryState{}, err
	}
	return inventory.State(), nil
}
