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

// Package sqlmodel holds the data model that emitters and embedders read: the structured
// SQLType, the schema catalogue with its tables, columns and functions, the analysed
// query with its typed parameters and output columns, the mapping from SQL types to Go
// types, and the generated files.
//
// A dialect and the analyser share these types too, but the vocabulary a dialect hands
// the analyser, such as parsed statements, expression trees and catalogue mutations,
// lives in internal/dialect/ast, which builds on sqlmodel. Nothing here imports it.
package sqlmodel
