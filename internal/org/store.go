package org

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ID/employee-number generation settings.
const (
	departmentIDPrefix = "D"
	employeeNoPrefix   = "E"
	departmentIDWidth  = 4
	employeeNoWidth    = 4
)

// Store persists departments and persons. Implementations must be safe for
// concurrent use. Create operations validate uniqueness and assign the
// system generated identifiers atomically so callers never observe partial
// state.
type Store interface {
	CreateDepartment(name string, t DepartmentType) (*Department, error)
	// RenameDepartment changes the display name of an existing department and
	// keeps its id, type, creation time and members untouched. It fails with
	// KindNotFound for an unknown id and KindConflict when the new name is
	// already taken by another department.
	RenameDepartment(id, name string) (*Department, error)
	GetDepartment(id string) (*Department, bool)
	ListDepartments() []Department

	RegisterPerson(name, id string, t PersonType, departmentID string) (*Person, error)
	GetPerson(id string) (*Person, bool)
	GetPersonByEmployeeNo(employeeNo string) (*Person, bool)
	ListPersons(departmentID string) []Person
}

// memoryStore is an in-memory Store implementation.
type memoryStore struct {
	mu sync.RWMutex

	departments     map[string]*Department
	departmentNames map[string]string // normalized name -> department id
	deptSeq         int

	persons     map[string]*Person
	employeeNos map[string]*Person
	empSeq      int
}

// NewMemoryStore returns an empty in-memory store.
func NewMemoryStore() Store { return newMemoryStore() }

// newMemoryStore returns the concrete in-memory store. It is shared with the
// file backed store, which embeds it to reuse the locking and uniqueness logic.
func newMemoryStore() *memoryStore {
	return &memoryStore{
		departments:     make(map[string]*Department),
		departmentNames: make(map[string]string),
		persons:         make(map[string]*Person),
		employeeNos:     make(map[string]*Person),
	}
}

// normalizeKey makes lookups case- and whitespace-insensitive.
func normalizeKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func (s *memoryStore) CreateDepartment(name string, t DepartmentType) (*Department, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id, ok := s.departmentNames[normalizeKey(name)]; ok {
		return nil, Conflictf("department name %q already exists (id=%s)", name, id)
	}

	s.deptSeq++
	dept := &Department{
		ID:        fmt.Sprintf("%s%0*d", departmentIDPrefix, departmentIDWidth, s.deptSeq),
		Name:      name,
		Type:      t,
		CreatedAt: time.Now().UTC(),
	}
	s.departments[dept.ID] = dept
	s.departmentNames[normalizeKey(name)] = dept.ID
	return dept, nil
}

// RenameDepartment changes a department's display name. The name index is
// rebuilt under the same lock that guards uniqueness, so two concurrent renames
// can never both claim the same name.
func (s *memoryStore) RenameDepartment(id, name string) (*Department, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	dept, ok := s.departments[id]
	if !ok {
		return nil, NotFoundf("department %q not found", id)
	}

	newKey := normalizeKey(name)
	if ownerID, taken := s.departmentNames[newKey]; taken && ownerID != id {
		return nil, Conflictf("department name %q already exists (id=%s)", name, ownerID)
	}

	// Point the index at the new name. Renaming a department to its own name
	// (or a differently cased/whitespaced variant) is an idempotent success.
	delete(s.departmentNames, normalizeKey(dept.Name))
	dept.Name = name
	s.departmentNames[newKey] = id

	copied := *dept
	return &copied, nil
}

func (s *memoryStore) GetDepartment(id string) (*Department, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	dept, ok := s.departments[id]
	if !ok {
		return nil, false
	}
	copied := *dept
	return &copied, true
}

func (s *memoryStore) ListDepartments() []Department {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Department, 0, len(s.departments))
	for _, dept := range s.departments {
		out = append(out, *dept)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *memoryStore) RegisterPerson(name, id string, t PersonType, departmentID string) (*Person, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.persons[id]; ok {
		return nil, Conflictf("person id %q is already registered", id)
	}
	if _, ok := s.departments[departmentID]; !ok {
		return nil, NotFoundf("department %q not found", departmentID)
	}

	s.empSeq++
	person := &Person{
		ID:           id,
		Name:         name,
		Type:         t,
		DepartmentID: departmentID,
		EmployeeNo:   fmt.Sprintf("%s%0*d", employeeNoPrefix, employeeNoWidth, s.empSeq),
		CreatedAt:    time.Now().UTC(),
	}
	s.persons[id] = person
	s.employeeNos[person.EmployeeNo] = person
	return person, nil
}

func (s *memoryStore) GetPerson(id string) (*Person, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	person, ok := s.persons[id]
	if !ok {
		return nil, false
	}
	copied := *person
	return &copied, true
}

func (s *memoryStore) GetPersonByEmployeeNo(employeeNo string) (*Person, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	person, ok := s.employeeNos[employeeNo]
	if !ok {
		return nil, false
	}
	copied := *person
	return &copied, true
}

// numericSuffix parses the numeric part of a generated id (D0007 -> 7). It
// returns 0 for anything that does not look like prefix followed by digits.
func numericSuffix(id, prefix string) int {
	if !strings.HasPrefix(id, prefix) {
		return 0
	}
	n, err := strconv.Atoi(id[len(prefix):])
	if err != nil {
		return 0
	}
	return n
}

// snapshot returns a deep copy of the whole store. It is the unit of
// persistence for the file backed store.
func (s *memoryStore) snapshot() storeFile {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state := storeFile{
		Version:     storeFileVersion,
		Departments: make([]Department, 0, len(s.departments)),
		Persons:     make([]Person, 0, len(s.persons)),
		DeptSeq:     s.deptSeq,
		EmpSeq:      s.empSeq,
	}
	for _, dept := range s.departments {
		state.Departments = append(state.Departments, *dept)
	}
	sort.Slice(state.Departments, func(i, j int) bool { return state.Departments[i].ID < state.Departments[j].ID })
	for _, person := range s.persons {
		state.Persons = append(state.Persons, *person)
	}
	sort.Slice(state.Persons, func(i, j int) bool { return state.Persons[i].EmployeeNo < state.Persons[j].EmployeeNo })
	return state
}

// load replaces the store contents with a snapshot. Sequence counters are
// clamped to the highest id present in the snapshot, so a truncated or
// hand-edited file can never hand out an id that is already in use.
func (s *memoryStore) load(state storeFile) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.departments = make(map[string]*Department, len(state.Departments))
	s.departmentNames = make(map[string]string, len(state.Departments))
	s.persons = make(map[string]*Person, len(state.Persons))
	s.employeeNos = make(map[string]*Person, len(state.Persons))

	s.deptSeq = 0
	for _, dept := range state.Departments {
		restored := dept
		s.departments[restored.ID] = &restored
		s.departmentNames[normalizeKey(restored.Name)] = restored.ID
		if n := numericSuffix(restored.ID, departmentIDPrefix); n > s.deptSeq {
			s.deptSeq = n
		}
	}
	if state.DeptSeq > s.deptSeq {
		s.deptSeq = state.DeptSeq
	}

	s.empSeq = 0
	for _, person := range state.Persons {
		restored := person
		s.persons[restored.ID] = &restored
		s.employeeNos[restored.EmployeeNo] = &restored
		if n := numericSuffix(restored.EmployeeNo, employeeNoPrefix); n > s.empSeq {
			s.empSeq = n
		}
	}
	if state.EmpSeq > s.empSeq {
		s.empSeq = state.EmpSeq
	}
}

// ListPersons returns the members of a department. An empty departmentID
// returns every registered person.
func (s *memoryStore) ListPersons(departmentID string) []Person {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Person, 0, len(s.persons))
	for _, person := range s.persons {
		if departmentID != "" && person.DepartmentID != departmentID {
			continue
		}
		out = append(out, *person)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EmployeeNo < out[j].EmployeeNo })
	return out
}
