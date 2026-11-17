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

// Package fuzzbuilder models call sequences against generated runtime query builders.
//
// A version 16 input chooses table rows and a bounded sequence of builder calls, Where,
// OrderBy, Limit, Offset and the All, One and Count terminals, over a fixed catalogue of
// queries with aliased, cast, computed, grouped, compound and awkwardly named columns.
// The package evaluates every call against its own row model, never consulting Seagull's
// analysis, so the expected rows, order and counts are independent of the generated code
// under test.
package fuzzbuilder
