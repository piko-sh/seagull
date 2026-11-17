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

package sqlexec

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

const (
	// leaseOwnerSeparator separates the parts of a lease owner: its random token, the
	// process's ID and the process's scope.
	leaseOwnerSeparator = "."

	// leaseOwnerParts is how many parts an owner that names its process has.
	leaseOwnerParts = 3

	// leaseScopeBytes is how many bytes of the scope's digest an owner holds.
	leaseScopeBytes = 8
)

// processScope returns a digest of the machine, the host name and the PID namespace this
// process runs in, the scope in which its process ID names it, or the empty string where
// none of them can be read.
var processScope = sync.OnceValue(func() string {
	machine := firstReadable("/etc/machine-id", "/var/lib/dbus/machine-id")
	namespace, _ := os.Readlink("/proc/self/ns/pid")
	if machine == "" && namespace == "" {
		return ""
	}
	host, _ := os.Hostname()
	digest := sha256.Sum256([]byte(machine + "\x00" + host + "\x00" + namespace))
	return hex.EncodeToString(digest[:leaseScopeBytes])
})

// firstReadable returns the trimmed contents of the first of the files that can be read.
//
// Takes paths (...string) which are the files, in order.
//
// Returns string which is the contents, or empty when none can be read.
func firstReadable(paths ...string) string {
	for _, path := range paths {
		if contents, err := os.ReadFile(path); err == nil { //nolint:gosec // the paths are fixed system files
			if trimmed := strings.TrimSpace(string(contents)); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

// leaseOwner returns the token that identifies one acquisition's lease: random bytes and,
// where the process's scope can be read, the process's ID and that scope, so a later run
// in the same scope can tell that the process holding a lease has gone.
//
// Returns string which is the token, at most 64 characters long.
// Returns error when the system's randomness fails.
func leaseOwner() (string, error) {
	token := make([]byte, leaseOwnerBytes)
	if _, err := rand.Read(token); err != nil {
		return "", fmt.Errorf("generating a lease owner: %w", err)
	}
	owner := hex.EncodeToString(token)
	if scope := processScope(); scope != "" {
		owner += leaseOwnerSeparator + strconv.Itoa(os.Getpid()) + leaseOwnerSeparator + scope
	}
	return owner, nil
}

// leaseHolderGone reports whether the process that took a lease is known to have ended:
// it ran in this process's scope, is not this process, and no process has its ID.
//
// An owner from another machine, host or PID namespace, or one that names no process, is
// never known to have ended, and its lease is left to expire.
//
// Takes holder (string) which is the lease's owner.
//
// Returns bool which is true only when the holder has certainly ended.
func leaseHolderGone(holder string) bool {
	parts := strings.Split(holder, leaseOwnerSeparator)
	if len(parts) != leaseOwnerParts || parts[2] == "" || parts[2] != processScope() {
		return false
	}
	pid, err := strconv.Atoi(parts[1])
	if err != nil || pid <= 0 || pid == os.Getpid() {
		return false
	}
	return processGone(pid)
}
