package compose

import "strings"

var SensitiveKeywords = []string{
	"PASSWORD", "PASS", "SECRET", "TOKEN", "KEY", "API_KEY",
	"AUTH", "CREDENTIAL", "PRIVATE", "CERT", "SSL", "TLS",
	"JWT", "OAUTH", "BEARER", "SESSION", "COOKIE",
}

func IsSensitiveKey(key string) bool {
	upperKey := strings.ToUpper(key)
	for _, keyword := range SensitiveKeywords {
		if strings.Contains(upperKey, keyword) {
			return true
		}
	}
	return false
}

type ComposeChanges struct {
	ServiceImageUpdates []ServiceImageUpdate       `json:"service_image_updates,omitempty"`
	ServicePortUpdates  []ServicePortUpdate        `json:"service_port_updates,omitempty"`
	ServiceEnvUpdates   []ServiceEnvironmentUpdate `json:"service_env_updates,omitempty"`
}

type ServiceImageUpdate struct {
	ServiceName string `json:"service_name" binding:"required"`
	NewImage    string `json:"new_image,omitempty"`
	NewTag      string `json:"new_tag,omitempty"`
}

type ServicePortUpdate struct {
	ServiceName string   `json:"service_name" binding:"required"`
	Ports       []string `json:"ports"`
}

type ServiceEnvironmentUpdate struct {
	ServiceName string                `json:"service_name" binding:"required"`
	Environment []EnvironmentVariable `json:"environment"`
}

type EnvironmentVariable struct {
	Key         string `json:"key" binding:"required"`
	Value       string `json:"value"`
	IsSensitive bool   `json:"is_sensitive"`
}

type UpdateComposeRequest struct {
	StackName string         `json:"stack_name" binding:"required"`
	Changes   ComposeChanges `json:"changes" binding:"required"`
}

type PreviewComposeResponse struct {
	Original string `json:"original"`
	Preview  string `json:"preview"`
}
