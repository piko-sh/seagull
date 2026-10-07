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

// Package catalogue builds the schema catalogue the analyser resolves queries against.
//
// The migration provider replays every migration's DDL through the dialect, in version
// order, into a catalogue of schemas, tables, views, enums, composite types, sequences
// and functions, applying migration directives as it goes. View definitions and SQL
// function bodies are typed against the catalogue built so far, and a call graph
// propagates each function's data access through the functions it calls. Custom functions
// declared in configuration are available before migration replay begins.
package catalogue
