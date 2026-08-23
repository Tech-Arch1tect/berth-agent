package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types/mount"
	"github.com/tech-arch1tect/berth-agent/internal/validation"
)

var ErrRunNotFound = errors.New("backup run not found")
var ErrRepositoryBusy = errors.New("the backup repository is in use by another operation; try again once it finishes")

const deleteReconciliationTimeout = 30 * time.Second
const deleteAllRunID = "delete-all"

type BackupPruneStatus string

const (
	BackupPruneNotNeeded    BackupPruneStatus = "not_needed"
	BackupPruneNotAttempted BackupPruneStatus = "not_attempted"
	BackupPruneSucceeded    BackupPruneStatus = "succeeded"
	BackupPruneFailed       BackupPruneStatus = "failed"
)

type DeleteStackBackupsResult struct {
	StackName          string            `json:"stack_name"`
	Attempted          bool              `json:"attempted"`
	RecordsBefore      int               `json:"records_before"`
	RecordsDeleted     int               `json:"records_deleted"`
	RecordsRemaining   int               `json:"records_remaining"`
	SnapshotsForgotten int               `json:"snapshots_forgotten"`
	PruneStatus        BackupPruneStatus `json:"prune_status"`
	Errors             []string          `json:"errors"`
}

type DeleteAllBackupsResult struct {
	Complete bool                       `json:"complete"`
	Before   HistoryState               `json:"before"`
	After    HistoryState               `json:"after"`
	Stacks   []DeleteStackBackupsResult `json:"stacks"`
	Errors   []string                   `json:"errors"`
}

type deleteSnapshotsResult struct {
	snapshotsForgotten int
	pruneStatus        BackupPruneStatus
}

type deleteResticRunner interface {
	runResticBuffered(ctx context.Context, image, stackName, runID, password string, s3Repo *S3Repository, args []string, mounts []mount.Mount) (bufferedResticResult, error)
}

type deleteStackWork struct {
	records []historyRecord
	runs    []*Run
}

func (s *Service) DeleteAllBackups(ctx context.Context, password string, s3RepositoryBase *S3Repository) (DeleteAllBackupsResult, error) {
	return s.deleteAllBackups(ctx, s, password, s3RepositoryBase)
}

func (s *Service) deleteAllBackups(ctx context.Context, runner deleteResticRunner, password string, s3RepositoryBase *S3Repository) (DeleteAllBackupsResult, error) {
	if password == "" {
		return DeleteAllBackupsResult{}, fmt.Errorf("a backup password is required")
	}
	if s3RepositoryBase == nil {
		if err := s.validateConfiguration(); err != nil {
			return DeleteAllBackupsResult{}, err
		}
	} else if err := validateS3RepositoryBase(s3RepositoryBase); err != nil {
		return DeleteAllBackupsResult{}, err
	}
	if !s.historyGate.TryLock() {
		return DeleteAllBackupsResult{}, ErrRepositoryBusy
	}
	defer s.historyGate.Unlock()

	before, err := s.persistence.historyInventory()
	if err != nil {
		return DeleteAllBackupsResult{}, err
	}
	result := DeleteAllBackupsResult{
		Before: before.State(),
		Stacks: []DeleteStackBackupsResult{},
		Errors: []string{},
	}
	workByStack := make(map[string]*deleteStackWork)
	stackNames := make([]string, 0)
	for _, record := range before.records {
		if record.stackName == "" {
			result.Errors = append(result.Errors, "an unexpected backup history root entry was retained")
			continue
		}
		work := workByStack[record.stackName]
		if work == nil {
			work = &deleteStackWork{}
			workByStack[record.stackName] = work
			stackNames = append(stackNames, record.stackName)
		}
		work.records = append(work.records, record)
		if record.err == nil && record.run != nil {
			work.runs = append(work.runs, record.run)
		}
	}
	sort.Strings(stackNames)

	var image string
	var imageErr error
	imageLoaded := false
	helperImage := func() (string, error) {
		if !imageLoaded {
			image, imageErr = s.helperImage(ctx)
			imageLoaded = true
		}
		return image, imageErr
	}

	for _, stackName := range stackNames {
		stackResult := DeleteStackBackupsResult{
			StackName:     stackName,
			RecordsBefore: len(workByStack[stackName].records),
			PruneStatus:   BackupPruneNotNeeded,
			Errors:        []string{},
		}
		for _, record := range workByStack[stackName].records {
			if record.err != nil || record.run == nil {
				stackResult.Errors = append(stackResult.Errors, "an unreadable backup history record was retained")
			}
		}
		if ctx.Err() == nil && len(workByStack[stackName].runs) > 0 {
			s.deleteStackBackups(ctx, runner, password, s3RepositoryBase, workByStack[stackName].runs, helperImage, &stackResult)
		}
		result.Stacks = append(result.Stacks, stackResult)
	}

	after, err := s.persistence.historyInventory()
	if err != nil {
		return DeleteAllBackupsResult{}, err
	}
	result.After = after.State()
	remainingPaths := make(map[string]struct{}, len(after.records))
	remainingByStack := make(map[string]int)
	for _, record := range after.records {
		remainingPaths[record.path] = struct{}{}
		if record.stackName != "" {
			remainingByStack[record.stackName]++
		}
	}
	for index := range result.Stacks {
		stackResult := &result.Stacks[index]
		stackResult.RecordsRemaining = remainingByStack[stackResult.StackName]
		for _, record := range workByStack[stackResult.StackName].records {
			if _, remains := remainingPaths[record.path]; !remains {
				stackResult.RecordsDeleted++
			}
		}
	}
	result.Complete = result.After.Empty && len(result.Errors) == 0
	for _, stackResult := range result.Stacks {
		if stackResult.RecordsRemaining != 0 || len(stackResult.Errors) != 0 {
			result.Complete = false
		}
	}
	return result, nil
}

func (s *Service) deleteStackBackups(ctx context.Context, runner deleteResticRunner, password string, s3RepositoryBase *S3Repository, runs []*Run, helperImage func() (string, error), result *DeleteStackBackupsResult) {
	result.Attempted = true
	lock := s.repoLocks.get(result.StackName)
	if !lock.TryLock() {
		result.PruneStatus = BackupPruneNotAttempted
		result.Errors = append(result.Errors, ErrRepositoryBusy.Error())
		return
	}
	defer lock.Unlock()

	snapshotRuns := make([]*Run, 0, len(runs))
	for _, run := range runs {
		if ctx.Err() != nil {
			result.Errors = append(result.Errors, "the request was cancelled before this stack was fully processed")
			return
		}
		if len(snapshotIDsForRun(run)) == 0 {
			if err := s.persistence.DeleteRun(run.StackName, run.ID); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("backup run %s metadata could not be removed", run.ID))
			}
			continue
		}
		snapshotRuns = append(snapshotRuns, run)
	}
	if len(snapshotRuns) == 0 {
		return
	}
	result.PruneStatus = BackupPruneNotAttempted
	if ctx.Err() != nil {
		result.Errors = append(result.Errors, "the request was cancelled before repository deletion started")
		return
	}

	repository, err := s3RepositoryForStack(s3RepositoryBase, result.StackName)
	if err != nil {
		result.Errors = append(result.Errors, safeDeleteError(err, password, s3RepositoryBase))
		return
	}
	image, err := helperImage()
	if err != nil {
		result.Errors = append(result.Errors, safeDeleteError(err, password, repository))
		return
	}
	mounts := []mount.Mount(nil)
	if repository == nil {
		mounts = []mount.Mount{repoMount(s.repoHostPath(result.StackName), false)}
	}
	snapshotIDs := snapshotIDsForRuns(snapshotRuns)
	deletion, err := deleteSnapshots(ctx, runner, image, result.StackName, deleteAllRunID, password, repository, mounts, snapshotIDs)
	result.SnapshotsForgotten = deletion.snapshotsForgotten
	result.PruneStatus = deletion.pruneStatus
	if err != nil {
		result.Errors = append(result.Errors, safeDeleteError(err, password, repository))
		return
	}
	for _, run := range snapshotRuns {
		if err := s.persistence.DeleteRun(run.StackName, run.ID); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("backup run %s metadata could not be removed", run.ID))
		}
	}
}

func snapshotIDsForRuns(runs []*Run) []string {
	seen := make(map[string]struct{})
	for _, run := range runs {
		for _, snapshotID := range snapshotIDsForRun(run) {
			seen[snapshotID] = struct{}{}
		}
	}
	ids := make([]string, 0, len(seen))
	for snapshotID := range seen {
		ids = append(ids, snapshotID)
	}
	sort.Strings(ids)
	return ids
}

func validateS3RepositoryBase(repository *S3Repository) error {
	if repository == nil {
		return nil
	}
	if err := repository.Validate(); err != nil {
		return err
	}
	if strings.HasSuffix(repository.URL, "/") {
		return fmt.Errorf("%w: the repository base must not end with a slash", ErrInvalidS3Repository)
	}
	return nil
}

func s3RepositoryForStack(base *S3Repository, stackName string) (*S3Repository, error) {
	if err := validation.ValidateStackName(stackName); err != nil {
		return nil, fmt.Errorf("invalid backup history stack name: %w", err)
	}
	if base == nil {
		return nil, nil
	}
	if err := validateS3RepositoryBase(base); err != nil {
		return nil, err
	}
	repository := *base
	repository.URL += "/stacks/" + stackName
	return &repository, nil
}

func safeDeleteError(err error, password string, repositories ...*S3Repository) string {
	message := err.Error()
	if password != "" {
		message = strings.ReplaceAll(message, password, "[redacted]")
	}
	for _, repository := range repositories {
		if repository == nil {
			continue
		}
		for _, secret := range []string{repository.AccessKeyID, repository.SecretKey} {
			if secret != "" {
				message = strings.ReplaceAll(message, secret, "[redacted]")
			}
		}
	}
	return message
}

func (s *Service) DeleteBackup(ctx context.Context, stackName, backupID, password string, s3Repo *S3Repository) error {
	release, err := s.reserveHistoryRead()
	if err != nil {
		return err
	}
	defer release()

	if err := s.validateConfiguration(); err != nil {
		return err
	}

	lock := s.repoLocks.get(stackName)
	if !lock.TryLock() {
		return ErrRepositoryBusy
	}
	defer lock.Unlock()

	run, err := s.getRun(stackName, backupID)
	if err != nil {
		return err
	}
	if run == nil {
		return ErrRunNotFound
	}

	if len(snapshotIDsForRun(run)) == 0 {
		return s.persistence.DeleteRun(stackName, backupID)
	}
	image, err := s.helperImage(ctx)
	if err != nil {
		return err
	}
	repo := []mount.Mount{repoMount(s.repoHostPath(stackName), false)}
	return deleteRun(ctx, s, s.persistence, image, run, password, s3Repo, repo)
}

func deleteRun(ctx context.Context, runner deleteResticRunner, persistence *RunPersistence, image string, run *Run, password string, s3Repo *S3Repository, mounts []mount.Mount) error {
	snapshotIDs := snapshotIDsForRun(run)
	if len(snapshotIDs) == 0 {
		return persistence.DeleteRun(run.StackName, run.ID)
	}
	if _, err := deleteSnapshots(ctx, runner, image, run.StackName, run.ID, password, s3Repo, mounts, snapshotIDs); err != nil {
		return err
	}
	return persistence.DeleteRun(run.StackName, run.ID)
}

func snapshotIDsForRun(run *Run) []string {
	seen := make(map[string]struct{})
	ids := make([]string, 0, len(run.Components))
	for _, component := range run.Components {
		if component.SnapshotID == "" {
			continue
		}
		if _, ok := seen[component.SnapshotID]; ok {
			continue
		}
		seen[component.SnapshotID] = struct{}{}
		ids = append(ids, component.SnapshotID)
	}
	sort.Strings(ids)
	return ids
}

func deleteSnapshots(ctx context.Context, runner deleteResticRunner, image, stackName, runID, password string, s3Repo *S3Repository, mounts []mount.Mount, snapshotIDs []string) (deleteSnapshotsResult, error) {
	result := deleteSnapshotsResult{pruneStatus: BackupPruneNotAttempted}
	unlock, err := runner.runResticBuffered(ctx, image, stackName, runID, password, s3Repo, []string{"unlock"}, mounts)
	if err != nil {
		return result, fmt.Errorf("failed to clear stale repository locks: %w", err)
	}
	if unlock.exitCode == resticExitRepoDoesNotExist {
		return result, fmt.Errorf("no repository found for stack %s; refusing to delete its backup records", stackName)
	}
	if unlock.exitCode == resticExitWrongPassword {
		return result, fmt.Errorf("the backup password configured for this server in berth does not open this stack's repository; if the password was changed, restore the previous one to manage existing backups")
	}
	if unlock.exitCode != 0 {
		return result, fmt.Errorf("clearing stale repository locks failed with exit code %d: %s", unlock.exitCode, unlock.output)
	}

	forgetArgs := append([]string{"forget"}, snapshotIDs...)
	forget, err := runner.runResticBuffered(ctx, image, stackName, runID, password, s3Repo, forgetArgs, mounts)
	if err != nil {
		failure := fmt.Errorf("failed to delete the backup's snapshots: %w", err)
		absent, reconciliationErr := reconcileDeleteFailure(ctx, runner, image, stackName, runID, password, s3Repo, mounts, snapshotIDs, failure)
		if absent {
			result.snapshotsForgotten = len(snapshotIDs)
		}
		return result, reconciliationErr
	}
	if forget.exitCode != 0 {
		if isRepositoryLockedOutput(forget.output) {
			return result, ErrRepositoryBusy
		}
		failure := fmt.Errorf("deleting the backup's snapshots failed with exit code %d: %s", forget.exitCode, lastLine(forget.output))
		absent, reconciliationErr := reconcileDeleteFailure(ctx, runner, image, stackName, runID, password, s3Repo, mounts, snapshotIDs, failure)
		if absent {
			result.snapshotsForgotten = len(snapshotIDs)
		}
		return result, reconciliationErr
	}
	result.snapshotsForgotten = len(snapshotIDs)

	prune, err := runner.runResticBuffered(ctx, image, stackName, runID, password, s3Repo, []string{"prune"}, mounts)
	if err != nil {
		result.pruneStatus = BackupPruneFailed
		return result, fmt.Errorf("failed to prune deleted backup data: %w", err)
	}
	if prune.exitCode != 0 {
		result.pruneStatus = BackupPruneFailed
		if isRepositoryLockedOutput(prune.output) {
			return result, ErrRepositoryBusy
		}
		return result, fmt.Errorf("pruning deleted backup data failed with exit code %d: %s", prune.exitCode, lastLine(prune.output))
	}
	result.pruneStatus = BackupPruneSucceeded
	return result, nil
}

func reconcileDeleteFailure(ctx context.Context, runner deleteResticRunner, image, stackName, runID, password string, s3Repo *S3Repository, mounts []mount.Mount, snapshotIDs []string, failure error) (bool, error) {
	reconcileCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deleteReconciliationTimeout)
	defer cancel()
	result, err := runner.runResticBuffered(reconcileCtx, image, stackName, runID, password, s3Repo, []string{"snapshots", "--json"}, mounts)
	if err != nil {
		return false, fmt.Errorf("%w; repository state could not be reconciled: %v", failure, err)
	}
	if result.exitCode != 0 {
		return false, fmt.Errorf("%w; repository state could not be reconciled after exit code %d: %s", failure, result.exitCode, lastLine(result.output))
	}
	absent, err := snapshotsAbsent(result.output, snapshotIDs)
	if err != nil {
		return false, fmt.Errorf("%w; repository snapshot state could not be decoded: %v", failure, err)
	}
	if absent {
		return true, fmt.Errorf("%w; the snapshot records are absent but prune has not completed; retry deletion", failure)
	}
	return false, failure
}

func snapshotsAbsent(output string, snapshotIDs []string) (bool, error) {
	var snapshots []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(output), &snapshots); err != nil {
		return false, err
	}
	present := make(map[string]struct{}, len(snapshots))
	for _, snapshot := range snapshots {
		present[snapshot.ID] = struct{}{}
	}
	for _, snapshotID := range snapshotIDs {
		if _, ok := present[snapshotID]; ok {
			return false, nil
		}
	}
	return true, nil
}

func isRepositoryLockedOutput(output string) bool {
	lowered := strings.ToLower(output)
	return strings.Contains(lowered, "repository is already locked") ||
		strings.Contains(lowered, "unable to create lock")
}
