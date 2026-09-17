package org

import "strings"

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

// ListDepartments returns every department ordered by id.
func (s *Service) ListDepartments() []Department {
	return s.store.ListDepartments()
}

// GetDepartment returns a department and its members. Requirement 3.
func (s *Service) GetDepartment(id string) (*DepartmentDetail, error) {
	deptID := strings.TrimSpace(id)
	dept, ok := s.store.GetDepartment(deptID)
	if !ok {
		return nil, NotFoundf("department %q not found", id)
	}
	return &DepartmentDetail{
		Department: *dept,
		Members:    s.viewAll(s.store.ListPersons(dept.ID)),
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
	if _, ok := s.store.GetDepartment(departmentID); !ok {
		return nil, NotFoundf("department %q not found", req.DepartmentID)
	}

	person, err := s.store.RegisterPerson(name, personID, personType, departmentID)
	if err != nil {
		return nil, err
	}
	view := s.view(*person)
	return &view, nil
}

// ListPersons returns registered persons, optionally filtered by department.
func (s *Service) ListPersons(departmentID string) ([]PersonView, error) {
	deptID := strings.TrimSpace(departmentID)
	if deptID != "" {
		if _, ok := s.store.GetDepartment(deptID); !ok {
			return nil, NotFoundf("department %q not found", departmentID)
		}
	}
	return s.viewAll(s.store.ListPersons(deptID)), nil
}

// GetPerson looks a person up by its registered id or by employee number.
// Requirement 4.
func (s *Service) GetPerson(id string) (*PersonView, error) {
	key := strings.TrimSpace(id)
	if key == "" {
		return nil, Validationf("person id is required")
	}

	person, ok := s.store.GetPerson(key)
	if !ok {
		person, ok = s.store.GetPersonByEmployeeNo(key)
	}
	if !ok {
		return nil, NotFoundf("person %q not found", id)
	}
	view := s.view(*person)
	return &view, nil
}

func (s *Service) view(p Person) PersonView {
	view := PersonView{Person: p}
	if dept, ok := s.store.GetDepartment(p.DepartmentID); ok {
		view.DepartmentName = dept.Name
	}
	return view
}

func (s *Service) viewAll(persons []Person) []PersonView {
	views := make([]PersonView, 0, len(persons))
	for _, p := range persons {
		views = append(views, s.view(p))
	}
	return views
}
