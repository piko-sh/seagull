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

// Package output holds the streams every sgl command writes to, so tests can capture
// them.
package output

import "io"

// IO bundles the standard streams a command reads and writes.
type IO struct {
	// Stdin is the input stream.
	Stdin io.Reader

	// Stdout is the primary output stream.
	Stdout io.Writer

	// Stderr is the error and diagnostic stream.
	Stderr io.Writer
}
