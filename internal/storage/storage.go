package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type MountInfo struct {
	ImagePath  string `json:"image_path"`
	MountPoint string `json:"mount_point"`
	Name       string `json:"name,omitempty"`
	ReadOnly   bool   `json:"read_only"`
}

type Storage struct {
	mu     sync.RWMutex
	mounts map[string]*MountInfo
	dbPath string
}

func New() (*Storage, error) {
	// Use /tmp/qimi since mounts don't persist across reboots anyway
	qimiDir := "/tmp/qimi"
	if err := os.MkdirAll(qimiDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create qimi directory: %w", err)
	}

	s := &Storage{
		mounts: make(map[string]*MountInfo),
		dbPath: filepath.Join(qimiDir, "state.json"),
	}

	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load mounts database: %w", err)
	}

	return s, nil
}

func (s *Storage) load() error {
	data, err := os.ReadFile(s.dbPath)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, &s.mounts)
}

func (s *Storage) save() error {
	data, err := json.MarshalIndent(s.mounts, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.dbPath, data, 0644)
}

func (s *Storage) AddMount(info *MountInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := info.ImagePath
	if info.Name != "" {
		if _, exists := s.mounts[info.Name]; exists {
			return fmt.Errorf("mount with name %s already exists", info.Name)
		}
		key = info.Name
	}

	s.mounts[key] = info
	return s.save()
}

func (s *Storage) RemoveMount(nameOrPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.mounts[nameOrPath]; !exists {
		for k, v := range s.mounts {
			if v.ImagePath == nameOrPath {
				delete(s.mounts, k)
				return s.save()
			}
		}
		return fmt.Errorf("mount not found: %s", nameOrPath)
	}

	delete(s.mounts, nameOrPath)
	return s.save()
}

func (s *Storage) GetMount(nameOrPath string) (*MountInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if info, exists := s.mounts[nameOrPath]; exists {
		return info, nil
	}

	for _, info := range s.mounts {
		if info.ImagePath == nameOrPath {
			return info, nil
		}
	}

	return nil, fmt.Errorf("mount not found: %s", nameOrPath)
}

func (s *Storage) ListMounts() []*MountInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var mounts []*MountInfo
	for _, info := range s.mounts {
		mounts = append(mounts, info)
	}
	return mounts
}

func (s *Storage) IsValidMount(info *MountInfo) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	return s.isValidMount(info)
}

func (s *Storage) cleanupStaleMounts() {
	var toRemove []string
	
	for key, info := range s.mounts {
		if !s.isValidMount(info) {
			toRemove = append(toRemove, key)
		}
	}
	
	for _, key := range toRemove {
		delete(s.mounts, key)
	}
	
	if len(toRemove) > 0 {
		s.save()
	}
}

func (s *Storage) isValidMount(info *MountInfo) bool {
	// Check if mount point exists
	if _, err := os.Stat(info.MountPoint); err != nil {
		return false
	}
	
	// Check if it's actually mounted by looking for the metadata file
	nbdFile := filepath.Join("/tmp/qimi/metadata", filepath.Base(info.MountPoint)+".nbd")
	if _, err := os.Stat(nbdFile); err != nil {
		return false
	}
	
	// Check if the mount is active in /proc/mounts
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return false
	}
	
	return strings.Contains(string(data), info.MountPoint)
}

func (s *Storage) CleanupStaleMounts() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	s.cleanupStaleMounts()
	return nil
}