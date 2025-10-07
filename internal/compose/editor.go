package compose

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Editor struct {
	stackLocation string
}

func NewEditor(stackLocation string) *Editor {
	return &Editor{
		stackLocation: stackLocation,
	}
}

func (e *Editor) ApplyChanges(stackName string, changes ComposeChanges) error {
	composePath, err := e.findComposeFile(stackName)
	if err != nil {
		return fmt.Errorf("failed to find compose file: %w", err)
	}

	data, err := os.ReadFile(composePath)
	if err != nil {
		return fmt.Errorf("failed to read compose file: %w", err)
	}

	var composeData yaml.Node
	if err := yaml.Unmarshal(data, &composeData); err != nil {
		return fmt.Errorf("failed to parse compose file: %w", err)
	}

	if err := e.applyServiceImageUpdates(&composeData, changes.ServiceImageUpdates); err != nil {
		return fmt.Errorf("failed to apply image updates: %w", err)
	}

	if err := e.applyServicePortUpdates(&composeData, changes.ServicePortUpdates); err != nil {
		return fmt.Errorf("failed to apply port updates: %w", err)
	}

	output, err := yaml.Marshal(&composeData)
	if err != nil {
		return fmt.Errorf("failed to marshal updated compose file: %w", err)
	}

	backupPath := composePath + ".backup"
	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	if err := os.WriteFile(composePath, output, 0644); err != nil {
		if restoreErr := os.Rename(backupPath, composePath); restoreErr != nil {
			return fmt.Errorf("failed to write compose file and restore backup: %w (restore error: %v)", err, restoreErr)
		}
		return fmt.Errorf("failed to write compose file: %w", err)
	}

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
		return nil
	}

	servicesNode, err := e.findServicesNode(root)
	if err != nil {
		return err
	}

	for _, update := range updates {
		serviceNode, err := e.findServiceNode(servicesNode, update.ServiceName)
		if err != nil {
			return fmt.Errorf("service '%s' not found: %w", update.ServiceName, err)
		}

		if update.NewImage != "" {
			if err := e.updateNodeValue(serviceNode, "image", update.NewImage); err != nil {
				return fmt.Errorf("failed to update image for service '%s': %w", update.ServiceName, err)
			}
		} else if update.NewTag != "" {
			currentImage, err := e.getNodeValue(serviceNode, "image")
			if err != nil {
				return fmt.Errorf("failed to get current image for service '%s': %w", update.ServiceName, err)
			}

			newImage := e.updateImageTag(currentImage, update.NewTag)
			if err := e.updateNodeValue(serviceNode, "image", newImage); err != nil {
				return fmt.Errorf("failed to update image tag for service '%s': %w", update.ServiceName, err)
			}
		}
	}

	return nil
}

func (e *Editor) applyServicePortUpdates(root *yaml.Node, updates []ServicePortUpdate) error {
	if len(updates) == 0 {
		return nil
	}

	servicesNode, err := e.findServicesNode(root)
	if err != nil {
		return err
	}

	for _, update := range updates {
		serviceNode, err := e.findServiceNode(servicesNode, update.ServiceName)
		if err != nil {
			return fmt.Errorf("service '%s' not found: %w", update.ServiceName, err)
		}

		if len(update.Ports) == 0 {
			if err := e.removeServiceField(serviceNode, "ports"); err != nil {
				return fmt.Errorf("failed to remove ports for service '%s': %w", update.ServiceName, err)
			}
			continue
		}

		if err := e.setServiceSequenceField(serviceNode, "ports", update.Ports); err != nil {
			return fmt.Errorf("failed to update ports for service '%s': %w", update.ServiceName, err)
		}
	}

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
