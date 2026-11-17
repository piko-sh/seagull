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

//go:build linux || darwin

// Package reduce shrinks a failing Go fuzz input to a smaller one that fails the same way.
//
// The target's test binary is built once. Each candidate is written into the package's
// seed corpus under a reserved name, replayed alone and removed. A candidate counts only
// when it fails with the same identity as the original, meaning the same test, the same
// first failing source line and the same message once run-specific details are masked. The
// original and every accepted reduction must reproduce a set number of times, so an
// intermittent failure is refused rather than reduced. Budgets bound attempts and time,
// and the best reduction verified so far is kept when one runs out.
package reduce
