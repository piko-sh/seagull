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

// Command 052_custom_emitter generates this example's store package with an emitter that
// wraps the database/sql one and adds query_index.go, and prints the result.
//
// Run it with `go run .`, or `go run . -write` to write the package into internal/store,
// where it is committed so the build checks the generated code.
package main

import (
	"context"
	"flag"
	"log"
	"os"

	"seagull.sh/seagull"
)

// outputDirectory is where -write puts the generated package.
const outputDirectory = "internal/store"

func main() {
	write := flag.Bool("write", false, "write the package into internal/store instead of printing it")
	flag.Parse()
	if err := execute(context.Background(), *write); err != nil {
		log.Fatal(err)
	}
}

// execute prints the generated package, or writes it into internal/store.
//
// Takes write (bool) which writes instead of printing.
//
// Returns error when generation or writing fails.
func execute(ctx context.Context, write bool) error {
	project := os.DirFS(".")
	if !write {
		return run(ctx, project, os.Stdout)
	}
	result, err := generate(ctx, project)
	if err != nil {
		return err
	}
	return seagull.WriteFiles(outputDirectory, result)
}
