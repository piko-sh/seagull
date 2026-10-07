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

package emitkit

import "strings"

const (
	// ignoredStemPrefix starts a stem the Go tool would ignore, since it begins with an
	// underscore or a full stop.
	ignoredStemPrefix = "q"

	// constraintBreaker ends a stem whose last elements read as a build constraint.
	constraintBreaker = "_sql"

	// testElement is the last element go/build drops before reading a constraint.
	testElement = "test"
)

var (
	// knownOperatingSystems holds the GOOS values go/build reads in a file name, copied from
	// the Go toolchain's go/build/syslist.go (internal/syslist.KnownOS).
	knownOperatingSystems = map[string]struct{}{
		"aix": {}, "android": {}, "darwin": {}, "dragonfly": {}, "freebsd": {}, "hurd": {},
		"illumos": {}, "ios": {}, "js": {}, "linux": {}, "nacl": {}, "netbsd": {},
		"openbsd": {}, "plan9": {}, "solaris": {}, "wasip1": {}, "windows": {}, "zos": {},
	}

	// knownArchitectures holds the GOARCH values go/build reads in a file name, copied from
	// the Go toolchain's go/build/syslist.go (internal/syslist.KnownArch).
	knownArchitectures = map[string]struct{}{
		"386": {}, "amd64": {}, "amd64p32": {}, "arm": {}, "armbe": {}, "arm64": {},
		"arm64be": {}, "loong64": {}, "mips": {}, "mipsle": {}, "mips64": {}, "mips64le": {},
		"mips64p32": {}, "mips64p32le": {}, "ppc": {}, "ppc64": {}, "ppc64le": {}, "riscv": {},
		"riscv64": {}, "s390": {}, "s390x": {}, "sparc": {}, "sparc64": {}, "wasm": {},
	}
)

// sanitiseStem makes a query filename safe for generated Go source.
//
// It adjusts names that Go would ignore or treat as platform-specific files.
//
// Takes stem (string) which is the file name without its extension.
//
// Returns string which is the safe stem.
func sanitiseStem(stem string) string {
	if stem == "" || stem[0] == '_' || stem[0] == '.' {
		stem = ignoredStemPrefix + stem
	}
	head, tail, dotted := strings.Cut(stem, ".")
	if readsAsBuildConstraint(head) {
		head += constraintBreaker
	}
	if dotted {
		return head + "." + tail
	}
	return head
}

// readsAsBuildConstraint reports whether go/build reads a file name, cut at its first
// full stop, as constrained to an operating system or architecture: it holds an
// underscore, and after the first one its last element, once a trailing test element is
// dropped, is a known GOOS or GOARCH.
//
// Takes name (string) which is the file name before its first full stop.
//
// Returns bool which is true when the name constrains the file.
func readsAsBuildConstraint(name string) bool {
	_, suffix, found := strings.Cut(name, "_")
	if !found {
		return false
	}
	elements := strings.Split(suffix, "_")
	if last := len(elements) - 1; elements[last] == testElement {
		elements = elements[:last]
	}
	if len(elements) == 0 {
		return false
	}
	last := elements[len(elements)-1]
	_, operatingSystem := knownOperatingSystems[last]
	_, architecture := knownArchitectures[last]
	return operatingSystem || architecture
}
