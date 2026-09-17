// Package org contains the domain model, storage and business logic of the
// simple organization management service.
package org

import (
	"strings"
	"time"
)

// DepartmentType is the kind of a department. Canonical values are the Chinese
// names required by the product; English aliases are accepted on input.
type DepartmentType string

// Canonical department types.
const (
	DepartmentTypeRND        DepartmentType = "研发"
	DepartmentTypeTesting    DepartmentType = "测试"
	DepartmentTypeProduct    DepartmentType = "产品"
	DepartmentTypeManagement DepartmentType = "管理"
)

// DepartmentTypes lists the canonical department types in display order.
var DepartmentTypes = []DepartmentType{
	DepartmentTypeRND,
	DepartmentTypeTesting,
	DepartmentTypeProduct,
	DepartmentTypeManagement,
}

// departmentTypeAliases maps accepted input values to canonical types.
var departmentTypeAliases = map[string]DepartmentType{
	"研发": DepartmentTypeRND, "rd": DepartmentTypeRND, "r&d": DepartmentTypeRND,
	"dev": DepartmentTypeRND, "develop": DepartmentTypeRND, "development": DepartmentTypeRND,
	"research": DepartmentTypeRND, "engineering": DepartmentTypeRND,

	"测试": DepartmentTypeTesting, "test": DepartmentTypeTesting, "testing": DepartmentTypeTesting,
	"qa": DepartmentTypeTesting, "quality": DepartmentTypeTesting,

	"产品": DepartmentTypeProduct, "product": DepartmentTypeProduct, "pm": DepartmentTypeProduct,

	"管理": DepartmentTypeManagement, "management": DepartmentTypeManagement,
	"mgmt": DepartmentTypeManagement, "admin": DepartmentTypeManagement,
}

// ParseDepartmentType normalizes and validates a user supplied department type.
func ParseDepartmentType(raw string) (DepartmentType, error) {
	t, ok := departmentTypeAliases[strings.ToLower(strings.TrimSpace(raw))]
	if !ok {
		return "", Validationf("invalid department type %q, want one of %s",
			raw, strings.Join(DepartmentTypeNames(), ", "))
	}
	return t, nil
}

// DepartmentTypeNames returns the canonical department type names.
func DepartmentTypeNames() []string {
	names := make([]string, 0, len(DepartmentTypes))
	for _, t := range DepartmentTypes {
		names = append(names, string(t))
	}
	return names
}

// PersonType distinguishes human employees from agents.
type PersonType string

// Supported person types.
const (
	PersonTypeHuman PersonType = "HUMAN"
	PersonTypeAgent PersonType = "AGENT"
)

// PersonTypes lists the supported person types.
var PersonTypes = []PersonType{PersonTypeHuman, PersonTypeAgent}

// ParsePersonType normalizes and validates a user supplied person type.
func ParsePersonType(raw string) (PersonType, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "human", "person", "employee", "人", "人类":
		return PersonTypeHuman, nil
	case "agent", "bot", "ai", "智能体":
		return PersonTypeAgent, nil
	default:
		return "", Validationf("invalid person type %q, want one of %s",
			raw, strings.Join(PersonTypeNames(), ", "))
	}
}

// PersonTypeNames returns the supported person type names.
func PersonTypeNames() []string {
	names := make([]string, 0, len(PersonTypes))
	for _, t := range PersonTypes {
		names = append(names, string(t))
	}
	return names
}

// Department is a department of the organization.
type Department struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Type      DepartmentType `json:"type"`
	CreatedAt time.Time      `json:"createdAt"`
}

// Person is a registered member of the organization. Either a HUMAN or an
// AGENT. ID is supplied by the caller at registration, EmployeeNo is assigned
// by the system.
type Person struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Type         PersonType `json:"type"`
	DepartmentID string     `json:"departmentId"`
	EmployeeNo   string     `json:"employeeNo"`
	CreatedAt    time.Time  `json:"createdAt"`
}

// CreateDepartmentRequest is the payload for creating a department.
type CreateDepartmentRequest struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// RenameDepartmentRequest is the payload for renaming a department. Only the
// display name changes: the id, type, creation time and members are preserved.
type RenameDepartmentRequest struct {
	Name string `json:"name"`
}

// RegisterPersonRequest is the payload for registering a person.
type RegisterPersonRequest struct {
	Name         string `json:"name"`
	ID           string `json:"id"`
	Type         string `json:"type"`
	DepartmentID string `json:"departmentId"`
}

// Field length limits.
const (
	maxNameLength = 64
	maxIDLength   = 64
)

// validateName trims and validates a human readable name field.
func validateName(field, raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", Validationf("%s is required", field)
	}
	if len([]rune(name)) > maxNameLength {
		return "", Validationf("%s must not exceed %d characters", field, maxNameLength)
	}
	return name, nil
}
