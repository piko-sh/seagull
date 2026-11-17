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
	"reflect"
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
		count, err = queries.ReadArray([]float64{value}).Count(ctx)
		if err != nil || count != 1 {
			return fmt.Errorf("array %g: %d, %v", value, count, err)
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
	count, err = queries.ReadNullableArray([]*float64{nil, &minimum}).Count(ctx)
	if err != nil || count != 1 {
		return fmt.Errorf("nullable array: %d, %v", count, err)
	}
	count, err = queries.ReadNestedArray([][]float64{{minimum}}).Count(ctx)
	if err != nil || count != 1 {
		return fmt.Errorf("nested array: %d, %v", count, err)
	}
	keyed := map[string]float64{"Float64 \\ \t '\n": minimum}
	count, err = queries.ReadMap(keyed).Count(ctx)
	if err != nil || count != 1 {
		return fmt.Errorf("map: %d, %v", count, err)
	}
	named, err := queries.ReadNamedMap(ctx, keyed)
	if err != nil || len(named) != 1 {
		return fmt.Errorf("named map: %v, %v", named, err)
	}
	method := reflect.ValueOf(queries).MethodByName("ReadTuple")
	tuple := reflect.New(method.Type().In(0)).Elem()
	if tuple.Kind() == reflect.Map {
		tuple = reflect.MakeMap(tuple.Type())
		tuple.SetMapIndex(reflect.ValueOf("score"), reflect.ValueOf(minimum))
		tuple.SetMapIndex(reflect.ValueOf("label"), reflect.ValueOf("'\\\t Float64"))
	} else {
		tuple.FieldByName("Score").SetFloat(minimum)
		tuple.FieldByName("Label").SetString("'\\\t Float64")
	}
	builder := method.Call([]reflect.Value{tuple})[0]
	counted := builder.MethodByName("Count").Call([]reflect.Value{reflect.ValueOf(ctx)})
	if counted[0].Int() != 1 || !counted[1].IsNil() {
		return fmt.Errorf("tuple: %v", counted)
	}
	if err := queries.AddFloatBatch(ctx, []db.AddFloatBatchParams{{Value: minimum * 2}, {Value: -minimum * 2}}); err != nil {
		return err
	}
	if err := queries.AddFloatRows(ctx, []db.AddFloatRowsParams{{Value: minimum * 3}, {Value: -minimum * 3}}); err != nil {
		return err
	}
	for _, value := range []float64{minimum * 2, -minimum * 2, minimum * 3, -minimum * 3} {
		rows, err := queries.ReadFloat(value).All(ctx)
		if err != nil || len(rows) != 1 || math.Float64bits(rows[0].Value) != math.Float64bits(value) {
			return fmt.Errorf("batch %g: %v, %v", value, rows, err)
		}
	}
	return json.NewEncoder(os.Stdout).Encode("ok")
}
