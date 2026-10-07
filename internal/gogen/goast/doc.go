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

// Package goast holds the Go-syntax building blocks of the code generator.
//
// It provides the identifier constants the generated code uses, snake_case to Go name
// conversion, Go type resolution from the type mapping table, an import tracker that
// records the packages a generated file needs, and FormatFileWithAST, which renders a
// file's declarations through gofmt with its imports grouped the way goimports groups
// them.
package goast
