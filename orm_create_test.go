package orm

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// fakeDriver records the SQL it receives. Queries return one row whose id is
// the last id inserted; failNext makes the next statement fail with that error.
type fakeDriver struct {
	mu       sync.Mutex
	stmts    []string
	lastID   any
	failNext error
}

func (d *fakeDriver) Open(string) (driver.Conn, error) { return &fakeConn{d}, nil }

func (d *fakeDriver) record(query string, args []driver.NamedValue) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stmts = append(d.stmts, query)
	if err := d.failNext; err != nil {
		d.failNext = nil
		return err
	}
	if strings.HasPrefix(query, "INSERT") {
		cols := strings.Split(query[strings.Index(query, "(")+1:strings.Index(query, ")")], ", ")
		for i, c := range cols {
			if c == "id" {
				d.lastID = args[i].Value
			}
		}
		if d.lastID == nil {
			d.lastID = "db-generated"
		}
	}
	return nil
}

type fakeConn struct{ d *fakeDriver }

func (c *fakeConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not used") }
func (c *fakeConn) Close() error                        { return nil }
func (c *fakeConn) Begin() (driver.Tx, error)           { return nil, errors.New("not used") }

func (c *fakeConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if err := c.d.record(q, args); err != nil {
		return nil, err
	}
	return driver.RowsAffected(1), nil
}

func (c *fakeConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if err := c.d.record(q, args); err != nil {
		return nil, err
	}
	return &fakeRows{vals: [][]driver.Value{{c.d.lastID, "hello"}}}, nil
}

type fakeRows struct{ vals [][]driver.Value }

func (r *fakeRows) Columns() []string { return []string{"id", "title"} }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if len(r.vals) == 0 {
		return io.EOF
	}
	copy(dest, r.vals[0])
	r.vals = r.vals[1:]
	return nil
}

type post struct {
	ID    string `db:"id"`
	Title string `db:"title"`
}

var driverSeq struct {
	sync.Mutex
	n int
}

func openFake(t *testing.T) (*sql.DB, *fakeDriver) {
	t.Helper()
	driverSeq.Lock()
	driverSeq.n++
	name := fmt.Sprintf("orm-fake-%d", driverSeq.n)
	driverSeq.Unlock()
	d := &fakeDriver{}
	sql.Register(name, d)
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, d
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// MySQL has no INSERT … RETURNING: Create generates the id, inserts, then
// re-selects the row by that id.
func TestCreateWithoutReturning(t *testing.T) {
	db, d := openFake(t)
	data := map[string]any{"title": "hello"}
	row, err := For[post](db, DialectFor("mysql"), "posts").Create(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.stmts) != 2 {
		t.Fatalf("want INSERT then SELECT, got %q", d.stmts)
	}
	if strings.Contains(d.stmts[0], "RETURNING") {
		t.Errorf("MySQL insert must not use RETURNING: %q", d.stmts[0])
	}
	if !strings.HasPrefix(d.stmts[1], "SELECT * FROM posts WHERE id = ?") {
		t.Errorf("want a re-select by id, got %q", d.stmts[1])
	}
	if !uuidRe.MatchString(row.ID) {
		t.Errorf("id %q is not a v4 uuid", row.ID)
	}
	if _, ok := data["id"]; ok {
		t.Error("Create must not mutate the caller's map")
	}
}

// A caller-supplied id is kept as is.
func TestCreateWithoutReturningKeepsID(t *testing.T) {
	db, _ := openFake(t)
	row, err := For[post](db, DialectFor("mysql"), "posts").Create(context.Background(), map[string]any{"id": "given", "title": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if row.ID != "given" {
		t.Errorf("id = %q, want given", row.ID)
	}
}

// Postgres and SQLite keep the single INSERT … RETURNING round trip.
func TestCreateWithReturning(t *testing.T) {
	for _, drv := range []string{"pgx", "sqlite"} {
		db, d := openFake(t)
		if _, err := For[post](db, DialectFor(drv), "posts").Create(context.Background(), map[string]any{"title": "x"}); err != nil {
			t.Fatal(err)
		}
		if len(d.stmts) != 1 || !strings.HasSuffix(d.stmts[0], " RETURNING *") {
			t.Errorf("%s: want one INSERT … RETURNING *, got %q", drv, d.stmts)
		}
	}
}

// pgErr mimics pgconn.PgError, which reports its code through SQLState().
type pgErr struct{ code string }

func (e *pgErr) Error() string    { return "pg error " + e.code }
func (e *pgErr) SQLState() string { return e.code }

// A data exception (class 22, e.g. 22P02 for a malformed uuid) is ErrInvalidInput
// on every terminal method; other errors are not.
func TestInvalidInputClassified(t *testing.T) {
	ctx := context.Background()
	ops := map[string]func(*sql.DB) error{
		"Find": func(db *sql.DB) error {
			_, err := For[post](db, DialectFor("pgx"), "posts").Find(ctx, "not-a-uuid")
			return err
		},
		"Create": func(db *sql.DB) error {
			_, err := For[post](db, DialectFor("pgx"), "posts").Create(ctx, map[string]any{"title": "x"})
			return err
		},
		"Update": func(db *sql.DB) error {
			return For[post](db, DialectFor("pgx"), "posts").Where("id", "=", "bad").Update(ctx, map[string]any{"title": "x"})
		},
		"Delete": func(db *sql.DB) error {
			return For[post](db, DialectFor("pgx"), "posts").Where("id", "=", "bad").Delete(ctx)
		},
	}
	for name, op := range ops {
		db, d := openFake(t)
		d.failNext = &pgErr{"22P02"}
		err := op(db)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: %v is not ErrInvalidInput", name, err)
		}
		var pe *pgErr
		if !errors.As(err, &pe) || pe.code != "22P02" {
			t.Errorf("%s: driver error not reachable via errors.As: %v", name, err)
		}

		db2, d2 := openFake(t)
		d2.failNext = &pgErr{"23505"} // unique_violation: not the caller's input format
		if err := op(db2); err == nil || errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: 23505 should stay a plain error, got %v", name, err)
		}
	}
}
