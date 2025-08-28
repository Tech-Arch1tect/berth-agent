package docker

import (
	"berth-agent/internal/websocket"
	"context"
	"encoding/json"
	"io"
	"log"
	"os/exec"
	"strings"
	"time"
)

type EventMonitor struct {
	hub           *websocket.Hub
	stackLocation string
	ctx           context.Context
	cancel        context.CancelFunc
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

func NewEventMonitor(hub *websocket.Hub, stackLocation string) *EventMonitor {
	ctx, cancel := context.WithCancel(context.Background())
	return &EventMonitor{
		hub:           hub,
		stackLocation: stackLocation,
		ctx:           ctx,
		cancel:        cancel,
	}
}

func (em *EventMonitor) Start() error {
	log.Println("Starting Docker event monitor")
	go em.monitorDockerEvents()
	return nil
}

func (em *EventMonitor) Stop() {
	log.Println("Stopping Docker event monitor")
	em.cancel()
}

func (em *EventMonitor) monitorDockerEvents() {
	for {
		select {
		case <-em.ctx.Done():
			return
		default:
			if err := em.streamDockerEvents(); err != nil {
				log.Printf("Docker event stream error: %v", err)
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
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	decoder := json.NewDecoder(stdout)
	for {
		var event DockerEvent
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			log.Printf("Error decoding Docker event: %v", err)
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
		return
	}

	stackName, serviceName := em.parseContainerName(containerName)
	if stackName == "" {
		return
	}

	status := em.mapDockerActionToStatus(event.Action)
	health := em.mapDockerActionToHealth(event.Action)

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
