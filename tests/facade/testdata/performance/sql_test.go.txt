// Copyright 2026 PolitePixels Limited
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.

package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"testing"
)

type mockDriver struct{}
type mockConn struct{}
type mockRows struct{ n, i int }

var columns = []string{"id", "name", "age"}
var mockN int

func (mockDriver) Open(string) (driver.Conn, error)  { return mockConn{}, nil }
func (mockConn) Prepare(string) (driver.Stmt, error) { panic("unused") }
func (mockConn) Close() error                        { return nil }
func (mockConn) Begin() (driver.Tx, error)           { panic("unused") }
func (mockConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &mockRows{n: mockN}, nil
}
func (r *mockRows) Columns() []string { return columns }
func (r *mockRows) Close() error      { return nil }
func (r *mockRows) Next(dest []driver.Value) error {
	r.i++
	if r.i > r.n {
		return io.EOF
	}
	dest[0] = int64(1)
	dest[1] = "Alice"
	dest[2] = int64(42)
	return nil
}

var resultSink []ListUsersRow

func init() { sql.Register("seagull_perf_mock", mockDriver{}) }
func BenchmarkSQLRows(b *testing.B) {
	ctx := context.Background()
	db, err := sql.Open("seagull_perf_mock", "")
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	q := New(db)
	for _, n := range []int{0, 1, 100, 1000} {
		mockN = n
		for _, test := range []struct {
			name string
			run  func(context.Context) ([]ListUsersRow, error)
		}{{"Generated", q.ListUsers}} {
			b.Run(fmt.Sprintf("%s/N%d", test.name, n), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					resultSink, err = test.run(ctx)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func TestSQLScalarAllocationBudgets(t *testing.T) {
	db, err := sql.Open("seagull_perf_mock", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	q := New(db)
	ctx := context.Background()
	for _, c := range []struct {
		rows  int
		limit float64
	}{{0, 6}, {1, 8}, {1000, 25}} {
		mockN = c.rows
		count := testing.AllocsPerRun(20, func() {
			resultSink, err = q.ListUsers(ctx)
			if err != nil {
				panic(err)
			}
		})
		if count > c.limit {
			t.Fatalf("%d rows: %.0f allocations, budget %.0f", c.rows, count, c.limit)
		}
	}
}

type SelfReferencing struct {
	Value int64
	Self  *SelfReferencing
}

func (s *SelfReferencing) Scan(value any) error { s.Value = value.(int64); s.Self = s; return nil }
