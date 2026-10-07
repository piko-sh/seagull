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

// Command 049_presets white-labels Seagull: it generates this example's store package
// with the stock vocabulary, converts the queries to the acme. prefix, and generates
// again with Acme's preset, its own header, helper names and query-names file.
//
// Run it with `go run .`; it writes into a temporary directory, or into the directory
// given with -out.
package main

import (
	"context"
	"flag"
	"log"
	"os"
)

func main() {
	out := flag.String("out", "", "directory to write the generated package into; a temporary one when empty")
	flag.Parse()
	if err := execute(context.Background(), *out); err != nil {
		log.Fatal(err)
	}
}

// execute runs the example, writing into directory or a temporary one when it is empty.
//
// Takes directory (string) which is the output directory, or empty.
//
// Returns error when the example fails.
func execute(ctx context.Context, directory string) error {
	if directory == "" {
		temporary, err := os.MkdirTemp("", "seagull-example-")
		if err != nil {
			return err
		}
		defer func() { _ = os.RemoveAll(temporary) }()
		directory = temporary
	}
	return run(ctx, os.DirFS("."), directory, os.Stdout)
}
