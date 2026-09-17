package org

import (
	"fmt"
	"sort"
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
func NewMemoryStore() Store {
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
