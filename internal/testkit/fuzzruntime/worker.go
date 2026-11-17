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

package fuzzruntime

import "strings"

// workerBuildIgnore excludes worker sources from normal builds.
const workerBuildIgnore = "//go:build ignore\n\n"

// workerSource removes the build constraint from an embedded worker source.
//
// Takes source (string) which is the embedded worker source.
//
// Returns string which can be compiled into a generated program.
func workerSource(source string) string {
	return strings.Replace(source, workerBuildIgnore, "", 1)
}
