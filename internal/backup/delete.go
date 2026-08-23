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
)

var ErrRunNotFound = errors.New("backup run not found")
var ErrRepositoryBusy = errors.New("the backup repository is in use by another operation; try again once it finishes")

const deleteReconciliationTimeout = 30 * time.Second

type deleteResticRunner interface {
	runResticBuffered(ctx context.Context, image, stackName, runID, password string, s3Repo *S3Repository, args []string, mounts []mount.Mount) (bufferedResticResult, error)
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
	if err := deleteRunSnapshots(ctx, runner, image, run, password, s3Repo, mounts, snapshotIDs); err != nil {
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

func deleteRunSnapshots(ctx context.Context, runner deleteResticRunner, image string, run *Run, password string, s3Repo *S3Repository, mounts []mount.Mount, snapshotIDs []string) error {
	unlock, err := runner.runResticBuffered(ctx, image, run.StackName, run.ID, password, s3Repo, []string{"unlock"}, mounts)
	if err != nil {
		return fmt.Errorf("failed to clear stale repository locks: %w", err)
	}
	if unlock.exitCode == resticExitRepoDoesNotExist {
		return fmt.Errorf("no repository found for stack %s; refusing to delete its backup records", run.StackName)
	}
	if unlock.exitCode == resticExitWrongPassword {
		return fmt.Errorf("the backup password configured for this server in berth does not open this stack's repository; if the password was changed, restore the previous one to manage existing backups")
	}
	if unlock.exitCode != 0 {
		return fmt.Errorf("clearing stale repository locks failed with exit code %d: %s", unlock.exitCode, unlock.output)
	}

	forgetArgs := append([]string{"forget"}, snapshotIDs...)
	forget, err := runner.runResticBuffered(ctx, image, run.StackName, run.ID, password, s3Repo, forgetArgs, mounts)
	if err != nil {
		failure := fmt.Errorf("failed to delete the backup's snapshots: %w", err)
		return reconcileDeleteFailure(ctx, runner, image, run, password, s3Repo, mounts, snapshotIDs, failure)
	}
	if forget.exitCode != 0 {
		if isRepositoryLockedOutput(forget.output) {
			return ErrRepositoryBusy
		}
		failure := fmt.Errorf("deleting the backup's snapshots failed with exit code %d: %s", forget.exitCode, lastLine(forget.output))
		return reconcileDeleteFailure(ctx, runner, image, run, password, s3Repo, mounts, snapshotIDs, failure)
	}

	prune, err := runner.runResticBuffered(ctx, image, run.StackName, run.ID, password, s3Repo, []string{"prune"}, mounts)
	if err != nil {
		return fmt.Errorf("failed to prune deleted backup data: %w", err)
	}
	if prune.exitCode != 0 {
		if isRepositoryLockedOutput(prune.output) {
			return ErrRepositoryBusy
		}
		return fmt.Errorf("pruning deleted backup data failed with exit code %d: %s", prune.exitCode, lastLine(prune.output))
	}
	return nil
}

func reconcileDeleteFailure(ctx context.Context, runner deleteResticRunner, image string, run *Run, password string, s3Repo *S3Repository, mounts []mount.Mount, snapshotIDs []string, failure error) error {
	reconcileCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deleteReconciliationTimeout)
	defer cancel()
	result, err := runner.runResticBuffered(reconcileCtx, image, run.StackName, run.ID, password, s3Repo, []string{"snapshots", "--json"}, mounts)
	if err != nil {
		return fmt.Errorf("%w; repository state could not be reconciled: %v", failure, err)
	}
	if result.exitCode != 0 {
		return fmt.Errorf("%w; repository state could not be reconciled after exit code %d: %s", failure, result.exitCode, lastLine(result.output))
	}
	absent, err := snapshotsAbsent(result.output, snapshotIDs)
	if err != nil {
		return fmt.Errorf("%w; repository snapshot state could not be decoded: %v", failure, err)
	}
	if absent {
		return fmt.Errorf("%w; the snapshot records are absent but prune has not completed; retry deletion", failure)
	}
	return failure
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
