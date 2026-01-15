package audit

const (
	EventFileRead     = "file.read"
	EventFileWrite    = "file.write"
	EventFileDelete   = "file.delete"
	EventFileRename   = "file.rename"
	EventFileCopy     = "file.copy"
	EventFileChmod    = "file.chmod"
	EventFileChown    = "file.chown"
	EventFileMkdir    = "file.mkdir"
	EventFileUpload   = "file.upload"
	EventFileDownload = "file.download"
	EventFileListDir  = "file.listdir"
	EventFileDirStats = "file.dirstats"
)

const (
	EventStackList          = "stack.list"
	EventStackCreate        = "stack.create"
	EventStackGetDetails    = "stack.get_details"
	EventStackGetSummary    = "stack.get_summary"
	EventStackGetEnvVars    = "stack.get_env_vars"
	EventStackGetNetworks   = "stack.get_networks"
	EventStackGetVolumes    = "stack.get_volumes"
	EventStackGetImages     = "stack.get_images"
	EventStackGetCompose    = "stack.get_compose"
	EventStackUpdateCompose = "stack.update_compose"
)

const (
	EventOperationStarted   = "operation.started"
	EventOperationCompleted = "operation.completed"
	EventOperationFailed    = "operation.failed"
	EventOperationStreamed  = "operation.streamed"
)

const (
	EventMaintenanceGetInfo        = "maintenance.get_info"
	EventMaintenancePrune          = "maintenance.prune"
	EventMaintenanceDeleteResource = "maintenance.delete_resource"
)

const (
	EventContainerLogs     = "container.logs"
	EventContainerStats    = "container.stats"
	EventImageCheckUpdates = "image.check_updates"
)

const (
	EventVulnscanStarted   = "vulnscan.started"
	EventVulnscanCompleted = "vulnscan.completed"
	EventVulnscanRetrieved = "vulnscan.retrieved"
	EventVulnscanStatus    = "vulnscan.status"
)

const (
	EventTerminalConnected    = "terminal.connected"
	EventTerminalDisconnected = "terminal.disconnected"
)

const (
	EventAuthSuccess = "auth.success"
	EventAuthFailure = "auth.failure"
)

func GetEventCategory(eventType string) string {
	switch eventType {
	case EventFileRead, EventFileWrite, EventFileDelete, EventFileRename,
		EventFileCopy, EventFileChmod, EventFileChown, EventFileMkdir,
		EventFileUpload, EventFileDownload, EventFileListDir, EventFileDirStats:
		return "file"

	case EventStackList, EventStackCreate, EventStackGetDetails, EventStackGetSummary,
		EventStackGetEnvVars, EventStackGetNetworks, EventStackGetVolumes,
		EventStackGetImages, EventStackGetCompose, EventStackUpdateCompose:
		return "stack"

	case EventOperationStarted, EventOperationCompleted, EventOperationFailed, EventOperationStreamed:
		return "operation"

	case EventMaintenanceGetInfo, EventMaintenancePrune, EventMaintenanceDeleteResource:
		return "maintenance"

	case EventContainerLogs, EventContainerStats, EventImageCheckUpdates:
		return "container"

	case EventVulnscanStarted, EventVulnscanCompleted, EventVulnscanRetrieved, EventVulnscanStatus:
		return "vulnscan"

	case EventTerminalConnected, EventTerminalDisconnected:
		return "terminal"

	case EventAuthSuccess, EventAuthFailure:
		return "auth"

	default:
		return "unknown"
	}
}

func GetEventSeverity(eventType string) string {
	switch eventType {

	case EventFileDelete, EventMaintenancePrune, EventMaintenanceDeleteResource:
		return "critical"

	case EventFileWrite, EventFileRename, EventFileCopy, EventFileChmod, EventFileChown,
		EventFileMkdir, EventFileUpload, EventStackCreate, EventStackUpdateCompose,
		EventStackGetEnvVars, EventOperationStarted, EventOperationCompleted,
		EventOperationFailed, EventTerminalConnected, EventAuthFailure:
		return "high"

	case EventFileRead, EventFileDownload, EventStackGetDetails, EventStackGetCompose,
		EventVulnscanStarted, EventVulnscanCompleted:
		return "medium"

	case EventStackList, EventStackGetSummary, EventStackGetNetworks, EventStackGetVolumes,
		EventStackGetImages, EventFileListDir, EventFileDirStats, EventContainerLogs,
		EventContainerStats, EventImageCheckUpdates, EventVulnscanRetrieved,
		EventVulnscanStatus, EventMaintenanceGetInfo, EventOperationStreamed,
		EventTerminalDisconnected, EventAuthSuccess:
		return "low"

	default:
		return "medium"
	}
}
