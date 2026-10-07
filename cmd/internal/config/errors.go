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

package config

import "errors"

var (
	// ErrNotFound is returned when no configuration file exists in the directory or any
	// parent.
	ErrNotFound = errors.New("no sgl.yaml found in this directory or any parent")

	// ErrTooLarge is returned when the configuration file exceeds MaxFileBytes.
	ErrTooLarge = errors.New("configuration file is too large")

	// ErrInvalid is returned when the configuration fails validation.
	ErrInvalid = errors.New("invalid configuration")

	// ErrUnsetVariable is returned when a ${env:NAME} reference names a variable the lookup
	// cannot resolve. The error names the database, the key and the variable.
	ErrUnsetVariable = errors.New("unset environment variable")
)
