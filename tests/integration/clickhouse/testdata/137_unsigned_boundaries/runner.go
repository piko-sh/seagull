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
	queries := db.New(connection)
	for _, value := range []uint64{0, math.MaxInt64, 1 << 63, math.MaxUint64} {
		if err := queries.AddValue(ctx, value); err != nil {
			return err
		}
	}
	values, err := queries.ReadValues(ctx)
	if err != nil {
		return err
	}

	high := uint64(math.MaxUint64)
	selected, err := queries.ReadSlice(ctx, db.ReadSliceParams{Values: []uint64{1 << 63, high}})
	if err != nil || len(selected) != 2 || selected[1].Value != high {
		return fmt.Errorf("slice boundary: %v, %v", selected, err)
	}
	optional, err := queries.ReadOptional(ctx, db.ReadOptionalParams{Value: &high})
	if err != nil || len(optional) != 1 || optional[0].Value != high {
		return fmt.Errorf("optional boundary: %v, %v", optional, err)
	}
	count, err := queries.BrowseValues(high).Count(ctx)
	if err != nil || count != 1 {
		return fmt.Errorf("builder boundary: %v, %v", count, err)
	}

	filtered, err := queries.BrowseValues(0).Where("value", "=", high).Count(ctx)
	if err != nil || filtered != 1 {
		return fmt.Errorf("runtime filter boundary: %v, %v", filtered, err)
	}
	inFiltered, err := queries.BrowseValues(0).Where("value", "IN", []uint64{high}).Count(ctx)
	if err != nil || inFiltered != 1 {
		return fmt.Errorf("runtime list boundary: %v, %v", inFiltered, err)
	}
	for _, value := range []*uint64{nil, &high} {
		if err := queries.AddNullable(ctx, db.NullFrom(value)); err != nil {
			return err
		}
	}
	nullable, err := queries.ReadNullable(ctx)
	if err != nil || len(nullable) != 2 || nullable[0].Value.Valid || !nullable[1].Value.Valid || nullable[1].Value.V != high {
		return fmt.Errorf("nullable boundary: %v, %v", nullable, err)
	}
	return json.NewEncoder(os.Stdout).Encode(values)
}
