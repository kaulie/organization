package org

import (
	"context"
	"strings"
)

// Service implements the organization management use cases on top of a Store.
type Service struct {
	store Store
}

// NewService wires a service around the given store. A nil store falls back to
// a fresh in-memory store.
func NewService(store Store) *Service {
	if store == nil {
		store = NewMemoryStore()
	}
	return &Service{store: store}
}

// DepartmentDetail is a department together with its member list.
type DepartmentDetail struct {
	Department
	Members []PersonView `json:"members"`
}

// PersonView is a person enriched with the display name of its department.
type PersonView struct {
	Person
	DepartmentName string `json:"departmentName"`
}

// CreateDepartment creates a new department. Requirement 1.
func (s *Service) CreateDepartment(req CreateDepartmentRequest) (*Department, error) {
	name, err := validateName("department name", req.Name)
	if err != nil {
		return nil, err
	}
	deptType, err := ParseDepartmentType(req.Type)
	if err != nil {
		return nil, err
	}
	return s.store.CreateDepartment(name, deptType)
}

// RenameDepartment changes the display name of an existing department. The id,
// type, creation time and members stay as they are, so every reference to the
// department keeps working after the rename. Requirement 5.
func (s *Service) RenameDepartment(id string, req RenameDepartmentRequest) (*Department, error) {
	deptID := strings.TrimSpace(id)
	if deptID == "" {
		return nil, Validationf("department id is required")
	}
	name, err := validateName("department name", req.Name)
	if err != nil {
		return nil, err
	}
	return s.store.RenameDepartment(deptID, name)
}

// Ping reports whether the backing store can serve a request. Stores that keep
// their data in process return nil; the PostgreSQL store checks the database,
// which is what /health uses so a deploy does not go live against a database
// it cannot reach.
func (s *Service) Ping(ctx context.Context) error {
	type pinger interface {
		Ping(context.Context) error
	}
	p, ok := s.store.(pinger)
	if !ok {
		return nil
	}
	return p.Ping(ctx)
}

// ListDepartments returns every department ordered by id.
func (s *Service) ListDepartments() ([]Department, error) {
	return s.store.ListDepartments()
}

// GetDepartment returns a department and its members. Requirement 3.
func (s *Service) GetDepartment(id string) (*DepartmentDetail, error) {
	deptID := strings.TrimSpace(id)
	dept, ok, err := s.store.GetDepartment(deptID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, NotFoundf("department %q not found", id)
	}
	members, err := s.store.ListPersons(dept.ID)
	if err != nil {
		return nil, err
	}
	views, err := s.viewAll(members)
	if err != nil {
		return nil, err
	}
	return &DepartmentDetail{
		Department: *dept,
		Members:    views,
	}, nil
}

// RegisterPerson registers a HUMAN or AGENT in a department and assigns an
// employee number. Requirement 2.
func (s *Service) RegisterPerson(req RegisterPersonRequest) (*PersonView, error) {
	name, err := validateName("person name", req.Name)
	if err != nil {
		return nil, err
	}

	personID := strings.TrimSpace(req.ID)
	if personID == "" {
		return nil, Validationf("person id is required")
	}
	if len([]rune(personID)) > maxIDLength {
		return nil, Validationf("person id must not exceed %d characters", maxIDLength)
	}

	personType, err := ParsePersonType(req.Type)
	if err != nil {
		return nil, err
	}

	departmentID := strings.TrimSpace(req.DepartmentID)
	if departmentID == "" {
		return nil, Validationf("departmentId is required")
	}
	if _, ok, err := s.store.GetDepartment(departmentID); err != nil {
		return nil, err
	} else if !ok {
		return nil, NotFoundf("department %q not found", req.DepartmentID)
	}

	person, err := s.store.RegisterPerson(name, personID, personType, departmentID)
	if err != nil {
		return nil, err
	}
	view, err := s.view(*person)
	if err != nil {
		return nil, err
	}
	return &view, nil
}

// ListPersons returns registered persons, optionally filtered by department.
func (s *Service) ListPersons(departmentID string) ([]PersonView, error) {
	deptID := strings.TrimSpace(departmentID)
	if deptID != "" {
		if _, ok, err := s.store.GetDepartment(deptID); err != nil {
			return nil, err
		} else if !ok {
			return nil, NotFoundf("department %q not found", departmentID)
		}
	}
	persons, err := s.store.ListPersons(deptID)
	if err != nil {
		return nil, err
	}
	return s.viewAll(persons)
}

// GetPerson looks a person up by its registered id or by employee number.
// Requirement 4.
func (s *Service) GetPerson(id string) (*PersonView, error) {
	key := strings.TrimSpace(id)
	if key == "" {
		return nil, Validationf("person id is required")
	}

	person, ok, err := s.store.GetPerson(key)
	if err != nil {
		return nil, err
	}
	if !ok {
		person, ok, err = s.store.GetPersonByEmployeeNo(key)
		if err != nil {
			return nil, err
		}
	}
	if !ok {
		return nil, NotFoundf("person %q not found", id)
	}
	view, err := s.view(*person)
	if err != nil {
		return nil, err
	}
	return &view, nil
}

func (s *Service) view(p Person) (PersonView, error) {
	view := PersonView{Person: p}
	dept, ok, err := s.store.GetDepartment(p.DepartmentID)
	if err != nil {
		return PersonView{}, err
	}
	if ok {
		view.DepartmentName = dept.Name
	}
	return view, nil
}

func (s *Service) viewAll(persons []Person) ([]PersonView, error) {
	views := make([]PersonView, 0, len(persons))
	for _, p := range persons {
		view, err := s.view(p)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}
