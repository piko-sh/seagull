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

// Package provenance records what a fuzz target's run tested, so a resumed campaign
// skips a target only when its passing run matches what would run now.
//
// A record names the target and its command, a fingerprint of every tracked and
// untracked source file outside caches, the module files, the toolchain with its
// compiler and linker hashes, and the target's seed corpus. Any difference, a missing
// field or an interrupted run runs the target again.
package provenance
