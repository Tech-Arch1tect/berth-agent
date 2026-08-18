package stack

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

type ComposeModel struct {
	ProjectName     string
	DefaultServices []string
}

type ComposeModelCache struct {
	mu            sync.RWMutex
	models        map[string]ComposeModel
	stackLocation string
	watcher       *fsnotify.Watcher
	ctx           context.Context
	cancel        context.CancelFunc
}

type composeFileModel struct {
	Name     string                         `json:"name"`
	Services map[string]composeServiceModel `json:"services"`
}

type composeServiceModel struct {
	Profiles []string `json:"profiles"`
}

func NewComposeModelCache(stackLocation string) *ComposeModelCache {
	ctx, cancel := context.WithCancel(context.Background())

	cache := &ComposeModelCache{
		models:        make(map[string]ComposeModel),
		stackLocation: stackLocation,
		ctx:           ctx,
		cancel:        cancel,
	}

	return cache
}

func (c *ComposeModelCache) Start() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}
	c.watcher = watcher

	if err := c.loadAllStackModels(); err != nil {
		log.Printf("Warning: failed to load initial stack models: %v", err)
	}

	if err := c.setupWatchers(); err != nil {
		return fmt.Errorf("failed to setup watchers: %w", err)
	}

	go c.watchLoop()

	return nil
}

func (c *ComposeModelCache) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
	if c.watcher != nil {
		c.watcher.Close()
	}
}

func (c *ComposeModelCache) Get(stackName string) (ComposeModel, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	model, exists := c.models[stackName]
	return model, exists
}

func (c *ComposeModelCache) StackForProject(projectName string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for stackName, model := range c.models {
		if model.ProjectName == projectName {
			return stackName, true
		}
	}
	return "", false
}

func (c *ComposeModelCache) loadAllStackModels() error {
	entries, err := os.ReadDir(c.stackLocation)
	if err != nil {
		return fmt.Errorf("failed to read stack location: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		stackName := entry.Name()
		if err := c.loadStackModel(stackName); err != nil {
			log.Printf("Warning: failed to load compose model for stack %s: %v", stackName, err)
		}
	}

	return nil
}

func (c *ComposeModelCache) loadStackModel(stackName string) error {
	stackPath := filepath.Join(c.stackLocation, stackName)

	composeFiles := []string{
		"docker-compose.yml",
		"docker-compose.yaml",
		"compose.yml",
		"compose.yaml",
	}

	var hasComposeFile bool
	for _, filename := range composeFiles {
		composePath := filepath.Join(stackPath, filename)
		if _, err := os.Stat(composePath); err == nil {
			hasComposeFile = true
			break
		}
	}

	if !hasComposeFile {
		c.mu.Lock()
		delete(c.models, stackName)
		c.mu.Unlock()
		return nil
	}

	model, err := loadComposeModel(stackPath)
	if err != nil {
		return fmt.Errorf("failed to load compose model: %w", err)
	}

	c.mu.Lock()
	c.models[stackName] = model
	c.mu.Unlock()

	log.Printf("Loaded compose model for stack %s: project %q, %d default services", stackName, model.ProjectName, len(model.DefaultServices))
	return nil
}

func loadComposeModel(stackPath string) (ComposeModel, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "compose", "config", "--format", "json")
	cmd.Dir = stackPath

	output, err := cmd.Output()
	if err != nil {
		return ComposeModel{}, fmt.Errorf("docker compose config failed: %w", err)
	}

	return parseComposeModel(output)
}

func parseComposeModel(output []byte) (ComposeModel, error) {
	var parsed composeFileModel
	if err := json.Unmarshal(output, &parsed); err != nil {
		return ComposeModel{}, fmt.Errorf("failed to parse compose config: %w", err)
	}

	model := ComposeModel{
		ProjectName:     parsed.Name,
		DefaultServices: []string{},
	}
	for service, definition := range parsed.Services {
		if len(definition.Profiles) == 0 {
			model.DefaultServices = append(model.DefaultServices, service)
		}
	}
	sort.Strings(model.DefaultServices)

	return model, nil
}

func (c *ComposeModelCache) setupWatchers() error {
	entries, err := os.ReadDir(c.stackLocation)
	if err != nil {
		return fmt.Errorf("failed to read stack location: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		stackDir := filepath.Join(c.stackLocation, entry.Name())
		if err := c.watcher.Add(stackDir); err != nil {
			log.Printf("Warning: failed to watch directory %s: %v", stackDir, err)
		}
	}

	return c.watcher.Add(c.stackLocation)
}

func (c *ComposeModelCache) watchLoop() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case event, ok := <-c.watcher.Events:
			if !ok {
				return
			}
			c.handleFileEvent(event)
		case err, ok := <-c.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("File watcher error: %v", err)
		}
	}
}

func (c *ComposeModelCache) handleFileEvent(event fsnotify.Event) {
	if event.Op&fsnotify.Create == fsnotify.Create {
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			if filepath.Dir(event.Name) == c.stackLocation {
				stackName := filepath.Base(event.Name)
				log.Printf("New stack directory created: %s", stackName)

				if err := c.watcher.Add(event.Name); err != nil {
					log.Printf("Warning: failed to watch new directory %s: %v", event.Name, err)
				}

				go func() {
					time.Sleep(500 * time.Millisecond)
					if err := c.loadStackModel(stackName); err != nil {
						log.Printf("Failed to load compose model for new stack %s: %v", stackName, err)
					}
				}()
				return
			}
		}
	}

	if !strings.HasSuffix(event.Name, ".yml") && !strings.HasSuffix(event.Name, ".yaml") {
		return
	}

	stackDir := filepath.Dir(event.Name)
	stackName := filepath.Base(stackDir)

	expectedDir := filepath.Join(c.stackLocation, stackName)
	if stackDir != expectedDir {
		return
	}

	log.Printf("Compose file changed: %s (event: %s)", event.Name, event.Op)

	go func() {
		time.Sleep(100 * time.Millisecond)
		if err := c.loadStackModel(stackName); err != nil {
			log.Printf("Failed to reload compose model for %s: %v", stackName, err)
		}
	}()
}
