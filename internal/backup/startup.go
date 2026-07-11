package backup

import (
	"context"
	"os"
	"time"

	"go.uber.org/zap"
)

func (p *RunPersistence) ListStacksWithRuns() ([]string, error) {
	entries, err := os.ReadDir(p.persistenceDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var stacks []string
	for _, entry := range entries {
		if entry.IsDir() {
			stacks = append(stacks, entry.Name())
		}
	}
	return stacks, nil
}

func (s *Service) MarkInterruptedRuns() {
	stacks, err := s.persistence.ListStacksWithRuns()
	if err != nil {
		s.logger.Error("failed to scan backup metadata for interrupted runs", zap.Error(err))
		return
	}

	for _, stackName := range stacks {
		runs, err := s.persistence.LoadStackRuns(stackName)
		if err != nil {
			s.logger.Error("failed to load backup runs while marking interrupted runs",
				zap.String("stack_name", stackName),
				zap.Error(err),
			)
			continue
		}
		for _, run := range runs {
			if run.Status != StatusRunning {
				continue
			}
			now := time.Now()
			run.Status = StatusInterrupted
			run.FinishedAt = &now
			run.Error = "the agent restarted while this backup was running"
			if err := s.persistence.PersistRun(run); err != nil {
				s.logger.Error("failed to mark backup run as interrupted",
					zap.String("stack_name", stackName),
					zap.String("run_id", run.ID),
					zap.Error(err),
				)
				continue
			}
			s.logger.Warn("marked backup run as interrupted after agent restart",
				zap.String("stack_name", stackName),
				zap.String("run_id", run.ID),
			)
		}
	}
}

func (s *Service) RemoveOrphanedHelpers(ctx context.Context) {
	containers, err := s.dockerClient.ContainerList(ctx, map[string][]string{
		"label": {"berth.backup.stack"},
	})
	if err != nil {
		s.logger.Error("failed to list backup helper containers for cleanup", zap.Error(err))
		return
	}

	for _, summary := range containers {
		if err := s.dockerClient.ContainerRemove(ctx, summary.ID, false, false, true); err != nil {
			s.logger.Error("failed to remove orphaned backup helper container",
				zap.String("container_id", summary.ID),
				zap.Error(err),
			)
			continue
		}
		s.logger.Warn("removed orphaned backup helper container left by a previous agent run",
			zap.String("container_id", summary.ID),
			zap.String("stack_name", summary.Labels["berth.backup.stack"]),
		)
	}
}
