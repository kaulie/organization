package org

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileStoreStartsEmptyWhenFileMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "org-store.json")

	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}
	if got := len(store.ListDepartments()); got != 0 {
		t.Fatalf("departments = %d, want 0 for a fresh store", got)
	}

	mustCreateDepartment(t, NewService(store), "研发中心", "研发")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("data file was not created: %v", err)
	}
}

// The whole point of the file store: this service is deployed by restarting the
// process, so departments, persons and id sequences must survive a restart.
func TestFileStoreSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "org-store.json")

	first, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}
	svc := NewService(first)
	rd := mustCreateDepartment(t, svc, "研发中心", "研发")
	if _, err := svc.RegisterPerson(RegisterPersonRequest{
		Name: "Alice", ID: "user-001", Type: "human", DepartmentID: rd.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RenameDepartment(rd.ID, RenameDepartmentRequest{Name: "平台研发部"}); err != nil {
		t.Fatal(err)
	}

	// Simulate a redeploy: a brand new process reading the same data directory.
	second, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	reopened := NewService(second)

	departments := reopened.ListDepartments()
	if len(departments) != 1 || departments[0].ID != rd.ID || departments[0].Name != "平台研发部" {
		t.Fatalf("departments after reopen = %+v, want the renamed %s", departments, rd.ID)
	}
	person, err := reopened.GetPerson("user-001")
	if err != nil {
		t.Fatalf("GetPerson after reopen failed: %v", err)
	}
	if person.EmployeeNo != "E0001" || person.DepartmentName != "平台研发部" {
		t.Errorf("person after reopen = %+v, want E0001 in 平台研发部", person)
	}

	// Ids keep counting from where they left off: no reuse, no duplicates.
	next := mustCreateDepartment(t, reopened, "测试组", "测试")
	if next.ID == rd.ID {
		t.Errorf("department id reused after reopen: %s", next.ID)
	}
	created, err := reopened.RegisterPerson(RegisterPersonRequest{
		Name: "Bob", ID: "user-002", Type: "agent", DepartmentID: next.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.EmployeeNo != "E0002" {
		t.Errorf("employeeNo = %q, want E0002 (the sequence must continue, not restart)", created.EmployeeNo)
	}

	// The name index survives the round trip too: the renamed (freed) name is
	// usable again while the new one stays taken.
	if _, err := reopened.CreateDepartment(CreateDepartmentRequest{Name: "研发中心", Type: "研发"}); err != nil {
		t.Errorf("old name should be reusable after reopen: %v", err)
	}
	if _, err := reopened.CreateDepartment(CreateDepartmentRequest{Name: "平台研发部", Type: "研发"}); err == nil {
		t.Error("expected a conflict for the renamed name after reopen")
	} else {
		assertKind(t, err, KindConflict)
	}
}

func TestFileStoreCreatesParentDirectories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backend", "data", "org-store.json")

	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}
	mustCreateDepartment(t, NewService(store), "研发中心", "研发")

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("data file missing after nested directories were created: %v", err)
	}
}

func TestFileStoreEmptyFileStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "org-store.json")
	if err := os.WriteFile(path, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("an empty data file should start an empty store: %v", err)
	}
	if got := len(store.ListDepartments()); got != 0 {
		t.Errorf("departments = %d, want 0", got)
	}
}

// A data file that exists but cannot be read must never be treated as "no data
// yet": silently starting empty would lose the user's data on disk.
func TestFileStoreRejectsUnreadableData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "org-store.json")

	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileStore(path); err == nil {
		t.Error("expected an error for a corrupt data file")
	}

	if err := os.WriteFile(path, []byte(`{"version":99,"departments":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileStore(path); err == nil {
		t.Error("expected an error for a data file written by a newer schema")
	}
}

// A mutation that cannot be persisted must fail loudly and leave no trace in
// memory, otherwise the API reports success while the data is lost on restart.
func TestFileStoreRollsBackWhenWriteFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory permissions are not enforced")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "org-store.json")

	store, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(store)
	mustCreateDepartment(t, svc, "研发中心", "研发")

	// A read-only directory stops the temp file from being created.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if _, err := svc.CreateDepartment(CreateDepartmentRequest{Name: "测试组", Type: "测试"}); err == nil {
		t.Fatal("expected an error when the data file cannot be written")
	} else {
		assertKind(t, err, KindInternal)
	}

	if got := len(svc.ListDepartments()); got != 1 {
		t.Errorf("departments = %d, want 1 (the failed create must be rolled back)", got)
	}
}
