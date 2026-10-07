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

// Command 08_embedding generates the first tutorial checkpoint into a temporary
// directory.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	generator, err := seagull.New(dialect_engine_sqlite.New())
	if err != nil {
		return err
	}
	input := seagull.Input{Package: "store", Migrations: os.DirFS("../01_first_query"), MigrationDir: "db/migrations", QueryDir: "db/queries"}
	report, err := generator.Vet(ctx, input)
	if err != nil {
		return err
	}
	if err := report.Err(); err != nil {
		return err
	}
	result, err := generator.Generate(ctx, input)
	if err != nil {
		return err
	}
	if err := result.Err(); err != nil {
		return err
	}
	directory, err := os.MkdirTemp("", "seagull-generated-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	if err := seagull.WriteFiles(directory, result); err != nil {
		return err
	}
	fmt.Printf("generated %d queries\n", result.QueryCount)
	return nil
}
