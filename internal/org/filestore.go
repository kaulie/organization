package org

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// storeFileVersion is the schema version of the persisted snapshot. It is
// bumped when the on-disk shape changes incompatibly.
const storeFileVersion = 1

// storeFile is the JSON document persisted by the file backed store. The
// sequence counters are stored explicitly so ids keep increasing even after the
// newest entity has been removed.
type storeFile struct {
	Version     int          `json:"version"`
	Departments []Department `json:"departments"`
	Persons     []Person     `json:"persons"`
	DeptSeq     int          `json:"departmentSeq"`
	EmpSeq      int          `json:"employeeSeq"`
}

// fileStore is a Store that keeps the working set in memory and mirrors every
// mutation to a JSON file.
//
// Why it exists: the service is deployed by restarting the process, so a purely
// in-memory store loses everything on each release. The file lives in the
// deployment's preserved data directory (backend/data), which survives the
// rsync --delete performed by the deployment tool.
type fileStore struct {
	*memoryStore
	path string
	// mu serializes a mutation together with its write, so two concurrent
	// mutations can never persist an older snapshot over a newer one.
	mu sync.Mutex
}

// NewFileStore opens the store backed by the JSON file at path. A missing file
// starts an empty store; an existing file is loaded. The parent directory is
// created when needed. A file that exists but cannot be read or decoded is
// reported as an error instead of being silently replaced by an empty store:
// silently discarding user data is worse than refusing to start.
func NewFileStore(path string) (Store, error) {
	s := &fileStore{memoryStore: newMemoryStore(), path: path}
	if err := s.loadFromDisk(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *fileStore) loadFromDisk() error {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read data file %s: %w", s.path, err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}

	var state storeFile
	if err := json.Unmarshal(raw, &state); err != nil {
		return fmt.Errorf("decode data file %s: %w", s.path, err)
	}
	if state.Version > storeFileVersion {
		return fmt.Errorf("data file %s has version %d, this binary understands up to %d",
			s.path, state.Version, storeFileVersion)
	}
	s.memoryStore.load(state)
	return nil
}

// mutate applies fn to the in-memory store and persists the result. When the
// write fails the in-memory state is rolled back and the caller sees an
// internal error: a mutation that cannot be persisted must not be reported as
// successful, otherwise the data would silently disappear at the next restart.
func (s *fileStore) mutate(fn func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	before := s.memoryStore.snapshot()
	if err := fn(); err != nil {
		return err
	}
	if err := s.persist(); err != nil {
		s.memoryStore.load(before)
		return err
	}
	return nil
}

// persist writes the current snapshot atomically: a temp file in the same
// directory is written, fsynced and then renamed over the target, so a crash
// mid-write can never leave a half written data file behind.
func (s *fileStore) persist() error {
	raw, err := json.MarshalIndent(s.memoryStore.snapshot(), "", "  ")
	if err != nil {
		return Internalf("encode organization data: %v", err)
	}
	raw = append(raw, '\n')

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Internalf("create data directory %s: %v", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".org-store-*.tmp")
	if err != nil {
		return Internalf("create temp file in %s: %v", dir, err)
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return Internalf("write %s: %v", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return Internalf("sync %s: %v", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return Internalf("close %s: %v", tmpName, err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return Internalf("replace %s: %v", s.path, err)
	}
	committed = true
	return nil
}

// CreateDepartment creates a department and persists it.
func (s *fileStore) CreateDepartment(name string, t DepartmentType) (*Department, error) {
	var dept *Department
	err := s.mutate(func() error {
		var err error
		dept, err = s.memoryStore.CreateDepartment(name, t)
		return err
	})
	if err != nil {
		return nil, err
	}
	return dept, nil
}

// RenameDepartment renames a department and persists it.
func (s *fileStore) RenameDepartment(id, name string) (*Department, error) {
	var dept *Department
	err := s.mutate(func() error {
		var err error
		dept, err = s.memoryStore.RenameDepartment(id, name)
		return err
	})
	if err != nil {
		return nil, err
	}
	return dept, nil
}

// RegisterPerson registers a person and persists it.
func (s *fileStore) RegisterPerson(name, id string, t PersonType, departmentID string) (*Person, error) {
	var person *Person
	err := s.mutate(func() error {
		var err error
		person, err = s.memoryStore.RegisterPerson(name, id, t, departmentID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return person, nil
}
