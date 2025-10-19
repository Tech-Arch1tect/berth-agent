package images

type RegistryCredential struct {
	Registry     string `json:"registry"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	StackPattern string `json:"stack_pattern"`
	ImagePattern string `json:"image_pattern"`
}

type CheckImageUpdatesRequest struct {
	RegistryCredentials []RegistryCredential `json:"registry_credentials,omitempty"`
	DisabledRegistries  []string             `json:"disabled_registries,omitempty"`
}

type ContainerImageCheckResult struct {
	StackName         string `json:"stack_name"`
	ContainerName     string `json:"container_name"`
	ImageName         string `json:"image_name"`
	CurrentRepoDigest string `json:"current_repo_digest"`
	LatestRepoDigest  string `json:"latest_repo_digest"`
	Error             string `json:"error,omitempty"`
}

type CheckImageUpdatesResponse struct {
	Results []ContainerImageCheckResult `json:"results"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
