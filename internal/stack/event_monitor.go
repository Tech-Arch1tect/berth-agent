package stack

import (
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/tech-arch1tect/berth-agent/internal/docker"
	"github.com/tech-arch1tect/berth-agent/internal/logging"
	"github.com/tech-arch1tect/berth-agent/internal/websocket"

	"go.uber.org/zap"
)

type EventMonitor struct {
	service *Service
	hub     *websocket.Hub
	ctx     context.Context
	cancel  context.CancelFunc
	logger  *logging.Logger
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

func NewEventMonitor(service *Service, hub *websocket.Hub, logger *logging.Logger) *EventMonitor {
	ctx, cancel := context.WithCancel(context.Background())
	return &EventMonitor{
		service: service,
		hub:     hub,
		ctx:     ctx,
		cancel:  cancel,
		logger:  logger.With(zap.String("component", "event_monitor")),
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
	for {
		select {
		case <-em.ctx.Done():
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

	decoder := json.NewDecoder(stdout)
	for {
		var event DockerEvent
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
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

	projectName := event.Actor.Attributes[docker.LabelComposeProject]
	serviceName := event.Actor.Attributes[docker.LabelComposeService]
	if projectName == "" || serviceName == "" {
		em.logger.Debug("skipping event without compose labels",
			zap.String("container", event.Actor.Attributes["name"]))
		return
	}

	stackName, ok := em.service.serviceCache.StackForProject(projectName)
	if !ok {
		em.logger.Debug("skipping container outside the managed stacks",
			zap.String("container", event.Actor.Attributes["name"]))
		return
	}

	containerEvent := websocket.ContainerStatusEvent{
		BaseMessage: websocket.BaseMessage{
			Type:      websocket.MessageTypeContainerStatus,
			Timestamp: time.Unix(event.Time, 0).Format(time.RFC3339),
		},
		StackName:     stackName,
		ServiceName:   serviceName,
		ContainerName: event.Actor.Attributes["name"],
		ContainerID:   event.Actor.ID,
		Status:        mapDockerActionToStatus(event.Action),
		Health:        mapDockerActionToHealth(event.Action),
		Image:         event.Actor.Attributes["image"],
	}

	em.hub.BroadcastContainerStatus(containerEvent)

	go func() {
		time.Sleep(1 * time.Second)
		em.broadcastStackStatus(stackName)
	}()
}

func (em *EventMonitor) broadcastStackStatus(stackName string) {
	state := em.service.evaluateStack(stackName)

	stackEvent := websocket.StackStatusEvent{
		BaseMessage: websocket.BaseMessage{
			Type:      websocket.MessageTypeStackStatus,
			Timestamp: time.Now().Format(time.RFC3339),
		},
		StackName: stackName,
		Status:    state.Status,
		Services:  state.ServicesWithContainers,
		Running:   state.RunningContainers,
		Stopped:   state.StoppedContainers,
	}

	em.hub.BroadcastStackStatus(stackEvent)
}

func mapDockerActionToStatus(action string) string {
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

func mapDockerActionToHealth(action string) string {
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
