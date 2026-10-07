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

package emit

import "errors"

var (
	// ErrRefused is wrapped by the error a built-in emitter's Emit returns for a request it
	// refuses rather than fails to write: one it cannot emit as it is, such as a nil query
	// or a naming plan that does not fit, and one its CheckerPort finds an error problem
	// with.
	//
	// A caller tells a refusal from a failure to generate the code with errors.Is.
	ErrRefused = errors.New("the emitter refuses the request")
)
