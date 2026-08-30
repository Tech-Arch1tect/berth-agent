package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types/mount"
	"github.com/tech-arch1tect/berth-agent/internal/docker"
)

var ErrComponentNotFound = errors.New("the backup has no such component with a snapshot")
var ErrPathNotFound = errors.New("the backup does not contain that path")
var errNoPassword = errors.New("no backup password was provided for this operation; backups must be enabled and given an encryption password in this server's settings in berth")
var errBackupRepositoryMissing = errors.New("no backup repository exists for this stack")
var errBackupPasswordRejected = errors.New("the backup password configured for this server in berth does not open this stack's repository")

const restoreScratchDir = "/berth-restore"

type FileEntry struct {
	Name  string    `json:"name"`
	Type  string    `json:"type"`
	Size  uint64    `json:"size"`
	MTime time.Time `json:"mtime"`
}

type FileListing struct {
	Path    string      `json:"path"`
	Entries []FileEntry `json:"entries"`
}

func (s *Service) findSnapshotComponent(stackName, backupID, componentID string) (*Run, *Component, error) {
	run, err := s.getRun(stackName, backupID)
	if err != nil {
		return nil, nil, err
	}
	if run == nil {
		return nil, nil, ErrRunNotFound
	}
	for i := range run.Components {
		if run.Components[i].ID == componentID && run.Components[i].SnapshotID != "" {
			return run, &run.Components[i], nil
		}
	}
	return nil, nil, ErrComponentNotFound
}

func componentSnapshotPath(component Component, relPath string) string {
	root := componentSourceMountPath(component)
	if component.IsFile {
		return root
	}
	cleaned := path.Clean("/" + relPath)
	if cleaned == "/" {
		return root
	}
	return root + cleaned
}

func (s *Service) ListBackupFiles(ctx context.Context, stackName, backupID, componentID, relPath, password string, s3Repo *S3Repository) (*FileListing, error) {
	release, err := s.reserveHistoryRead()
	if err != nil {
		return nil, err
	}
	defer release()

	if err := s.validateConfiguration(); err != nil {
		return nil, err
	}
	if password == "" {
		return nil, errNoPassword
	}

	lock := s.repoLocks.get(stackName)
	if !lock.TryRLock() {
		return nil, ErrRepositoryBusy
	}
	defer lock.RUnlock()

	run, component, err := s.findSnapshotComponent(stackName, backupID, componentID)
	if err != nil {
		return nil, err
	}

	if component.IsFile {
		entry, err := s.statFileComponent(ctx, stackName, run, component, password, s3Repo)
		if err != nil {
			return nil, err
		}
		return &FileListing{Path: "/", Entries: []FileEntry{*entry}}, nil
	}

	image, err := s.helperImage(ctx)
	if err != nil {
		return nil, err
	}

	fullPath := componentSnapshotPath(*component, relPath)
	result, err := s.runResticBuffered(ctx, image, stackName, run.ID, password, s3Repo,
		[]string{"ls", "--no-lock", component.SnapshotID, fullPath, "--json"},
		[]mount.Mount{repoMount(s.repoHostPath(stackName), true)})
	if err != nil {
		return nil, errors.New("backup files could not be listed")
	}
	if result.exitCode != 0 {
		return nil, resticReadError("listing files", result)
	}

	return &FileListing{
		Path:    strings.TrimPrefix(fullPath, componentSourceMountPath(*component)) + "/",
		Entries: parseResticLs(result.output, fullPath),
	}, nil
}

func (s *Service) statFileComponent(ctx context.Context, stackName string, run *Run, component *Component, password string, s3Repo *S3Repository) (*FileEntry, error) {
	image, err := s.helperImage(ctx)
	if err != nil {
		return nil, err
	}

	root := componentSourceMountPath(*component)
	result, err := s.runResticBuffered(ctx, image, stackName, run.ID, password, s3Repo,
		[]string{"ls", "--no-lock", component.SnapshotID, root, "--json"},
		[]mount.Mount{repoMount(s.repoHostPath(stackName), true)})
	if err != nil {
		return nil, errors.New("backup paths could not be inspected")
	}
	if result.exitCode != 0 {
		return nil, resticReadError("inspecting the path", result)
	}

	for _, node := range parseResticLsNodes(result.output) {
		if node.Path == root {
			return &FileEntry{
				Name:  path.Base(component.SourcePath),
				Type:  node.Type,
				Size:  node.Size,
				MTime: node.MTime,
			}, nil
		}
	}
	return nil, ErrPathNotFound
}

func (s *Service) StatBackupFiles(ctx context.Context, stackName, backupID, componentID string, relPaths []string, password string, s3Repo *S3Repository) ([]FileEntry, error) {
	releaseHistory, err := s.reserveHistoryRead()
	if err != nil {
		return nil, err
	}
	defer releaseHistory()
	releaseRepository, err := s.reserveRepositoryRead(stackName)
	if err != nil {
		return nil, err
	}
	defer releaseRepository()
	return s.statBackupFiles(ctx, stackName, backupID, componentID, relPaths, password, s3Repo)
}

func (s *Service) statBackupFiles(ctx context.Context, stackName, backupID, componentID string, relPaths []string, password string, s3Repo *S3Repository) ([]FileEntry, error) {
	if err := s.validateConfiguration(); err != nil {
		return nil, err
	}
	if password == "" {
		return nil, errNoPassword
	}

	run, component, err := s.findSnapshotComponent(stackName, backupID, componentID)
	if err != nil {
		return nil, err
	}

	root := componentSourceMountPath(*component)
	parents := map[string]map[string]FileEntry{}
	for _, relPath := range relPaths {
		fullPath := componentSnapshotPath(*component, relPath)
		if fullPath == root {
			continue
		}
		parents[path.Dir(fullPath)] = nil
	}

	if len(parents) > 0 {
		image, err := s.helperImage(ctx)
		if err != nil {
			return nil, err
		}
		for parent := range parents {
			result, err := s.runResticBuffered(ctx, image, stackName, run.ID, password, s3Repo,
				[]string{"ls", "--no-lock", component.SnapshotID, parent, "--json"},
				[]mount.Mount{repoMount(s.repoHostPath(stackName), true)})
			if err != nil {
				return nil, errors.New("backup paths could not be inspected")
			}
			if result.exitCode != 0 {
				return nil, resticReadError("inspecting the path", result)
			}
			byName := map[string]FileEntry{}
			for _, entry := range parseResticLs(result.output, parent) {
				byName[entry.Name] = entry
			}
			parents[parent] = byName
		}
	}

	entries := make([]FileEntry, 0, len(relPaths))
	for _, relPath := range relPaths {
		fullPath := componentSnapshotPath(*component, relPath)
		if fullPath == root {
			if component.IsFile {
				entry, err := s.statFileComponent(ctx, stackName, run, component, password, s3Repo)
				if err != nil {
					return nil, err
				}
				entries = append(entries, *entry)
				continue
			}
			entries = append(entries, FileEntry{Name: "/", Type: "dir"})
			continue
		}
		entry, ok := parents[path.Dir(fullPath)][path.Base(fullPath)]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrPathNotFound, relPath)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (s *Service) DumpBackupFile(ctx context.Context, stackName, backupID, componentID, relPath, password string, s3Repo *S3Repository, out io.Writer) error {
	releaseHistory, err := s.reserveHistoryRead()
	if err != nil {
		return err
	}
	defer releaseHistory()
	releaseRepository, err := s.reserveRepositoryRead(stackName)
	if err != nil {
		return err
	}
	defer releaseRepository()
	return s.dumpBackupFile(ctx, stackName, backupID, componentID, relPath, password, s3Repo, out)
}

func (s *Service) dumpBackupFile(ctx context.Context, stackName, backupID, componentID, relPath, password string, s3Repo *S3Repository, out io.Writer) error {
	if err := s.validateConfiguration(); err != nil {
		return err
	}
	if password == "" {
		return errNoPassword
	}

	run, component, err := s.findSnapshotComponent(stackName, backupID, componentID)
	if err != nil {
		return err
	}

	image, err := s.helperImage(ctx)
	if err != nil {
		return err
	}

	container := docker.ContainerRunSpec{
		Image:      image,
		Entrypoint: []string{"restic"},
		Cmd:        []string{"dump", "--no-lock", component.SnapshotID, componentSnapshotPath(*component, relPath)},
		Env:        resticEnv(password, s3Repo),
		Mounts:     mountsForRepository([]mount.Mount{repoMount(s.repoHostPath(stackName), true)}, s3Repo),
		Labels:     s.helperLabels(stackName, run.ID),
	}

	var stderr bytes.Buffer
	exitCode, err := s.dockerClient.RunContainer(ctx, container, out, &stderr)
	if err != nil {
		return errors.New("backup file could not be downloaded")
	}
	if exitCode != 0 {
		return resticReadError("downloading the file", bufferedResticResult{exitCode: exitCode, output: strings.TrimSpace(stderr.String())})
	}
	return nil
}

const archiveScript = `set -e
snap="$1"
sub="$2"
shift 2
n=$#
i=0
while [ "$i" -lt "$n" ]; do
  p="$1"
  shift
  set -- "$@" --include "$p"
  i=$((i+1))
done
restic -q restore --no-lock --sparse "$snap" --target ` + restoreScratchDir + ` "$@" 1>&2
cd "` + restoreScratchDir + `$sub"
tar czf - .`

func (s *Service) ArchiveBackupFiles(ctx context.Context, stackName, backupID, componentID string, relPaths []string, password string, s3Repo *S3Repository, out io.Writer) error {
	releaseHistory, err := s.reserveHistoryRead()
	if err != nil {
		return err
	}
	defer releaseHistory()
	releaseRepository, err := s.reserveRepositoryRead(stackName)
	if err != nil {
		return err
	}
	defer releaseRepository()
	return s.archiveBackupFiles(ctx, stackName, backupID, componentID, relPaths, password, s3Repo, out)
}

func (s *Service) archiveBackupFiles(ctx context.Context, stackName, backupID, componentID string, relPaths []string, password string, s3Repo *S3Repository, out io.Writer) error {
	if err := s.validateConfiguration(); err != nil {
		return err
	}
	if password == "" {
		return errNoPassword
	}

	run, component, err := s.findSnapshotComponent(stackName, backupID, componentID)
	if err != nil {
		return err
	}

	image, err := s.helperImage(ctx)
	if err != nil {
		return err
	}

	root := componentSourceMountPath(*component)
	entrypoint := []string{"/bin/sh", "-c", archiveScript, "sh", component.SnapshotID, root}
	for _, relPath := range relPaths {
		entrypoint = append(entrypoint, escapeResticPattern(componentSnapshotPath(*component, relPath)))
	}

	container := docker.ContainerRunSpec{
		Image:      image,
		Entrypoint: entrypoint,
		Env:        resticEnv(password, s3Repo),
		Mounts:     mountsForRepository([]mount.Mount{repoMount(s.repoHostPath(stackName), true)}, s3Repo),
		Labels:     s.helperLabels(stackName, run.ID),
	}

	var stderr bytes.Buffer
	exitCode, err := s.dockerClient.RunContainer(ctx, container, out, &stderr)
	if err != nil {
		return errors.New("backup archive could not be downloaded")
	}
	if exitCode != 0 {
		return resticReadError("building the archive", bufferedResticResult{exitCode: exitCode, output: strings.TrimSpace(stderr.String())})
	}
	return nil
}

func resticReadError(action string, result bufferedResticResult) error {
	switch result.exitCode {
	case resticExitRepoDoesNotExist:
		return errBackupRepositoryMissing
	case resticExitWrongPassword:
		return errBackupPasswordRejected
	default:
		if isRepositoryLockedOutput(result.output) {
			return ErrRepositoryBusy
		}
		return fmt.Errorf("%s failed", action)
	}
}

type resticLsNode struct {
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	Path        string    `json:"path"`
	Size        uint64    `json:"size"`
	Mode        uint64    `json:"mode"`
	UID         uint32    `json:"uid"`
	GID         uint32    `json:"gid"`
	MTime       time.Time `json:"mtime"`
	StructType  string    `json:"struct_type"`
	MessageType string    `json:"message_type"`
}

func (n resticLsNode) permissionBits() uint64 {
	return n.Mode & 0o7777
}

func parseResticLsNodes(output string) []resticLsNode {
	var nodes []resticLsNode
	for line := range strings.Lines(output) {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var node resticLsNode
		if err := json.Unmarshal([]byte(line), &node); err != nil {
			continue
		}
		if node.StructType != "node" && node.MessageType != "node" {
			continue
		}
		if node.Type != "file" && node.Type != "dir" {
			continue
		}
		nodes = append(nodes, node)
	}
	return nodes
}

func parseResticLs(output, dir string) []FileEntry {
	var entries []FileEntry
	for _, node := range parseResticLsNodes(output) {
		if node.Path == dir || path.Dir(node.Path) != dir {
			continue
		}
		entries = append(entries, FileEntry{
			Name:  node.Name,
			Type:  node.Type,
			Size:  node.Size,
			MTime: node.MTime,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Type != entries[j].Type {
			return entries[i].Type == "dir"
		}
		return entries[i].Name < entries[j].Name
	})
	return entries
}
