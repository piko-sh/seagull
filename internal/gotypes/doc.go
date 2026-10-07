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

// Package gotypes maps SQL types to the Go types the generated code uses.
//
// The mapping is structural: it reads a type's category and attributes, never its engine
// name. An integer maps by its width and signedness, a float by its width, a temporal
// type by its kind, an IP address type by its network kind, and a map or a struct of
// named fields recursively into a Go map or struct of its parts' Go types. The types with
// no single natural Go type, such as exact decimals, wide integers, intervals and JSON,
// map through type slots that an emitter's TypeDefaulterPort and the preset can replace.
//
// TypeMapper builds the mapping table for one generation run. A configured type override
// names an engine's type, so it is the one place an engine name decides a mapping; it
// ranks above the structural mapping. The emitters look up each column's Go type in the
// table.
package gotypes
