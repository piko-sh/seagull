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

// Package dialects constructs the built-in dialects by name.
//
// It is the single string-keyed entry point to the dialects: the sgl command and
// embedders that read a dialect name from configuration call New with the name and a
// plain-data Options value, and Names lists what New accepts. Code that knows its dialect
// at compile time calls the engine provider's own New, such as
// dialect_engine_postgres.New, instead.
package dialects
