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

package supervise

import (
	"fmt"
	"syscall"
)

// freeBytes returns the space an unprivileged process may still use on a filesystem.
//
// Takes path (string) which is any location on the filesystem.
//
// Returns uint64 which is the available size in bytes.
// Returns error when the filesystem cannot be read.
func freeBytes(path string) (uint64, error) {
	var status syscall.Statfs_t
	if err := syscall.Statfs(path, &status); err != nil {
		return 0, fmt.Errorf("reading free space at %s: %w", path, err)
	}
	return status.Bavail * uint64(status.Frsize), nil //nolint:gosec // A fragment size is positive.
}
