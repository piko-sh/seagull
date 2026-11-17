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
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"

	"case_runner/db"
)

func main() {
	connection, err := sql.Open(databaseDriver, os.Getenv("DATABASE_URL"))
	if err != nil {
		panic(err)
	}
	defer connection.Close()
	queries := db.New(connection)
	output := make(map[string]string)
	for _, value := range []int64{-8, 8, 0, -9223372036854775808} {
		result, err := queries.NegateInteger(context.Background(), value)
		if err != nil {
			panic(err)
		}
		output[fmt.Sprint(value)] = result.Value.V.String()
	}
	if err := json.NewEncoder(os.Stdout).Encode(output); err != nil {
		panic(err)
	}
}
