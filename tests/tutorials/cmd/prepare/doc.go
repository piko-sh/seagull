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

// Command prepare creates the practice workspace the Seagull tutorials start from.
//
// Usage, from anywhere inside a Seagull checkout:
//
//	go run ./tests/tutorials/cmd/prepare [-sgl FILE] DIRECTORY
//
// DIRECTORY must not exist yet and must lie outside the checkout. prepare copies the
// tutorial module's go.mod, go.sum and shared internal packages into it, then creates the
// 01_first_query, 07_postgres and 08_embedding lesson directories. Each holds the
// lesson's supplied files, a main.go whose run function only returns nil, and empty
// db/migrations, db/queries and db/seeds directories.
//
// The copied go.mod is pointed at the checkout and at the safeconv and safedisk
// directories beside it, and a new go.work uses both the checkout and the workspace.
// Finally prepare builds sgl from the checkout into DIRECTORY, or with -sgl copies an
// executable built already.
//
// The go command is the one the GO environment variable names, or go on PATH.
package main
