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

// Package session runs one playground generation: it takes a project held in memory as a
// map of slash-separated paths to file contents, reads its sgl.yaml the way sgl does,
// generates, vets and builds the catalogue, and returns a JSON-ready view of everything
// the playground shows.
//
// [Run] never panics and never reads the host: the project lives in an in-memory file
// system, and a panic anywhere in the run becomes a response with Internal set. The
// panic's stack is logged once, at warning level, through the logger the run's context
// carries, or slog.Default() when it carries none, which under WebAssembly writes to the
// console; nothing else is logged at that level. Parser limits that sgl.yaml raises are
// held to the dialects' defaults, and every request input is bounded before it is used.
// The package builds and is tested both natively and for GOOS=js, so the WebAssembly
// bridge and the native inspect command share one implementation.
//
// The generator is assembled from sgl.yaml by cmd/internal/assemble, as sgl assembles it,
// so the files a run returns are byte for byte the files sgl generate writes; the
// examples parity test holds the package to that. Starter files come from
// cmd/internal/starter and diagnostics are laid out by seagull.Present, so the playground
// shows what sgl init writes and what sgl vet prints.
package session
