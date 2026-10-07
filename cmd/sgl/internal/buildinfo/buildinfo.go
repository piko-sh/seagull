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

// Package buildinfo carries CLI version information injected at build time through
// -ldflags. It is kept in a tiny dedicated package so the linker flag string stays short
// and stable.
package buildinfo

import (
	"runtime/debug"

	"seagull.sh/seagull"
	"seagull.sh/seagull/cmd/internal/config"
)

// Version is the CLI release version. Build tooling sets it with -ldflags "-X
// 'seagull.sh/seagull/cmd/sgl/internal/buildinfo.Version=...'".
var Version = "dev"

// LibraryVersion returns the version of the linked seagull library.
//
// Returns string which is the library version without its leading v.
func LibraryVersion() string {
	return seagull.Version
}

// CodegenVersion returns the generated-code shape version of the linked library.
//
// Returns string which identifies the shape of the Go code the generator emits.
func CodegenVersion() string {
	return seagull.CodegenVersion
}

// ConfigSchemaVersion returns the sgl.yaml schema version the CLI reads, the version the
// configuration reader enforces.
//
// Returns int which is the config schema version.
func ConfigSchemaVersion() int {
	return config.CurrentVersion
}

// GoVersion returns the Go toolchain version that compiled sgl.
//
// Returns string which is the Go toolchain version, or "unknown".
func GoVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	return info.GoVersion
}
