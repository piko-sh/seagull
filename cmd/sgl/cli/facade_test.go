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

package cli_test

import (
	"testing"

	"seagull.sh/seagull/cmd/sgl/cli"
	"seagull.sh/seagull/internal/apitest"
)

func TestCLIFacadeAPI(t *testing.T) {
	t.Parallel()

	surface := apitest.Surface{
		"Main":        cli.Main,
		"MainContext": cli.MainContext,

		"Option":               cli.Option(nil),
		"WithDialect":          cli.WithDialect,
		"WithDirectory":        cli.WithDirectory,
		"WithDriver":           cli.WithDriver,
		"WithEmitter":          cli.WithEmitter,
		"WithIO":               cli.WithIO,
		"WithMigrationBackend": cli.WithMigrationBackend,
		"WithName":             cli.WithName,

		"DialectFactory":          cli.DialectFactory(nil),
		"EmitterFactory":          cli.EmitterFactory(nil),
		"EmitterRequest":          cli.EmitterRequest{},
		"MigrationBackendFactory": cli.MigrationBackendFactory(nil),
	}

	apitest.Check(t, surface, "facade_test.golden.yaml")
}
