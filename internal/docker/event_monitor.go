package docker

import (
	"context"
	"encoding/json"
	"github.com/tech-arch1tect/berth-agent/internal/logging"
	"github.com/tech-arch1tect/berth-agent/internal/websocket"
	"io"
	"os/exec"
	"strings"
	"time"

	"go.uber.org/zap"
)

type EventMonitor struct {
	hub           *websocket.Hub
	stackLocation string
	ctx           context.Context
	cancel        context.CancelFunc
	logger        *logging.Logger
}

type DockerEvent struct {
	Type     string           `json:"Type"`
	Action   string           `json:"Action"`
	Actor    DockerEventActor `json:"Actor"`
	Time     int64            `json:"time"`
	TimeNano int64            `json:"timeNano"`
}

type DockerEventActor struct {
	ID         string            `json:"ID"`
	Attributes map[string]string `json:"Attributes"`
}

func NewEventMonitor(hub *websocket.Hub, stackLocation string, logger *logging.Logger) *EventMonitor {
	ctx, cancel := context.WithCancel(context.Background())
	logger.Debug("creating docker event monitor", zap.String("stack_location", stackLocation))
	return &EventMonitor{
		hub:           hub,
		stackLocation: stackLocation,
		ctx:           ctx,
		cancel:        cancel,
		logger:        logger,
	}
}

func (em *EventMonitor) Start() error {
	em.logger.Info("docker event monitor starting")
	go em.monitorDockerEvents()
	return nil
}

func (em *EventMonitor) Stop() {
	em.logger.Info("docker event monitor stopping")
	em.cancel()
}

func (em *EventMonitor) monitorDockerEvents() {
	em.logger.Debug("docker event monitor loop started")
	for {
		select {
		case <-em.ctx.Done():
			em.logger.Debug("docker event monitor loop stopped")
			return
		default:
			if err := em.streamDockerEvents(); err != nil {
				em.logger.Warn("docker event stream error, retrying in 5s", zap.Error(err))
				time.Sleep(5 * time.Second)
				continue
			}
		}
	}
}

func (em *EventMonitor) streamDockerEvents() error {
	em.logger.Debug("starting docker events stream")
	cmd := exec.CommandContext(em.ctx, "docker", "events", "--format", "{{json .}}", "--filter", "type=container")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		em.logger.Error("failed to create stdout pipe for docker events", zap.Error(err))
		return err
	}

	if err := cmd.Start(); err != nil {
		em.logger.Error("failed to start docker events command", zap.Error(err))
		return err
	}

	em.logger.Info("docker events stream connected")
	decoder := json.NewDecoder(stdout)
	for {
		var event DockerEvent
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				em.logger.Debug("docker events stream EOF")
				break
			}
			em.logger.Warn("error decoding docker event", zap.Error(err))
			continue
		}

		em.handleDockerEvent(event)
	}

	return cmd.Wait()
}

func (em *EventMonitor) handleDockerEvent(event DockerEvent) {
	if event.Type != "container" {
		return
	}

	containerName := event.Actor.Attributes["name"]
	if containerName == "" {
		em.logger.Debug("skipping event with no container name")
		return
	}

	stackName, serviceName := em.parseContainerName(containerName)
	if stackName == "" {
		em.logger.Debug("skipping non-stack container",
			zap.String("container_name", containerName),
		)
		return
	}

	status := em.mapDockerActionToStatus(event.Action)
	health := em.mapDockerActionToHealth(event.Action)

	em.logger.Debug("docker container event",
		zap.String("action", event.Action),
		zap.String("stack_name", stackName),
		zap.String("service_name", serviceName),
		zap.String("container_name", containerName),
		zap.String("status", status),
		zap.String("health", health),
	)

	containerEvent := websocket.ContainerStatusEvent{
		BaseMessage: websocket.BaseMessage{
			Type:      websocket.MessageTypeContainerStatus,
			Timestamp: time.Unix(event.Time, 0).Format(time.RFC3339),
		},
		StackName:     stackName,
		ServiceName:   serviceName,
		ContainerName: containerName,
		ContainerID:   event.Actor.ID,
		Status:        status,
		Health:        health,
		Image:         event.Actor.Attributes["image"],
	}

	em.hub.BroadcastContainerStatus(containerEvent)

	em.checkAndBroadcastStackStatus(stackName)
}

func (em *EventMonitor) parseContainerName(containerName string) (stackName, serviceName string) {
	parts := strings.Split(containerName, "-")
	if len(parts) < 3 {
		return "", ""
	}
	serviceName = parts[len(parts)-2]
	stackName = strings.Join(parts[:len(parts)-2], "-")

	return stackName, serviceName
}

func (em *EventMonitor) mapDockerActionToStatus(action string) string {
	switch action {
	case "start":
		return "running"
	case "stop", "die":
		return "stopped"
	case "destroy":
		return "not created"
	case "create":
		return "created"
	case "restart":
		return "restarting"
	case "pause":
		return "paused"
	case "unpause":
		return "running"
	case "kill":
		return "stopped"
	case "oom":
		return "stopped"
	default:
		if strings.HasPrefix(action, "health_status") {
			return "running"
		}
		return "unknown"
	}
}

func (em *EventMonitor) mapDockerActionToHealth(action string) string {
	switch action {
	case "health_status: healthy":
		return "healthy"
	case "health_status: unhealthy":
		return "unhealthy"
	case "health_status: starting":
		return "starting"
	default:
		return ""
	}
}

func (em *EventMonitor) checkAndBroadcastStackStatus(stackName string) {
	go func() {
		time.Sleep(1 * time.Second)

		cmd := exec.Command("docker", "compose", "ps", "--format", "json")
		cmd.Dir = em.stackLocation + "/" + stackName

		output, err := cmd.Output()
		if err != nil {
			return
		}

		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		serviceCount := make(map[string]bool)
		runningCount := 0
		stoppedCount := 0

		for _, line := range lines {
			if line == "" {
				continue
			}

			var containerInfo map[string]any
			if err := json.Unmarshal([]byte(line), &containerInfo); err != nil {
				continue
			}

			service, _ := containerInfo["Service"].(string)
			state, _ := containerInfo["State"].(string)

			if service != "" {
				serviceCount[service] = true

				if state == "running" {
					runningCount++
				} else {
					stoppedCount++
				}
			}
		}

		stackEvent := websocket.StackStatusEvent{
			BaseMessage: websocket.BaseMessage{
				Type:      websocket.MessageTypeStackStatus,
				Timestamp: time.Now().Format(time.RFC3339),
			},
			StackName: stackName,
			Status:    em.determineStackStatus(runningCount, stoppedCount),
			Services:  len(serviceCount),
			Running:   runningCount,
			Stopped:   stoppedCount,
		}

		em.hub.BroadcastStackStatus(stackEvent)
	}()
}

func (em *EventMonitor) determineStackStatus(running, stopped int) string {
	total := running + stopped
	if total == 0 {
		return "down"
	}
	if running == total {
		return "running"
	}
	if stopped == total {
		return "stopped"
	}
	return "partial"
}
