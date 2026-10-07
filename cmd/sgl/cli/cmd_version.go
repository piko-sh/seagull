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

package cli

import (
	"context"
	"fmt"
	"runtime"

	"seagull.sh/seagull/cmd/sgl/internal/buildinfo"
)

// runVersion prints the CLI, library, generated-code, config schema and Go versions.
//
// Takes arguments ([]string) which must be empty.
//
// Returns int which is the exit status.
func (inv *invocation) runVersion(_ context.Context, arguments []string) int {
	if status, proceed := inv.parseNoArguments(versionCommand, arguments); !proceed {
		return status
	}
	streams := inv.streams
	fmt.Fprintf(streams.Stdout, "%-14s %s\n", inv.program, buildinfo.Version)
	fmt.Fprintf(streams.Stdout, "library        %s\n", buildinfo.LibraryVersion())
	fmt.Fprintf(streams.Stdout, "codegen        %s\n", buildinfo.CodegenVersion())
	fmt.Fprintf(streams.Stdout, "config schema  %d\n", buildinfo.ConfigSchemaVersion())
	fmt.Fprintf(streams.Stdout, "go             %s (%s/%s)\n", buildinfo.GoVersion(), runtime.GOOS, runtime.GOARCH)
	return exitOK
}
