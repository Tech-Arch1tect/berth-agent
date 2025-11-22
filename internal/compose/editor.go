package compose

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"berth-agent/internal/docker"
	"berth-agent/internal/logging"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

type Editor struct {
	stackLocation string
	commandExec   *docker.CommandExecutor
	logger        *logging.Logger
}

func NewEditor(stackLocation string, logger *logging.Logger) *Editor {
	return &Editor{
		stackLocation: stackLocation,
		commandExec:   docker.NewCommandExecutor(stackLocation),
		logger:        logger.With(zap.String("component", "compose.editor")),
	}
}

func (e *Editor) ApplyChanges(stackName string, changes ComposeChanges) error {
	e.logger.Debug("applying changes to compose file",
		zap.String("stack_name", stackName),
	)

	composePath, err := e.findComposeFile(stackName)
	if err != nil {
		e.logger.Error("failed to find compose file",
			zap.String("stack_name", stackName),
			zap.Error(err),
		)
		return fmt.Errorf("failed to find compose file: %w", err)
	}

	e.logger.Debug("found compose file",
		zap.String("compose_path", composePath),
	)

	data, err := os.ReadFile(composePath)
	if err != nil {
		e.logger.Error("failed to read compose file",
			zap.String("compose_path", composePath),
			zap.Error(err),
		)
		return fmt.Errorf("failed to read compose file: %w", err)
	}

	e.logger.Debug("parsing compose file",
		zap.String("compose_path", composePath),
		zap.Int("file_size_bytes", len(data)),
	)

	var composeData yaml.Node
	if err := yaml.Unmarshal(data, &composeData); err != nil {
		e.logger.Error("failed to parse YAML compose file",
			zap.String("compose_path", composePath),
			zap.Error(err),
		)
		return fmt.Errorf("failed to parse compose file: %w", err)
	}

	e.logger.Debug("compose file parsed successfully",
		zap.String("compose_path", composePath),
	)

	if err := e.applyServiceImageUpdates(&composeData, changes.ServiceImageUpdates); err != nil {
		e.logger.Error("failed to apply image updates",
			zap.String("stack_name", stackName),
			zap.Error(err),
		)
		return fmt.Errorf("failed to apply image updates: %w", err)
	}

	if err := e.applyServicePortUpdates(&composeData, changes.ServicePortUpdates); err != nil {
		e.logger.Error("failed to apply port updates",
			zap.String("stack_name", stackName),
			zap.Error(err),
		)
		return fmt.Errorf("failed to apply port updates: %w", err)
	}

	e.logger.Debug("marshalling updated compose file",
		zap.String("compose_path", composePath),
	)

	output, err := yaml.Marshal(&composeData)
	if err != nil {
		e.logger.Error("failed to marshal YAML compose file",
			zap.String("compose_path", composePath),
			zap.Error(err),
		)
		return fmt.Errorf("failed to marshal updated compose file: %w", err)
	}

	backupPath := composePath + ".backup"
	e.logger.Debug("creating backup of compose file",
		zap.String("backup_path", backupPath),
	)

	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		e.logger.Error("failed to create backup file",
			zap.String("backup_path", backupPath),
			zap.Error(err),
		)
		return fmt.Errorf("failed to create backup: %w", err)
	}

	e.logger.Debug("writing updated compose file",
		zap.String("compose_path", composePath),
		zap.Int("output_size_bytes", len(output)),
	)

	if err := os.WriteFile(composePath, output, 0644); err != nil {
		e.logger.Error("failed to write compose file",
			zap.String("compose_path", composePath),
			zap.Error(err),
		)
		if restoreErr := os.Rename(backupPath, composePath); restoreErr != nil {
			e.logger.Error("failed to restore backup after write failure",
				zap.String("compose_path", composePath),
				zap.String("backup_path", backupPath),
				zap.Error(restoreErr),
			)
			return fmt.Errorf("failed to write compose file and restore backup: %w (restore error: %v)", err, restoreErr)
		}
		return fmt.Errorf("failed to write compose file: %w", err)
	}

	e.logger.Debug("validating compose file with docker compose config",
		zap.String("stack_name", stackName),
	)

	if err := e.validateComposeFile(stackName); err != nil {
		e.logger.Error("compose file validation failed, restoring backup",
			zap.String("stack_name", stackName),
			zap.Error(err),
		)

		if restoreErr := os.Rename(backupPath, composePath); restoreErr != nil {
			e.logger.Error("failed to restore backup after validation failure",
				zap.String("compose_path", composePath),
				zap.String("backup_path", backupPath),
				zap.Error(restoreErr),
			)
			return fmt.Errorf("validation failed and backup restoration failed: %w (restore error: %v)", err, restoreErr)
		}

		e.logger.Info("backup restored successfully after validation failure",
			zap.String("compose_path", composePath),
		)

		return fmt.Errorf("compose file validation failed: %w", err)
	}

	e.logger.Debug("compose file validation succeeded",
		zap.String("stack_name", stackName),
	)

	if err := os.Remove(backupPath); err != nil {
		e.logger.Warn("failed to remove backup file (not critical)",
			zap.String("backup_path", backupPath),
			zap.Error(err),
		)
	}

	e.logger.Info("compose file updated successfully",
		zap.String("compose_path", composePath),
	)

	return nil
}

func (e *Editor) findComposeFile(stackName string) (string, error) {
	stackPath := filepath.Join(e.stackLocation, stackName)

	if _, err := os.Stat(stackPath); os.IsNotExist(err) {
		return "", fmt.Errorf("stack '%s' not found", stackName)
	}

	composeFiles := []string{
		"docker-compose.yml",
		"docker-compose.yaml",
		"compose.yml",
		"compose.yaml",
	}

	for _, filename := range composeFiles {
		composePath := filepath.Join(stackPath, filename)
		if _, err := os.Stat(composePath); err == nil {
			return composePath, nil
		}
	}

	return "", fmt.Errorf("no compose file found in stack '%s'", stackName)
}

func (e *Editor) applyServiceImageUpdates(root *yaml.Node, updates []ServiceImageUpdate) error {
	if len(updates) == 0 {
		e.logger.Debug("no image updates to apply")
		return nil
	}

	e.logger.Debug("extracting services section from compose file",
		zap.Int("image_updates_count", len(updates)),
	)

	servicesNode, err := e.findServicesNode(root)
	if err != nil {
		e.logger.Error("failed to find services section in compose file",
			zap.Error(err),
		)
		return err
	}

	e.logger.Debug("services section extracted successfully")

	for _, update := range updates {
		e.logger.Debug("extracting service node",
			zap.String("service_name", update.ServiceName),
		)

		serviceNode, err := e.findServiceNode(servicesNode, update.ServiceName)
		if err != nil {
			e.logger.Error("service not found in compose file",
				zap.String("service_name", update.ServiceName),
				zap.Error(err),
			)
			return fmt.Errorf("service '%s' not found: %w", update.ServiceName, err)
		}

		if update.NewImage != "" {
			e.logger.Debug("updating service image",
				zap.String("service_name", update.ServiceName),
				zap.String("new_image", update.NewImage),
			)
			if err := e.updateNodeValue(serviceNode, "image", update.NewImage); err != nil {
				e.logger.Error("failed to update service image",
					zap.String("service_name", update.ServiceName),
					zap.String("new_image", update.NewImage),
					zap.Error(err),
				)
				return fmt.Errorf("failed to update image for service '%s': %w", update.ServiceName, err)
			}
		} else if update.NewTag != "" {
			currentImage, err := e.getNodeValue(serviceNode, "image")
			if err != nil {
				e.logger.Error("failed to get current image for service",
					zap.String("service_name", update.ServiceName),
					zap.Error(err),
				)
				return fmt.Errorf("failed to get current image for service '%s': %w", update.ServiceName, err)
			}

			newImage := e.updateImageTag(currentImage, update.NewTag)
			e.logger.Debug("updating service image tag",
				zap.String("service_name", update.ServiceName),
				zap.String("current_image", currentImage),
				zap.String("new_tag", update.NewTag),
				zap.String("new_image", newImage),
			)

			if err := e.updateNodeValue(serviceNode, "image", newImage); err != nil {
				e.logger.Error("failed to update service image tag",
					zap.String("service_name", update.ServiceName),
					zap.String("new_tag", update.NewTag),
					zap.Error(err),
				)
				return fmt.Errorf("failed to update image tag for service '%s': %w", update.ServiceName, err)
			}
		}
	}

	e.logger.Debug("all image updates applied successfully",
		zap.Int("updated_services", len(updates)),
	)

	return nil
}

func (e *Editor) applyServicePortUpdates(root *yaml.Node, updates []ServicePortUpdate) error {
	if len(updates) == 0 {
		e.logger.Debug("no port updates to apply")
		return nil
	}

	e.logger.Debug("extracting services section for port updates",
		zap.Int("port_updates_count", len(updates)),
	)

	servicesNode, err := e.findServicesNode(root)
	if err != nil {
		e.logger.Error("failed to find services section for port updates",
			zap.Error(err),
		)
		return err
	}

	for _, update := range updates {
		e.logger.Debug("extracting service node for port update",
			zap.String("service_name", update.ServiceName),
		)

		serviceNode, err := e.findServiceNode(servicesNode, update.ServiceName)
		if err != nil {
			e.logger.Error("service not found for port update",
				zap.String("service_name", update.ServiceName),
				zap.Error(err),
			)
			return fmt.Errorf("service '%s' not found: %w", update.ServiceName, err)
		}

		if len(update.Ports) == 0 {
			e.logger.Debug("removing ports from service",
				zap.String("service_name", update.ServiceName),
			)
			if err := e.removeServiceField(serviceNode, "ports"); err != nil {
				e.logger.Error("failed to remove ports from service",
					zap.String("service_name", update.ServiceName),
					zap.Error(err),
				)
				return fmt.Errorf("failed to remove ports for service '%s': %w", update.ServiceName, err)
			}
			continue
		}

		e.logger.Debug("updating service ports",
			zap.String("service_name", update.ServiceName),
			zap.Strings("ports", update.Ports),
		)

		if err := e.setServiceSequenceField(serviceNode, "ports", update.Ports); err != nil {
			e.logger.Error("failed to update service ports",
				zap.String("service_name", update.ServiceName),
				zap.Error(err),
			)
			return fmt.Errorf("failed to update ports for service '%s': %w", update.ServiceName, err)
		}
	}

	e.logger.Debug("all port updates applied successfully",
		zap.Int("updated_services", len(updates)),
	)

	return nil
}

func (e *Editor) setServiceSequenceField(serviceNode *yaml.Node, key string, values []string) error {
	if serviceNode.Kind != yaml.MappingNode {
		return fmt.Errorf("service node is not a mapping")
	}

	for i := 0; i < len(serviceNode.Content); i += 2 {
		if serviceNode.Content[i].Value == key {
			target := serviceNode.Content[i+1]
			target.Kind = yaml.SequenceNode
			target.Tag = "!!seq"
			target.Content = target.Content[:0]
			for _, value := range values {
				trimmed := strings.TrimSpace(value)
				if trimmed == "" {
					continue
				}
				target.Content = append(target.Content, &yaml.Node{
					Kind:  yaml.ScalarNode,
					Tag:   "!!str",
					Value: trimmed,
				})
			}
			return nil
		}
	}

	keyNode := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Value: key,
	}
	seqNode := &yaml.Node{
		Kind: yaml.SequenceNode,
		Tag:  "!!seq",
	}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		seqNode.Content = append(seqNode.Content, &yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!str",
			Value: trimmed,
		})
	}

	serviceNode.Content = append(serviceNode.Content, keyNode, seqNode)

	return nil
}

func (e *Editor) removeServiceField(serviceNode *yaml.Node, key string) error {
	if serviceNode.Kind != yaml.MappingNode {
		return fmt.Errorf("service node is not a mapping")
	}

	for i := 0; i < len(serviceNode.Content); i += 2 {
		if serviceNode.Content[i].Value == key {
			serviceNode.Content = append(serviceNode.Content[:i], serviceNode.Content[i+2:]...)
			return nil
		}
	}

	return nil
}

func (e *Editor) findServicesNode(root *yaml.Node) (*yaml.Node, error) {
	if root.Kind != yaml.DocumentNode {
		return nil, fmt.Errorf("expected document node")
	}

	if len(root.Content) == 0 {
		return nil, fmt.Errorf("empty document")
	}

	mappingNode := root.Content[0]
	if mappingNode.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("expected mapping node")
	}

	for i := 0; i < len(mappingNode.Content); i += 2 {
		if mappingNode.Content[i].Value == "services" {
			return mappingNode.Content[i+1], nil
		}
	}

	return nil, fmt.Errorf("services section not found")
}

func (e *Editor) findServiceNode(servicesNode *yaml.Node, serviceName string) (*yaml.Node, error) {
	if servicesNode.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("services node is not a mapping")
	}

	for i := 0; i < len(servicesNode.Content); i += 2 {
		if servicesNode.Content[i].Value == serviceName {
			return servicesNode.Content[i+1], nil
		}
	}

	return nil, fmt.Errorf("service not found")
}

func (e *Editor) getNodeValue(serviceNode *yaml.Node, key string) (string, error) {
	if serviceNode.Kind != yaml.MappingNode {
		return "", fmt.Errorf("service node is not a mapping")
	}

	for i := 0; i < len(serviceNode.Content); i += 2 {
		if serviceNode.Content[i].Value == key {
			return serviceNode.Content[i+1].Value, nil
		}
	}

	return "", fmt.Errorf("key '%s' not found", key)
}

func (e *Editor) updateNodeValue(serviceNode *yaml.Node, key, value string) error {
	if serviceNode.Kind != yaml.MappingNode {
		return fmt.Errorf("service node is not a mapping")
	}

	for i := 0; i < len(serviceNode.Content); i += 2 {
		if serviceNode.Content[i].Value == key {
			serviceNode.Content[i+1].Value = value
			return nil
		}
	}

	keyNode := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Value: key,
	}
	valueNode := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Value: value,
	}
	serviceNode.Content = append(serviceNode.Content, keyNode, valueNode)

	return nil
}

func (e *Editor) updateImageTag(currentImage, newTag string) string {
	parts := strings.Split(currentImage, ":")
	if len(parts) > 1 {
		return parts[0] + ":" + newTag
	}
	return currentImage + ":" + newTag
}

func (e *Editor) validateComposeFile(stackName string) error {
	cmd, err := e.commandExec.ExecuteComposeCommand(stackName, "config", "--quiet")
	if err != nil {
		return fmt.Errorf("failed to create validation command: %w", err)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errorMsg := strings.TrimSpace(stderr.String())
		if errorMsg == "" {
			errorMsg = err.Error()
		}

		e.logger.Debug("docker compose config validation output",
			zap.String("stack_name", stackName),
			zap.String("error", errorMsg),
		)

		return fmt.Errorf("%s", errorMsg)
	}

	return nil
}
