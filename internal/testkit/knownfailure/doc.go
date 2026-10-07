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

// Package knownfailure judges the outcome of a test kit's checks against the kit's record
// of the checks its subject is known to fail today.
//
// A kit runs named checks against one or more subjects, such as dialects. Each check
// passes, fails with problems, or does not apply. A known failure must still fail: one
// that now passes is reported, so the record cannot go stale. A key names a check for
// every subject the kit drives, or, with Separator, for one subject alone. A known
// failure that does not apply to the subject it names alone is reported too. Every record
// must give a reason.
package knownfailure
