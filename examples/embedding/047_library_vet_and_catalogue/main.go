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

// Command 047_library_vet_and_catalogue is a small linter built on Seagull's analysis: it
// vets this example's queries from Go, prints what it learnt about each one, applies a
// rule of its own and prints the schema catalogue.
//
// Run it from this directory with `go run .`.
package main

import (
	"context"
	"log"
	"os"
)

func main() {
	if err := run(context.Background(), os.DirFS("."), os.Stdout); err != nil {
		log.Fatal(err)
	}
}
