package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/docker/docker/api/types/mount"
	"github.com/tech-arch1tect/berth-agent/internal/validation"
	"go.uber.org/zap"
)

const rebuildRunID = "index-rebuild"

type bufferedProgressWriter struct {
	lines []string
}

func (w *bufferedProgressWriter) WriteStdout(message string) {
	w.lines = append(w.lines, message)
}

func (w *bufferedProgressWriter) WriteStderr(message string) {
	w.lines = append(w.lines, message)
}

func (w *bufferedProgressWriter) WriteProgress(string) {}

type snapshotSummaryJSON struct {
	BackupStart         string `json:"backup_start"`
	BackupEnd           string `json:"backup_end"`
	FilesNew            uint64 `json:"files_new"`
	FilesChanged        uint64 `json:"files_changed"`
	FilesUnmodified     uint64 `json:"files_unmodified"`
	DataAdded           uint64 `json:"data_added"`
	TotalBytesProcessed uint64 `json:"total_bytes_processed"`
}

type snapshotJSON struct {
	Time           string               `json:"time"`
	ID             string               `json:"id"`
	Hostname       string               `json:"hostname"`
	Tags           []string             `json:"tags"`
	Paths          []string             `json:"paths"`
	ProgramVersion string               `json:"program_version"`
	Summary        *snapshotSummaryJSON `json:"summary"`
}

type RebuildResult struct {
	RunsInRepository int      `json:"runs_in_repository"`
	RunsAdded        int      `json:"runs_added"`
	Output           []string `json:"output"`
}

func (s *Service) RebuildIndex(ctx context.Context, stackName, password string, s3Repo *S3Repository, writer ProgressWriter) (RebuildResult, error) {
	result := RebuildResult{Output: []string{}}

	stackPath, err := validation.SanitizeStackPath(s.cfg.StackLocation, stackName)
	if err != nil {
		return result, fmt.Errorf("invalid stack name: %w", err)
	}

	image, err := s.helperImage(ctx)
	if err != nil {
		return result, err
	}

	writer.WriteStdout(commandEcho("restic", []string{"snapshots", "--json"}))
	snapshots, err := s.runResticBuffered(ctx, image, stackName, rebuildRunID, password, s3Repo,
		[]string{"snapshots", "--json"},
		[]mount.Mount{repoMount(s.repoHostPath(stackName), false)})
	if err != nil {
		return result, err
	}
	if snapshots.exitCode == resticExitRepoDoesNotExist {
		writer.WriteStdout("No backup repository exists for this stack yet; there is nothing to rebuild")
		return result, nil
	}
	if snapshots.exitCode == resticExitWrongPassword {
		return result, fmt.Errorf("the backup password did not unlock this stack's repository")
	}
	if snapshots.exitCode != 0 {
		return result, fmt.Errorf("reading the repository's snapshots failed with exit code %d: %s", snapshots.exitCode, lastLine(snapshots.output))
	}

	rebuilt, err := runsFromSnapshots(snapshots.output, stackName, stackPath)
	if err != nil {
		return result, err
	}
	result.RunsInRepository = len(rebuilt)

	added, err := mergeRebuiltRuns(s.persistence, rebuilt)
	if err != nil {
		return result, err
	}
	result.RunsAdded = added

	if newest := newestRun(rebuilt); newest != nil {
		s.refreshRebuiltRepoSize(ctx, image, newest, password, s3Repo, writer)
	}

	writer.WriteStdout(fmt.Sprintf("The repository holds %d backup runs; %d were missing from the local metadata and have been rebuilt", result.RunsInRepository, result.RunsAdded))
	return result, nil
}

func (s *Service) refreshRebuiltRepoSize(ctx context.Context, image string, run *Run, password string, s3Repo *S3Repository, writer ProgressWriter) {
	stats, err := s.runResticBuffered(ctx, image, run.StackName, rebuildRunID, password, s3Repo,
		[]string{"stats", "--mode", "raw-data", "--json"},
		[]mount.Mount{repoMount(s.repoHostPath(run.StackName), false)})
	if err != nil || stats.exitCode != 0 {
		s.logger.Warn("failed to measure repository size during index rebuild",
			zap.String("stack_name", run.StackName),
			zap.Error(err),
		)
		return
	}
	if size, ok := parseRepoStatsTotalSize(stats.output); ok {
		run.RepoSizeBytes = size
		writer.WriteStdout("All of this stack's backups currently use " + formatBytes(size) + " on disk")
		if err := s.persistence.PersistRun(run); err != nil {
			s.logger.Warn("failed to record the measured repository size",
				zap.String("run_id", run.ID),
				zap.String("stack_name", run.StackName),
				zap.Error(err),
			)
		}
	}
}

func mergeRebuiltRuns(p *RunPersistence, rebuilt []*Run) (int, error) {
	if len(rebuilt) == 0 {
		return 0, nil
	}

	existing, err := p.LoadStackRuns(rebuilt[0].StackName)
	if err != nil {
		return 0, err
	}

	known := make(map[string]bool, len(existing))
	for _, run := range existing {
		known[run.ID] = true
	}

	added := 0
	for _, run := range rebuilt {
		if known[run.ID] {
			continue
		}
		if err := p.PersistRun(run); err != nil {
			return added, err
		}
		added++
	}
	return added, nil
}

func newestRun(runs []*Run) *Run {
	if len(runs) == 0 {
		return nil
	}
	newest := runs[0]
	for _, run := range runs[1:] {
		if run.StartedAt.After(newest.StartedAt) {
			newest = run
		}
	}
	return newest
}

func runsFromSnapshots(output, stackName, stackPath string) ([]*Run, error) {
	var snapshots []snapshotJSON
	if err := json.Unmarshal([]byte(output), &snapshots); err != nil {
		return nil, fmt.Errorf("the repository returned snapshots that could not be parsed: %w", err)
	}

	byRunID := map[string]*Run{}
	order := []string{}
	for _, snapshot := range snapshots {
		runID, label, componentID, ok := splitSnapshotTags(snapshot.Tags)
		if !ok {
			continue
		}
		run, exists := byRunID[runID]
		if !exists {
			run = &Run{
				ID:            runID,
				StackName:     stackName,
				Status:        StatusCompleted,
				Label:         label,
				ResticVersion: snapshot.ProgramVersion,
				Components:    []Component{},
			}
			applySnapshotTimes(run, snapshot)
			byRunID[runID] = run
			order = append(order, runID)
		}
		run.Components = append(run.Components, componentFromSnapshot(snapshot, componentID, stackPath))
	}

	runs := make([]*Run, 0, len(order))
	for _, runID := range order {
		runs = append(runs, byRunID[runID])
	}
	return runs, nil
}

func applySnapshotTimes(run *Run, snapshot snapshotJSON) {
	var backupStart, backupEnd string
	if snapshot.Summary != nil {
		backupStart, backupEnd = snapshot.Summary.BackupStart, snapshot.Summary.BackupEnd
	}
	startedAt, _ := parseSnapshotTime(backupStart, snapshot.Time)
	finishedAt, finishedOK := parseSnapshotTime(backupEnd, snapshot.Time)
	run.StartedAt = startedAt
	if finishedOK && !finishedAt.Before(startedAt) {
		run.FinishedAt = &finishedAt
	}
}

func parseSnapshotTime(primary, fallback string) (time.Time, bool) {
	for _, candidate := range []string{primary, fallback} {
		if candidate == "" {
			continue
		}
		if parsed, err := time.Parse(time.RFC3339Nano, candidate); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func splitSnapshotTags(tags []string) (runID, label, componentID string, ok bool) {
	for _, tag := range tags {
		switch {
		case strings.HasPrefix(tag, "run:"):
			runID = strings.TrimPrefix(tag, "run:")
		case strings.HasPrefix(tag, "label:"):
			label = strings.TrimPrefix(tag, "label:")
		case strings.HasPrefix(tag, "component:"):
			componentID = strings.TrimPrefix(tag, "component:")
		}
	}
	return runID, label, componentID, runID != "" && componentID != ""
}

func componentFromSnapshot(snapshot snapshotJSON, componentID, stackPath string) Component {
	component := Component{
		ID:         componentID,
		Kind:       componentKindFromID(componentID),
		SnapshotID: snapshot.ID,
	}
	switch component.Kind {
	case KindStackDirectory:
		component.SourcePath = stackPath
	case KindBindMount:
		component.SourcePath = strings.TrimPrefix(componentID, string(KindBindMount)+":")
	case KindVolume:
		component.VolumeName = strings.TrimPrefix(componentID, string(KindVolume)+":")
	case KindAnonymousVolume:
		remainder := strings.TrimPrefix(componentID, string(KindAnonymousVolume)+":")
		if service, target, found := strings.Cut(remainder, ":"); found {
			component.Service = service
			component.Target = target
		}
	}
	if snapshot.Summary != nil {
		component.FilesNew = snapshot.Summary.FilesNew
		component.FilesChanged = snapshot.Summary.FilesChanged
		component.FilesUnmodified = snapshot.Summary.FilesUnmodified
		component.BytesAdded = snapshot.Summary.DataAdded
		component.BytesProcessed = snapshot.Summary.TotalBytesProcessed
	}
	return component
}

func componentKindFromID(componentID string) ComponentKind {
	kind, _, found := strings.Cut(componentID, ":")
	if !found {
		return ComponentKind(kind)
	}
	switch ComponentKind(kind) {
	case KindStackDirectory, KindVolume, KindBindMount, KindAnonymousVolume:
		return ComponentKind(kind)
	default:
		return ""
	}
}
