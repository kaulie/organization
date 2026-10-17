package org

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// postgresAdvisoryLock is held for the duration of a JSON import transaction so
// two processes starting together cannot both decide the database is empty and
// insert the same snapshot.
const postgresAdvisoryLock int64 = 4244

// PostgresStore is the Store backed by PostgreSQL. Every mutation runs in a
// transaction: id allocation, uniqueness and the insert commit together, or
// none of them do.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// OpenPostgres connects to PostgreSQL and creates the schema if it is missing.
// dsn is a URL (postgres://...) or a keyword string. The caller owns Close.
func OpenPostgres(ctx context.Context, dsn string) (*PostgresStore, error) {
	if dsn == "" {
		return nil, fmt.Errorf("open postgres: empty database url")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: parse database url: %w", err)
	}
	cfg.MaxConns = 10
	cfg.MinConns = 1
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second

	// The pool keeps a background goroutine for idle connections. That goroutine
	// uses the context passed to NewWithConfig, so it must not be the caller's
	// startup timeout — cancelling that context would drop the pool's connections.
	if ctx == nil {
		ctx = context.Background()
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	s := &PostgresStore{pool: pool}
	if err := s.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	if err := s.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the connection pool.
func (s *PostgresStore) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

// Ping checks that the database accepts a query.
func (s *PostgresStore) Ping(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := s.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}
	return nil
}

func (s *PostgresStore) migrate(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS departments (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			name_key TEXT NOT NULL UNIQUE,
			type TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS persons (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			department_id TEXT NOT NULL REFERENCES departments (id),
			employee_no TEXT NOT NULL UNIQUE,
			created_at TIMESTAMPTZ NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS id_sequences (
			name TEXT PRIMARY KEY,
			value INTEGER NOT NULL CHECK (value >= 0)
		)`,
		`INSERT INTO id_sequences (name, value) VALUES ('department', 0), ('employee', 0)
			ON CONFLICT (name) DO NOTHING`,
	}
	for _, stmt := range statements {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("migrate postgres schema: %w", err)
		}
	}
	return nil
}

func (s *PostgresStore) opCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 8*time.Second)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func nextSeq(ctx context.Context, tx pgx.Tx, name string) (int, error) {
	var n int
	err := tx.QueryRow(ctx,
		`UPDATE id_sequences SET value = value + 1 WHERE name = $1 RETURNING value`, name).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// CreateDepartment inserts a department and assigns the next D-id.
func (s *PostgresStore) CreateDepartment(name string, t DepartmentType) (*Department, error) {
	ctx, cancel := s.opCtx()
	defer cancel()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, Internalf("begin department create: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var existing string
	err = tx.QueryRow(ctx, `SELECT id FROM departments WHERE name_key = $1`, normalizeKey(name)).Scan(&existing)
	if err == nil {
		return nil, Conflictf("department name %q already exists (id=%s)", name, existing)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, Internalf("check department name: %v", err)
	}

	n, err := nextSeq(ctx, tx, "department")
	if err != nil {
		return nil, Internalf("allocate department id: %v", err)
	}
	dept := &Department{
		ID:        fmt.Sprintf("%s%0*d", departmentIDPrefix, departmentIDWidth, n),
		Name:      name,
		Type:      t,
		CreatedAt: time.Now().UTC(),
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO departments (id, name, name_key, type, created_at) VALUES ($1, $2, $3, $4, $5)`,
		dept.ID, dept.Name, normalizeKey(dept.Name), string(dept.Type), dept.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, Conflictf("department name %q already exists", name)
		}
		return nil, Internalf("insert department: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, Internalf("commit department: %v", err)
	}
	return dept, nil
}

// RenameDepartment updates the display name. The id, type and created time stay.
func (s *PostgresStore) RenameDepartment(id, name string) (*Department, error) {
	ctx, cancel := s.opCtx()
	defer cancel()

	var dept Department
	var typ string
	err := s.pool.QueryRow(ctx,
		`UPDATE departments SET name = $1, name_key = $2 WHERE id = $3
		 RETURNING id, name, type, created_at`,
		name, normalizeKey(name), id).Scan(&dept.ID, &dept.Name, &typ, &dept.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, NotFoundf("department %q not found", id)
	}
	if isUniqueViolation(err) {
		return nil, s.departmentNameConflict(ctx, name)
	}
	if err != nil {
		return nil, Internalf("rename department: %v", err)
	}
	dept.Type = DepartmentType(typ)
	dept.CreatedAt = dept.CreatedAt.UTC()
	return &dept, nil
}

func (s *PostgresStore) departmentNameConflict(ctx context.Context, name string) error {
	var id string
	err := s.pool.QueryRow(ctx, `SELECT id FROM departments WHERE name_key = $1`, normalizeKey(name)).Scan(&id)
	if err != nil {
		return Conflictf("department name %q already exists", name)
	}
	return Conflictf("department name %q already exists (id=%s)", name, id)
}

// GetDepartment loads one department by id.
func (s *PostgresStore) GetDepartment(id string) (*Department, bool, error) {
	ctx, cancel := s.opCtx()
	defer cancel()

	dept, err := scanDepartment(s.pool.QueryRow(ctx,
		`SELECT id, name, type, created_at FROM departments WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, Internalf("get department: %v", err)
	}
	return dept, true, nil
}

// ListDepartments returns every department ordered by id.
func (s *PostgresStore) ListDepartments() ([]Department, error) {
	ctx, cancel := s.opCtx()
	defer cancel()

	rows, err := s.pool.Query(ctx,
		`SELECT id, name, type, created_at FROM departments ORDER BY id`)
	if err != nil {
		return nil, Internalf("list departments: %v", err)
	}
	defer rows.Close()

	out := make([]Department, 0)
	for rows.Next() {
		dept, err := scanDepartment(rows)
		if err != nil {
			return nil, Internalf("list departments: %v", err)
		}
		out = append(out, *dept)
	}
	if err := rows.Err(); err != nil {
		return nil, Internalf("list departments: %v", err)
	}
	return out, nil
}

// RegisterPerson inserts a person and assigns the next employee number.
func (s *PostgresStore) RegisterPerson(name, id string, t PersonType, departmentID string) (*Person, error) {
	ctx, cancel := s.opCtx()
	defer cancel()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, Internalf("begin person register: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var existing string
	err = tx.QueryRow(ctx, `SELECT id FROM persons WHERE id = $1`, id).Scan(&existing)
	if err == nil {
		return nil, Conflictf("person id %q is already registered", id)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, Internalf("check person id: %v", err)
	}

	err = tx.QueryRow(ctx, `SELECT id FROM departments WHERE id = $1`, departmentID).Scan(&existing)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, NotFoundf("department %q not found", departmentID)
	}
	if err != nil {
		return nil, Internalf("check department: %v", err)
	}

	n, err := nextSeq(ctx, tx, "employee")
	if err != nil {
		return nil, Internalf("allocate employee number: %v", err)
	}
	person := &Person{
		ID:           id,
		Name:         name,
		Type:         t,
		DepartmentID: departmentID,
		EmployeeNo:   fmt.Sprintf("%s%0*d", employeeNoPrefix, employeeNoWidth, n),
		CreatedAt:    time.Now().UTC(),
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO persons (id, name, type, department_id, employee_no, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		person.ID, person.Name, string(person.Type), person.DepartmentID, person.EmployeeNo, person.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, Conflictf("person id %q is already registered", id)
		}
		return nil, Internalf("insert person: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, Internalf("commit person: %v", err)
	}
	return person, nil
}

// GetPerson loads a person by the caller-supplied id.
func (s *PostgresStore) GetPerson(id string) (*Person, bool, error) {
	return s.getPerson(`SELECT id, name, type, department_id, employee_no, created_at FROM persons WHERE id = $1`, id)
}

// GetPersonByEmployeeNo loads a person by the system-assigned employee number.
func (s *PostgresStore) GetPersonByEmployeeNo(employeeNo string) (*Person, bool, error) {
	return s.getPerson(`SELECT id, name, type, department_id, employee_no, created_at FROM persons WHERE employee_no = $1`, employeeNo)
}

func (s *PostgresStore) getPerson(query, key string) (*Person, bool, error) {
	ctx, cancel := s.opCtx()
	defer cancel()

	person, err := scanPerson(s.pool.QueryRow(ctx, query, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, Internalf("get person: %v", err)
	}
	return person, true, nil
}

// ListPersons returns members of one department, or everyone when departmentID is empty.
func (s *PostgresStore) ListPersons(departmentID string) ([]Person, error) {
	ctx, cancel := s.opCtx()
	defer cancel()

	query := `SELECT id, name, type, department_id, employee_no, created_at FROM persons`
	args := []any{}
	if departmentID != "" {
		query += ` WHERE department_id = $1`
		args = append(args, departmentID)
	}
	query += ` ORDER BY employee_no`

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, Internalf("list persons: %v", err)
	}
	defer rows.Close()

	out := make([]Person, 0)
	for rows.Next() {
		person, err := scanPerson(rows)
		if err != nil {
			return nil, Internalf("list persons: %v", err)
		}
		out = append(out, *person)
	}
	if err := rows.Err(); err != nil {
		return nil, Internalf("list persons: %v", err)
	}
	return out, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDepartment(row rowScanner) (*Department, error) {
	var dept Department
	var typ string
	if err := row.Scan(&dept.ID, &dept.Name, &typ, &dept.CreatedAt); err != nil {
		return nil, err
	}
	dept.Type = DepartmentType(typ)
	dept.CreatedAt = dept.CreatedAt.UTC()
	return &dept, nil
}

func scanPerson(row rowScanner) (*Person, error) {
	var person Person
	var typ string
	if err := row.Scan(&person.ID, &person.Name, &typ, &person.DepartmentID, &person.EmployeeNo, &person.CreatedAt); err != nil {
		return nil, err
	}
	person.Type = PersonType(typ)
	person.CreatedAt = person.CreatedAt.UTC()
	return &person, nil
}

// ImportFileIfEmpty copies a JSON snapshot into PostgreSQL when the database
// has no departments yet. The file is the previous on-disk store
// (org-store.json). A database that already has rows is left untouched, so a
// restart never duplicates the snapshot. A missing file is not an error.
// A file that cannot be decoded is an error only while the database is empty:
// refusing to start is safer than going live with a blank organization.
func (s *PostgresStore) ImportFileIfEmpty(ctx context.Context, path string) (bool, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read data file %s: %w", path, err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return false, nil
	}

	var state storeFile
	decodeErr := json.Unmarshal(raw, &state)
	if decodeErr == nil && state.Version > storeFileVersion {
		decodeErr = fmt.Errorf("data file %s has version %d, this binary understands up to %d",
			path, state.Version, storeFileVersion)
	}
	if decodeErr != nil {
		empty, emptyErr := s.isEmpty(ctx)
		if emptyErr != nil {
			return false, emptyErr
		}
		if !empty {
			return false, nil
		}
		return false, fmt.Errorf("decode data file %s: %w", path, decodeErr)
	}
	return s.importState(ctx, state)
}

func (s *PostgresStore) isEmpty(ctx context.Context) (bool, error) {
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM departments`).Scan(&n); err != nil {
		return false, fmt.Errorf("count departments: %w", err)
	}
	return n == 0, nil
}

func (s *PostgresStore) importState(ctx context.Context, state storeFile) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin import: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, postgresAdvisoryLock); err != nil {
		return false, fmt.Errorf("lock import: %w", err)
	}

	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM departments`).Scan(&n); err != nil {
		return false, fmt.Errorf("count departments: %w", err)
	}
	if n > 0 {
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit import skip: %w", err)
		}
		return false, nil
	}

	for _, dept := range state.Departments {
		_, err := tx.Exec(ctx,
			`INSERT INTO departments (id, name, name_key, type, created_at) VALUES ($1, $2, $3, $4, $5)`,
			dept.ID, dept.Name, normalizeKey(dept.Name), string(dept.Type), dept.CreatedAt.UTC())
		if err != nil {
			return false, fmt.Errorf("import department %s: %w", dept.ID, err)
		}
	}
	for _, person := range state.Persons {
		_, err := tx.Exec(ctx,
			`INSERT INTO persons (id, name, type, department_id, employee_no, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			person.ID, person.Name, string(person.Type), person.DepartmentID, person.EmployeeNo, person.CreatedAt.UTC())
		if err != nil {
			return false, fmt.Errorf("import person %s: %w", person.ID, err)
		}
	}

	deptSeq := state.DeptSeq
	for _, dept := range state.Departments {
		if n := numericSuffix(dept.ID, departmentIDPrefix); n > deptSeq {
			deptSeq = n
		}
	}
	empSeq := state.EmpSeq
	for _, person := range state.Persons {
		if n := numericSuffix(person.EmployeeNo, employeeNoPrefix); n > empSeq {
			empSeq = n
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE id_sequences SET value = $1 WHERE name = 'department'`, deptSeq); err != nil {
		return false, fmt.Errorf("import department sequence: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE id_sequences SET value = $1 WHERE name = 'employee'`, empSeq); err != nil {
		return false, fmt.Errorf("import employee sequence: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit import: %w", err)
	}
	return len(state.Departments) > 0 || len(state.Persons) > 0 || deptSeq > 0 || empSeq > 0, nil
}

// resetForTest wipes rows so integration tests start from an empty database.
func (s *PostgresStore) resetForTest(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `TRUNCATE TABLE persons, departments`)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE id_sequences SET value = 0`)
	return err
}
