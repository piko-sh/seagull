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

package seagull

import (
	"runtime/debug"
	"strings"
)

const (
	// CodegenVersion identifies the shape of the Go code the generator emits. It changes
	// whenever identical input would produce different output, so a host that pins generated
	// code (such as a framework that checks generated files into a repository) can detect
	// that regeneration is due without diffing the output.
	CodegenVersion = "3"

	// modulePath is this module's path as it appears in a binary's build info.
	modulePath = "seagull.sh/seagull"

	// develVersion is the Version of a binary that carries no released seagull.
	develVersion = "devel"
)

// Version holds the released semver of the seagull library module, without the leading v.
// Release builds set it using: go build -ldflags "-X seagull.sh/seagull.Version=1.0.0".
var Version string

func init() {
	if Version == "" {
		info, ok := debug.ReadBuildInfo()
		Version = moduleVersion(info, ok)
	}
}

// moduleVersion finds this module's version in a binary's build info.
//
// Takes info (*debug.BuildInfo) which is the binary's build info.
// Takes ok (bool) which reports whether the build info was available.
//
// Returns string which is the version without its leading v, or "devel" when the module
// is absent, replaced by a directory, or built from a working copy.
func moduleVersion(info *debug.BuildInfo, ok bool) string {
	if !ok || info == nil {
		return develVersion
	}

	module := &info.Main
	if module.Path != modulePath {
		module = nil
		for _, dependency := range info.Deps {
			if dependency.Path == modulePath {
				module = dependency
				break
			}
		}
	}
	if module == nil {
		return develVersion
	}
	if module.Replace != nil {
		module = module.Replace
	}

	if module.Version == "" || module.Version == "(devel)" {
		return develVersion
	}
	return strings.TrimPrefix(module.Version, "v")
}
