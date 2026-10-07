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

package dialecttest

import "errors"

var (
	// errNoGoldenCases is returned when a directory holds no golden case.
	errNoGoldenCases = errors.New("no golden cases found")

	// errFileTooLarge is returned when a file the kit reads is larger than its bound.
	errFileTooLarge = errors.New("the file is larger than the kit reads")

	// errSpecTrailingContent is returned when a testspec.json holds more than one JSON
	// value.
	errSpecTrailingContent = errors.New("content follows the spec's JSON object")

	// errFuzzInputDeadline is the cause recorded when one fuzz input runs past its deadline.
	errFuzzInputDeadline = errors.New("the fuzz input ran past its deadline")

	// errUnexpectedGenerateError is returned when generation of a fuzz input fails with an
	// error that is neither a limit nor malformed SQL.
	errUnexpectedGenerateError = errors.New("generation may fail only with a limit or malformed-SQL error")
)
