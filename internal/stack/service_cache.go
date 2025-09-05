package stack

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

type ServiceCountCache struct {
	mu            sync.RWMutex
	counts        map[string]int
	stackLocation string
	watcher       *fsnotify.Watcher
	ctx           context.Context
	cancel        context.CancelFunc
}

type ComposeConfig struct {
	Services map[string]any `json:"services"`
}

func NewServiceCountCache(stackLocation string) *ServiceCountCache {
	ctx, cancel := context.WithCancel(context.Background())

	cache := &ServiceCountCache{
		counts:        make(map[string]int),
		stackLocation: stackLocation,
		ctx:           ctx,
		cancel:        cancel,
	}

	return cache
}

func (c *ServiceCountCache) Start() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}
	c.watcher = watcher

	if err := c.loadAllStackCounts(); err != nil {
		log.Printf("Warning: failed to load initial stack counts: %v", err)
	}

	if err := c.setupWatchers(); err != nil {
		return fmt.Errorf("failed to setup watchers: %w", err)
	}

	go c.watchLoop()

	return nil
}

func (c *ServiceCountCache) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
	if c.watcher != nil {
		c.watcher.Close()
	}
}

func (c *ServiceCountCache) GetServiceCount(stackName string) (int, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	count, exists := c.counts[stackName]
	return count, exists
}

func (c *ServiceCountCache) loadAllStackCounts() error {
	entries, err := os.ReadDir(c.stackLocation)
	if err != nil {
		return fmt.Errorf("failed to read stack location: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		stackName := entry.Name()
		if err := c.loadStackCount(stackName); err != nil {
			log.Printf("Warning: failed to load count for stack %s: %v", stackName, err)
		}
	}

	return nil
}

func (c *ServiceCountCache) loadStackCount(stackName string) error {
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
		delete(c.counts, stackName)
		c.mu.Unlock()
		return nil
	}

	count, err := c.getServiceCountFromCompose(stackPath)
	if err != nil {
		return fmt.Errorf("failed to get service count: %w", err)
	}

	c.mu.Lock()
	c.counts[stackName] = count
	c.mu.Unlock()

	log.Printf("Loaded service count for stack %s: %d services", stackName, count)
	return nil
}

func (c *ServiceCountCache) getServiceCountFromCompose(stackPath string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "compose", "config", "--format", "json")
	cmd.Dir = stackPath

	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("docker compose config failed: %w", err)
	}

	var config ComposeConfig
	if err := json.Unmarshal(output, &config); err != nil {
		return 0, fmt.Errorf("failed to parse compose config: %w", err)
	}

	return len(config.Services), nil
}

func (c *ServiceCountCache) setupWatchers() error {
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

func (c *ServiceCountCache) watchLoop() {
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

func (c *ServiceCountCache) handleFileEvent(event fsnotify.Event) {
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
					if err := c.loadStackCount(stackName); err != nil {
						log.Printf("Failed to load count for new stack %s: %v", stackName, err)
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
		if err := c.loadStackCount(stackName); err != nil {
			log.Printf("Failed to reload stack count for %s: %v", stackName, err)
		}
	}()
}
