package backup

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/mount"
)

var ErrRunNotFound = errors.New("backup run not found")
var ErrRepositoryBusy = errors.New("the backup repository is in use by another operation; try again once it finishes")

func (s *Service) DeleteBackup(ctx context.Context, stackName, backupID, password string) error {
	ctx = context.WithoutCancel(ctx)

	if err := s.validateConfiguration(); err != nil {
		return err
	}

	lock := s.repoLocks.get(stackName)
	if !lock.TryLock() {
		return ErrRepositoryBusy
	}
	defer lock.Unlock()

	run, err := s.GetRun(stackName, backupID)
	if err != nil {
		return err
	}
	if run == nil {
		return ErrRunNotFound
	}

	var snapshotIDs []string
	for _, component := range run.Components {
		if component.SnapshotID != "" {
			snapshotIDs = append(snapshotIDs, component.SnapshotID)
		}
	}

	if len(snapshotIDs) > 0 {
		image, err := s.helperImage(ctx)
		if err != nil {
			return err
		}
		repo := []mount.Mount{repoMount(s.repoHostPath(stackName), false)}

		unlock, err := s.runResticBuffered(ctx, image, stackName, run.ID, password, []string{"unlock"}, repo)
		if err != nil {
			return fmt.Errorf("failed to clear stale repository locks: %w", err)
		}
		if unlock.exitCode != 0 {
			return fmt.Errorf("clearing stale repository locks failed with exit code %d: %s", unlock.exitCode, unlock.output)
		}

		args := append([]string{"forget", "--prune"}, snapshotIDs...)
		forget, err := s.runResticBuffered(ctx, image, stackName, run.ID, password, args, repo)
		if err != nil {
			return fmt.Errorf("failed to delete the backup's snapshots: %w", err)
		}
		if forget.exitCode != 0 {
			if isRepositoryLockedOutput(forget.output) {
				return ErrRepositoryBusy
			}
			return fmt.Errorf("deleting the backup's snapshots failed with exit code %d: %s", forget.exitCode, lastLine(forget.output))
		}
	}

	return s.persistence.DeleteRun(stackName, backupID)
}

func isRepositoryLockedOutput(output string) bool {
	lowered := strings.ToLower(output)
	return strings.Contains(lowered, "repository is already locked") ||
		strings.Contains(lowered, "unable to create lock")
}
