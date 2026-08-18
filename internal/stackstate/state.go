package stackstate

import (
	"fmt"
	"strings"
)

const (
	StatusRunning   = "running"
	StatusPartial   = "partial"
	StatusStopped   = "stopped"
	StatusDown      = "down"
	StatusUnhealthy = "unhealthy"
)

type ContainerFacts struct {
	Service string
	State   string
	Health  string
}

type State struct {
	Status                 string
	Healthy                bool
	ServicesWithContainers int
	TotalContainers        int
	RunningContainers      int
	StoppedContainers      int
	HealthyContainers      int
	UnhealthyContainers    int
	Percentage             int
	Reasons                []string
}

func HealthFromStatus(status string) string {
	switch {
	case strings.Contains(status, "(healthy)"):
		return "healthy"
	case strings.Contains(status, "(unhealthy)"):
		return "unhealthy"
	case strings.Contains(status, "(health: starting)"):
		return "starting"
	default:
		return ""
	}
}

func Evaluate(expectedServices []string, containers []ContainerFacts) State {
	state := State{
		Status:  StatusDown,
		Reasons: []string{},
	}

	containersByService := make(map[string][]ContainerFacts)
	startingContainers := 0
	for _, container := range containers {
		if container.Service == "" {
			continue
		}
		containersByService[container.Service] = append(containersByService[container.Service], container)
		state.TotalContainers++
		if container.State != "running" {
			state.StoppedContainers++
			continue
		}
		state.RunningContainers++
		switch container.Health {
		case "", "healthy":
			state.HealthyContainers++
		case "unhealthy":
			state.UnhealthyContainers++
		case "starting":
			startingContainers++
		}
	}
	state.ServicesWithContainers = len(containersByService)

	upServices := 0
	expectedWithoutContainers := 0
	for _, service := range expectedServices {
		containersOfService := containersByService[service]
		if len(containersOfService) == 0 {
			expectedWithoutContainers++
			continue
		}
		if serviceGreen(containersOfService) {
			upServices++
		}
	}

	allExpectedUp := len(expectedServices) > 0 && upServices == len(expectedServices)
	state.Healthy = allExpectedUp && state.UnhealthyContainers == 0

	if len(expectedServices) > 0 {
		state.Percentage = int(float64(upServices) / float64(len(expectedServices)) * 100)
	}

	switch {
	case state.TotalContainers == 0:
		state.Status = StatusDown
	case state.UnhealthyContainers > 0:
		state.Status = StatusUnhealthy
	case allExpectedUp:
		state.Status = StatusRunning
	case state.RunningContainers > 0:
		state.Status = StatusPartial
	default:
		state.Status = StatusStopped
	}

	state.Reasons = buildReasons(state, len(expectedServices), expectedWithoutContainers, startingContainers)
	return state
}

func serviceGreen(containers []ContainerFacts) bool {
	running := 0
	for _, container := range containers {
		if container.State != "running" {
			continue
		}
		running++
		if container.Health == "unhealthy" || container.Health == "starting" {
			return false
		}
	}
	return running > 0
}

func buildReasons(state State, expectedServices, expectedWithoutContainers, startingContainers int) []string {
	reasons := []string{}
	if expectedServices == 0 {
		reasons = append(reasons, "compose service list unavailable")
	}
	if state.TotalContainers == 0 {
		reasons = append(reasons, "no containers found")
		return reasons
	}
	if expectedWithoutContainers > 0 {
		reasons = append(reasons, fmt.Sprintf("%d service(s) without containers", expectedWithoutContainers))
	}
	if state.StoppedContainers > 0 {
		reasons = append(reasons, fmt.Sprintf("%d container(s) stopped", state.StoppedContainers))
	}
	if state.UnhealthyContainers > 0 {
		reasons = append(reasons, fmt.Sprintf("%d container(s) failing health check", state.UnhealthyContainers))
	}
	if startingContainers > 0 {
		reasons = append(reasons, fmt.Sprintf("%d container(s) still starting their health check", startingContainers))
	}
	return reasons
}
