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

// Package diag defines the diagnostics Seagull reports against SQL sources.
//
// A SourceError carries a stable code (Q001 onwards), a severity and the file, line and
// column it was raised at. Severity decides whether generation is blocked: errors stop
// it, warnings and hints do not. The codes are listed in diagnostic_codes.go and never
// reused.
//
// Present lays a diagnostic out for display, with its source line and carets under the
// span, and WriteText prints the result for a terminal. Neither reads a file: the caller
// supplies the source line, so every front end renders diagnostics the same way.
package diag
