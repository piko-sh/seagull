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

package main

import (
	"case_runner/db"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	connection, err := sql.Open(databaseDriver, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer connection.Close()
	connection.SetMaxOpenConns(1)
	ctx := context.Background()
	if _, err := connection.ExecContext(ctx, `CREATE TABLE fuzz_floats (value DOUBLE PRECISION NOT NULL)`); err != nil {
		return err
	}
	if _, err := connection.ExecContext(ctx, `CREATE TABLE fuzz_nullable (value DOUBLE PRECISION)`); err != nil {
		return err
	}
	queries := db.New(connection)
	values := []float64{math.SmallestNonzeroFloat64, -math.SmallestNonzeroFloat64, float64(math.SmallestNonzeroFloat32), -float64(math.SmallestNonzeroFloat32), 1, math.MaxFloat64, 0}
	for _, value := range values {
		if err := queries.AddFloat(ctx, value); err != nil {
			return fmt.Errorf("insert %g: %w", value, err)
		}
	}
	for _, value := range values {
		rows, err := queries.ReadFloat(value).All(ctx)
		if err != nil || len(rows) != 1 || math.Float64bits(rows[0].Value) != math.Float64bits(value) {
			return fmt.Errorf("round trip %g: %v, %v", value, rows, err)
		}
		count, err := queries.ReadFloat(value).Count(ctx)
		if err != nil || count != 1 {
			return fmt.Errorf("count %g: %d, %v", value, count, err)
		}
		count, err = queries.ReadArithmetic(value).Count(ctx)
		if err != nil || count != 1 {
			return fmt.Errorf("arithmetic %g: %d, %v", value, count, err)
		}
		selected, err := queries.ReadSlice(ctx, db.ReadSliceParams{Value: []float64{value}})
		count = int64(len(selected))
		if err != nil || count != 1 {
			return fmt.Errorf("slice %g: %d, %v", value, count, err)
		}
		for _, operator := range []string{"=", "IN"} {
			var bound any = value
			if operator == "IN" {
				bound = []float64{value}
			}
			count, err = queries.BrowseFloats().Where("value", operator, bound).Count(ctx)
			if err != nil || count != 1 {
				return fmt.Errorf("filter %s %g: %d, %v", operator, value, count, err)
			}
		}
	}
	selected, err := queries.ReadSlice(ctx, db.ReadSliceParams{Value: []float64{math.SmallestNonzeroFloat64, 1}})
	if err != nil || len(selected) != 2 {
		return fmt.Errorf("multiple list values: %v, %v", selected, err)
	}
	minimum32 := float32(math.SmallestNonzeroFloat32)
	count, err := queries.BrowseFloats().Where("value", "=", &minimum32).Count(ctx)
	if err != nil || count != 1 {
		return fmt.Errorf("float32 pointer: %d, %v", count, err)
	}
	minimum := math.SmallestNonzeroFloat64
	for _, value := range []*float64{nil, &minimum} {
		if err := queries.AddNullable(ctx, db.NullFrom(value)); err != nil {
			return err
		}
	}
	count, err = queries.ReadNullable(db.NullOf(minimum)).Count(ctx)
	if err != nil || count != 1 {
		return fmt.Errorf("nullable: %d, %v", count, err)
	}
	count, err = queries.ReadNullable(db.Null[float64]{}).Count(ctx)
	if err != nil || count != 0 {
		return fmt.Errorf("NULL: %d, %v", count, err)
	}
	return json.NewEncoder(os.Stdout).Encode("ok")
}
