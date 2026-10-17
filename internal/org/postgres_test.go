package org

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func openPostgresTestStore(t *testing.T) *PostgresStore {
	t.Helper()
	dsn := os.Getenv("ORG_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("ORG_POSTGRES_TEST_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	store, err := OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("OpenPostgres: %v", err)
	}
	t.Cleanup(store.Close)
	if err := store.resetForTest(ctx); err != nil {
		t.Fatalf("reset: %v", err)
	}
	return store
}

func TestPostgresStoreSurvivesReopen(t *testing.T) {
	store := openPostgresTestStore(t)
	dsn := os.Getenv("ORG_POSTGRES_TEST_DSN")
	svc := NewService(store)

	rd := mustCreateDepartment(t, svc, "研发中心", "研发")
	if _, err := svc.RegisterPerson(RegisterPersonRequest{
		Name: "Alice", ID: "user-001", Type: "human", DepartmentID: rd.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RenameDepartment(rd.ID, RenameDepartmentRequest{Name: "平台研发部"}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	reopenedStore, err := OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(reopenedStore.Close)
	reopened := NewService(reopenedStore)

	departments, err := reopened.ListDepartments()
	if err != nil {
		t.Fatal(err)
	}
	if len(departments) != 1 || departments[0].ID != rd.ID || departments[0].Name != "平台研发部" {
		t.Fatalf("departments after reopen = %+v, want the renamed %s", departments, rd.ID)
	}
	person, err := reopened.GetPerson("user-001")
	if err != nil {
		t.Fatal(err)
	}
	if person.EmployeeNo != "E0001" || person.DepartmentName != "平台研发部" {
		t.Errorf("person after reopen = %+v, want E0001 in 平台研发部", person)
	}
	byNo, err := reopened.GetPerson("E0001")
	if err != nil {
		t.Fatal(err)
	}
	if byNo.ID != "user-001" {
		t.Errorf("GetPerson(E0001) id = %q, want user-001", byNo.ID)
	}

	next := mustCreateDepartment(t, reopened, "测试组", "测试")
	if next.ID != "D0002" {
		t.Errorf("next department id = %q, want D0002", next.ID)
	}
	created, err := reopened.RegisterPerson(RegisterPersonRequest{
		Name: "Bob", ID: "user-002", Type: "agent", DepartmentID: next.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.EmployeeNo != "E0002" {
		t.Errorf("employeeNo = %q, want E0002", created.EmployeeNo)
	}

	if _, err := reopened.CreateDepartment(CreateDepartmentRequest{Name: "研发中心", Type: "研发"}); err != nil {
		t.Errorf("old name should be reusable after rename: %v", err)
	}
	if _, err := reopened.CreateDepartment(CreateDepartmentRequest{Name: " 平台研发部 ", Type: "研发"}); err == nil {
		t.Error("expected a conflict for the renamed name")
	} else {
		assertKind(t, err, KindConflict)
	}
}

func TestPostgresImportFileContinuesSequences(t *testing.T) {
	store := openPostgresTestStore(t)
	path := filepath.Join(t.TempDir(), "org-store.json")
	raw := []byte(`{
	  "version": 1,
	  "departments": [
	    {"id":"D0006","name":"IT组织部","type":"研发","createdAt":"2026-09-17T08:15:44.584887Z"},
	    {"id":"D0007","name":"创新业务部","type":"研发","createdAt":"2026-09-17T08:15:52.837601Z"}
	  ],
	  "persons": [
	    {"id":"user-001","name":"Kaulie","type":"HUMAN","departmentId":"D0006","employeeNo":"E0002","createdAt":"2026-09-17T10:25:16.052183Z"}
	  ],
	  "departmentSeq": 7,
	  "employeeSeq": 2
	}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	imported, err := store.ImportFileIfEmpty(ctx, path)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if !imported {
		t.Fatal("expected the snapshot to be imported into an empty database")
	}
	again, err := store.ImportFileIfEmpty(ctx, path)
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if again {
		t.Fatal("a second import must leave the existing rows untouched")
	}

	svc := NewService(store)
	departments, err := svc.ListDepartments()
	if err != nil {
		t.Fatal(err)
	}
	if len(departments) != 2 || departments[0].ID != "D0006" || departments[1].Name != "创新业务部" {
		t.Fatalf("departments = %+v", departments)
	}
	person, err := svc.GetPerson("E0002")
	if err != nil {
		t.Fatal(err)
	}
	if person.Name != "Kaulie" || person.DepartmentName != "IT组织部" {
		t.Fatalf("person = %+v", person)
	}

	next := mustCreateDepartment(t, svc, "新部门", "产品")
	if next.ID != "D0008" {
		t.Errorf("department id = %q, want D0008 (sequence continues past the snapshot)", next.ID)
	}
	created, err := svc.RegisterPerson(RegisterPersonRequest{
		Name: "Ada", ID: "user-ada", Type: "HUMAN", DepartmentID: next.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.EmployeeNo != "E0003" {
		t.Errorf("employeeNo = %q, want E0003", created.EmployeeNo)
	}

	// A corrupt snapshot must not block startup once PostgreSQL already has data.
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	imported, err = store.ImportFileIfEmpty(ctx, bad)
	if err != nil {
		t.Fatalf("corrupt file should be ignored when the database is not empty: %v", err)
	}
	if imported {
		t.Fatal("corrupt file must not import")
	}
}

func TestPostgresRejectsCorruptSnapshotWhenEmpty(t *testing.T) {
	store := openPostgresTestStore(t)
	path := filepath.Join(t.TempDir(), "org-store.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := store.ImportFileIfEmpty(ctx, path); err == nil {
		t.Fatal("expected an error for a corrupt snapshot when the database is empty")
	}
}

func TestPostgresImportsLiveSnapshotWhenPresent(t *testing.T) {
	path := os.Getenv("ORG_IMPORT_SNAPSHOT")
	if path == "" {
		t.Skip("ORG_IMPORT_SNAPSHOT is not set")
	}
	store := openPostgresTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	imported, err := store.ImportFileIfEmpty(ctx, path)
	if err != nil {
		t.Fatalf("import live snapshot: %v", err)
	}
	if !imported {
		t.Fatal("expected the live snapshot to import into an empty database")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state storeFile
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	departments, err := store.ListDepartments()
	if err != nil {
		t.Fatal(err)
	}
	persons, err := store.ListPersons("")
	if err != nil {
		t.Fatal(err)
	}
	if len(departments) != len(state.Departments) || len(persons) != len(state.Persons) {
		t.Fatalf("imported departments=%d persons=%d, snapshot has %d and %d",
			len(departments), len(persons), len(state.Departments), len(state.Persons))
	}
	deptSeq := state.DeptSeq
	for _, dept := range state.Departments {
		if n := numericSuffix(dept.ID, departmentIDPrefix); n > deptSeq {
			deptSeq = n
		}
	}
	svc := NewService(store)
	next := mustCreateDepartment(t, svc, "序列探针", "管理")
	wantID := fmt.Sprintf("%s%0*d", departmentIDPrefix, departmentIDWidth, deptSeq+1)
	if next.ID != wantID {
		t.Errorf("next id = %q, want %s", next.ID, wantID)
	}
}
