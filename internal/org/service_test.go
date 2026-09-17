package org

import (
	"errors"
	"testing"
)

func newTestService() *Service { return NewService(NewMemoryStore()) }

func mustCreateDepartment(t *testing.T, svc *Service, name, deptType string) *Department {
	t.Helper()
	dept, err := svc.CreateDepartment(CreateDepartmentRequest{Name: name, Type: deptType})
	if err != nil {
		t.Fatalf("CreateDepartment(%q, %q) failed: %v", name, deptType, err)
	}
	return dept
}

func assertKind(t *testing.T, err error, want ErrorKind) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s error, got nil", want)
	}
	if got := KindOf(err); got != want {
		t.Fatalf("expected error kind %s, got %s (%v)", want, got, err)
	}
}

func TestCreateDepartmentAcceptsAllTypes(t *testing.T) {
	svc := newTestService()
	cases := []struct {
		input string
		want  DepartmentType
	}{
		{"研发", DepartmentTypeRND},
		{"测试", DepartmentTypeTesting},
		{"产品", DepartmentTypeProduct},
		{"管理", DepartmentTypeManagement},
		{"rd", DepartmentTypeRND},
		{"QA", DepartmentTypeTesting},
		{"Product", DepartmentTypeProduct},
		{"management", DepartmentTypeManagement},
	}
	for i, tc := range cases {
		dept := mustCreateDepartment(t, svc, "部门-"+tc.input, tc.input)
		if dept.Type != tc.want {
			t.Errorf("case %d: type = %q, want %q", i, dept.Type, tc.want)
		}
		if dept.ID == "" {
			t.Errorf("case %d: department id must be assigned", i)
		}
	}
	if got := len(svc.ListDepartments()); got != len(cases) {
		t.Fatalf("ListDepartments() = %d departments, want %d", got, len(cases))
	}
}

func TestCreateDepartmentValidation(t *testing.T) {
	svc := newTestService()
	mustCreateDepartment(t, svc, "研发中心", "研发")

	if _, err := svc.CreateDepartment(CreateDepartmentRequest{Name: "  ", Type: "研发"}); err == nil {
		t.Error("expected error for empty name")
	} else {
		assertKind(t, err, KindValidation)
	}

	if _, err := svc.CreateDepartment(CreateDepartmentRequest{Name: "测试组", Type: "财务"}); err == nil {
		t.Error("expected error for invalid type")
	} else {
		assertKind(t, err, KindValidation)
	}

	// Duplicate names are rejected, whitespace and case insensitively.
	if _, err := svc.CreateDepartment(CreateDepartmentRequest{Name: " 研发中心 ", Type: "测试"}); err == nil {
		t.Error("expected conflict for duplicate name")
	} else {
		assertKind(t, err, KindConflict)
	}
}

func TestRegisterPersonAssignsEmployeeNo(t *testing.T) {
	svc := newTestService()
	rd := mustCreateDepartment(t, svc, "研发中心", "研发")

	alice, err := svc.RegisterPerson(RegisterPersonRequest{
		Name: "Alice", ID: "user-001", Type: "human", DepartmentID: rd.ID,
	})
	if err != nil {
		t.Fatalf("RegisterPerson failed: %v", err)
	}
	if alice.EmployeeNo != "E0001" {
		t.Errorf("employeeNo = %q, want E0001", alice.EmployeeNo)
	}
	if alice.Type != PersonTypeHuman {
		t.Errorf("type = %q, want HUMAN", alice.Type)
	}
	if alice.DepartmentName != "研发中心" {
		t.Errorf("departmentName = %q, want 研发中心", alice.DepartmentName)
	}

	bot, err := svc.RegisterPerson(RegisterPersonRequest{
		Name: "Cline", ID: "agent-001", Type: "AGENT", DepartmentID: rd.ID,
	})
	if err != nil {
		t.Fatalf("RegisterPerson(agent) failed: %v", err)
	}
	if bot.Type != PersonTypeAgent {
		t.Errorf("type = %q, want AGENT", bot.Type)
	}
	if bot.EmployeeNo != "E0002" {
		t.Errorf("employeeNo = %q, want E0002", bot.EmployeeNo)
	}
}

func TestRegisterPersonValidation(t *testing.T) {
	svc := newTestService()
	rd := mustCreateDepartment(t, svc, "研发中心", "研发")

	cases := []struct {
		name string
		req  RegisterPersonRequest
		want ErrorKind
	}{
		{"missing name", RegisterPersonRequest{ID: "u1", Type: "human", DepartmentID: rd.ID}, KindValidation},
		{"missing id", RegisterPersonRequest{Name: "Bob", Type: "human", DepartmentID: rd.ID}, KindValidation},
		{"bad type", RegisterPersonRequest{Name: "Bob", ID: "u2", Type: "robot", DepartmentID: rd.ID}, KindValidation},
		{"missing department", RegisterPersonRequest{Name: "Bob", ID: "u3", Type: "human"}, KindValidation},
		{"unknown department", RegisterPersonRequest{Name: "Bob", ID: "u4", Type: "human", DepartmentID: "D9999"}, KindNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.RegisterPerson(tc.req)
			assertKind(t, err, tc.want)
		})
	}

	if _, err := svc.RegisterPerson(RegisterPersonRequest{Name: "Bob", ID: "dup", Type: "human", DepartmentID: rd.ID}); err != nil {
		t.Fatalf("first registration failed: %v", err)
	}
	_, err := svc.RegisterPerson(RegisterPersonRequest{Name: "Bobby", ID: "dup", Type: "agent", DepartmentID: rd.ID})
	assertKind(t, err, KindConflict)
}

func TestGetDepartmentReturnsMembers(t *testing.T) {
	svc := newTestService()
	rd := mustCreateDepartment(t, svc, "研发中心", "研发")
	qa := mustCreateDepartment(t, svc, "测试组", "测试")

	if _, err := svc.RegisterPerson(RegisterPersonRequest{Name: "Alice", ID: "u1", Type: "human", DepartmentID: rd.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RegisterPerson(RegisterPersonRequest{Name: "Cline", ID: "a1", Type: "agent", DepartmentID: rd.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RegisterPerson(RegisterPersonRequest{Name: "Carol", ID: "u2", Type: "human", DepartmentID: qa.ID}); err != nil {
		t.Fatal(err)
	}

	detail, err := svc.GetDepartment(rd.ID)
	if err != nil {
		t.Fatalf("GetDepartment failed: %v", err)
	}
	if detail.ID != rd.ID || detail.Name != "研发中心" || detail.Type != DepartmentTypeRND {
		t.Errorf("unexpected department detail: %+v", detail.Department)
	}
	if len(detail.Members) != 2 {
		t.Fatalf("members = %d, want 2", len(detail.Members))
	}
	for _, m := range detail.Members {
		if m.DepartmentID != rd.ID || m.DepartmentName != "研发中心" {
			t.Errorf("member %+v not bound to %s", m, rd.ID)
		}
	}

	if _, err := svc.GetDepartment("D404"); err == nil {
		t.Error("expected error for unknown department")
	} else {
		assertKind(t, err, KindNotFound)
	}
}

func TestGetPersonByIDAndEmployeeNo(t *testing.T) {
	svc := newTestService()
	rd := mustCreateDepartment(t, svc, "研发中心", "研发")
	created, err := svc.RegisterPerson(RegisterPersonRequest{Name: "Alice", ID: "user-001", Type: "human", DepartmentID: rd.ID})
	if err != nil {
		t.Fatal(err)
	}

	byID, err := svc.GetPerson("user-001")
	if err != nil {
		t.Fatalf("GetPerson(id) failed: %v", err)
	}
	byNo, err := svc.GetPerson(created.EmployeeNo)
	if err != nil {
		t.Fatalf("GetPerson(employeeNo) failed: %v", err)
	}
	if byID.ID != byNo.ID || byID.EmployeeNo != byNo.EmployeeNo {
		t.Errorf("lookups disagree: %+v vs %+v", byID, byNo)
	}
	if byID.DepartmentName != "研发中心" {
		t.Errorf("departmentName = %q, want 研发中心", byID.DepartmentName)
	}

	if _, err := svc.GetPerson("nobody"); err == nil {
		t.Error("expected error for unknown person")
	} else {
		assertKind(t, err, KindNotFound)
	}
}

func TestListPersonsFiltersByDepartment(t *testing.T) {
	svc := newTestService()
	rd := mustCreateDepartment(t, svc, "研发中心", "研发")
	qa := mustCreateDepartment(t, svc, "测试组", "测试")
	for _, p := range []RegisterPersonRequest{
		{Name: "Alice", ID: "u1", Type: "human", DepartmentID: rd.ID},
		{Name: "Bob", ID: "u2", Type: "human", DepartmentID: qa.ID},
		{Name: "Cline", ID: "a1", Type: "AGENT", DepartmentID: rd.ID},
	} {
		if _, err := svc.RegisterPerson(p); err != nil {
			t.Fatal(err)
		}
	}

	all, err := svc.ListPersons("")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Errorf("ListPersons() = %d, want 3", len(all))
	}

	rdOnly, err := svc.ListPersons(rd.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rdOnly) != 2 {
		t.Errorf("ListPersons(%s) = %d, want 2", rd.ID, len(rdOnly))
	}
	if _, err := svc.ListPersons("D404"); err == nil {
		t.Error("expected error for unknown department filter")
	} else {
		assertKind(t, err, KindNotFound)
	}
}

func TestKindOfUnknownError(t *testing.T) {
	if got := KindOf(errors.New("boom")); got != KindInternal {
		t.Errorf("KindOf(unknown) = %s, want %s", got, KindInternal)
	}
}
func TestRenameDepartment(t *testing.T) {
	svc := newTestService()
	rd := mustCreateDepartment(t, svc, "研发中心", "研发")
	qa := mustCreateDepartment(t, svc, "测试组", "测试")

	// The name is trimmed, and id/type/createdAt survive the rename.
	renamed, err := svc.RenameDepartment(rd.ID, RenameDepartmentRequest{Name: "  基础研发中心  "})
	if err != nil {
		t.Fatalf("RenameDepartment failed: %v", err)
	}
	if renamed.Name != "基础研发中心" {
		t.Errorf("name = %q, want trimmed 基础研发中心", renamed.Name)
	}
	if renamed.ID != rd.ID || renamed.Type != DepartmentTypeRND {
		t.Errorf("id/type must be preserved: got %+v, want id=%s type=%s", renamed, rd.ID, DepartmentTypeRND)
	}
	if !renamed.CreatedAt.Equal(rd.CreatedAt) {
		t.Errorf("createdAt changed: %v -> %v", rd.CreatedAt, renamed.CreatedAt)
	}

	// The name index is rebuilt: the freed name becomes available again.
	if _, err := svc.CreateDepartment(CreateDepartmentRequest{Name: "研发中心", Type: "研发"}); err != nil {
		t.Errorf("old name should be reusable after rename: %v", err)
	}

	// Claiming a name that another department holds is a conflict, and the
	// comparison stays whitespace/case-insensitive.
	if _, err := svc.RenameDepartment(qa.ID, RenameDepartmentRequest{Name: " 基础研发中心 "}); err == nil {
		t.Error("expected conflict when renaming to an existing name")
	} else {
		assertKind(t, err, KindConflict)
	}

	// Renaming a department to its own (current) name is an idempotent success.
	if again, err := svc.RenameDepartment(qa.ID, RenameDepartmentRequest{Name: " 测试组 "}); err != nil {
		t.Errorf("re-renaming to own name should succeed: %v", err)
	} else if again.Name != "测试组" {
		t.Errorf("name = %q, want 测试组", again.Name)
	}

	if _, err := svc.RenameDepartment(rd.ID, RenameDepartmentRequest{Name: "   "}); err == nil {
		t.Error("expected validation error for empty name")
	} else {
		assertKind(t, err, KindValidation)
	}

	if _, err := svc.RenameDepartment("D404", RenameDepartmentRequest{Name: "无人部"}); err == nil {
		t.Error("expected not-found error for unknown department")
	} else {
		assertKind(t, err, KindNotFound)
	}
}

func TestRenameDepartmentKeepsMembersAndViews(t *testing.T) {
	svc := newTestService()
	rd := mustCreateDepartment(t, svc, "研发中心", "研发")
	if _, err := svc.RegisterPerson(RegisterPersonRequest{Name: "Alice", ID: "u1", Type: "human", DepartmentID: rd.ID}); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.RenameDepartment(rd.ID, RenameDepartmentRequest{Name: "平台研发部"}); err != nil {
		t.Fatalf("RenameDepartment failed: %v", err)
	}

	// Members stay attached to the same department id and see the new name.
	detail, err := svc.GetDepartment(rd.ID)
	if err != nil {
		t.Fatalf("GetDepartment failed: %v", err)
	}
	if detail.Name != "平台研发部" || detail.Type != DepartmentTypeRND {
		t.Errorf("unexpected department after rename: %+v", detail.Department)
	}
	if len(detail.Members) != 1 || detail.Members[0].DepartmentID != rd.ID {
		t.Fatalf("members not preserved: %+v", detail.Members)
	}
	if detail.Members[0].DepartmentName != "平台研发部" {
		t.Errorf("member departmentName = %q, want 平台研发部", detail.Members[0].DepartmentName)
	}

	person, err := svc.GetPerson("u1")
	if err != nil {
		t.Fatalf("GetPerson failed: %v", err)
	}
	if person.DepartmentID != rd.ID || person.DepartmentName != "平台研发部" {
		t.Errorf("person view = %+v, want department %s named 平台研发部", person, rd.ID)
	}

	// The department list reflects the new name under the same id.
	departments := svc.ListDepartments()
	if len(departments) != 1 || departments[0].Name != "平台研发部" {
		t.Errorf("ListDepartments() = %+v, want one department named 平台研发部", departments)
	}
}
