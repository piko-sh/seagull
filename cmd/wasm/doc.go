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

//go:build js && wasm

// Command wasm exposes Seagull's playground API through WebAssembly.
//
// Load it in a Web Worker with the Go distribution's wasm_exec.js. After startup, it sets
// self.seagull and calls self.seagullReady when provided. Its synchronous functions
// exchange strings and report failures as JSON objects with an error field.
//
// Build with:
//
//	GOOS=js GOARCH=wasm go build -o seagull.wasm .
//
// Run with -selftest to generate the SQLite starter project and check the result.
package main
