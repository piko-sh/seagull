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

package valuereading

import (
	"embed"

	"seagull.sh/seagull/internal/gogen/helpers"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// File is the generated file holding the readers.
	File = "value_reading.go"
)

var (
	// Helpers lists the helper-prefix suffixes of the identifiers the readers' file
	// declares, so an emitter can reserve them.
	Helpers = []string{"MapEntries", "OrderedMap", "ReadList", "ReadMap", "ReadScalar", "ScalarHolds", "ScalarKind", "Unreadable"}

	// source holds the readers' source.
	//
	//go:embed source/value_reading.go
	source embed.FS

	// preparedSources caches the parsed source.
	preparedSources = helpers.NewSources(source, "source")
)

// Render renders value_reading.go, the readers of a list, a map and a scalar, each
// element through the reader of the driver the package uses, which the generated code
// passes them.
//
// Takes vocabulary (helpers.Vocabulary) which names the generated package's identifiers.
// Takes packageName (string) which is the generated package's name.
//
// Returns sqlmodel.GeneratedFile which is the rendered file.
// Returns error when the source cannot be read or rendered.
func Render(vocabulary helpers.Vocabulary, packageName string) (sqlmodel.GeneratedFile, error) {
	return preparedSources.File(File, File, vocabulary, &helpers.Data{Flags: nil, Values: nil, Renames: nil, Package: packageName})
}
