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

// Package scaling checks that an operation finishes within a time budget and that its
// cost grows no faster than its input, for tests that guard against quadratic or
// exponential work on large or deeply nested input.
//
// Budgets are generous on purpose: they catch work that runs for seconds or minutes, not
// small regressions. A timing is measured up to three times and passes as soon as one
// measurement meets it, so a busy machine does not fail a test that work growing too fast
// fails every time. Under the race detector, which slows every operation, budgets are
// widened by a fixed factor. The package is imported only from tests.
package scaling
