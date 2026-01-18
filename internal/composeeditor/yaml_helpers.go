package composeeditor

import (
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func (s *Service) findYamlKey(node *yaml.Node, key string) *yaml.Node {
	if node == nil {
		return nil
	}

	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		return s.findYamlKey(node.Content[0], key)
	}

	if node.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i < len(node.Content); i += 2 {
		if i+1 < len(node.Content) && node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}

	return nil
}

func (s *Service) setYamlValue(node *yaml.Node, key, value string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}

	for i := 0; i < len(node.Content); i += 2 {
		if i+1 < len(node.Content) && node.Content[i].Value == key {
			node.Content[i+1].Kind = yaml.ScalarNode
			node.Content[i+1].Value = value
			node.Content[i+1].Tag = ""
			return
		}
	}

	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
	valueNode := &yaml.Node{Kind: yaml.ScalarNode, Value: value}
	node.Content = append(node.Content, keyNode, valueNode)
}

func (s *Service) setYamlNode(node *yaml.Node, key string, valueNode *yaml.Node) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}

	for i := 0; i < len(node.Content); i += 2 {
		if i+1 < len(node.Content) && node.Content[i].Value == key {
			node.Content[i+1] = valueNode
			return
		}
	}

	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
	node.Content = append(node.Content, keyNode, valueNode)
}

func (s *Service) deleteYamlKey(node *yaml.Node, key string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}

	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content = append(node.Content[:i], node.Content[i+2:]...)
			return
		}
	}
}

func appendScalarPair(node *yaml.Node, key, value string) {
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Value: value},
	)
}

func appendBoolPair(node *yaml.Node, key string, value bool) {
	valStr := "false"
	if value {
		valStr = "true"
	}
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Value: valStr, Tag: "!!bool"},
	)
}

func appendIntPair(node *yaml.Node, key string, value int) {
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Value: strconv.Itoa(value), Tag: "!!int"},
	)
}

func appendUint64Pair(node *yaml.Node, key string, value uint64) {
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Value: strconv.FormatUint(value, 10), Tag: "!!int"},
	)
}

func appendNodePair(node *yaml.Node, key string, valueNode *yaml.Node) {
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		valueNode,
	)
}

func createMappingNode() *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode}
}

func createSequenceNode() *yaml.Node {
	return &yaml.Node{Kind: yaml.SequenceNode}
}

func createScalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: value}
}

func createNullNode() *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null"}
}

func convertSequenceToMapping(seqNode *yaml.Node, separator string) *yaml.Node {
	newNode := createMappingNode()
	newNode.Content = convertSequenceContent(seqNode.Content, separator)
	return newNode
}

func convertSequenceContent(content []*yaml.Node, separator string) []*yaml.Node {
	var result []*yaml.Node
	for _, item := range content {
		if item.Kind == yaml.ScalarNode {
			entry := item.Value
			if idx := strings.Index(entry, separator); idx > 0 {
				key := entry[:idx]
				value := entry[idx+1:]
				result = append(result, createScalarNode(key), createScalarNode(value))
			} else {
				result = append(result, createScalarNode(entry), createScalarNode(""))
			}
		}
	}
	return result
}
