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

package schema

import (
	"context"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/sqlmodel"
)

// SchemaSourcePort builds the schema catalogue from somewhere other than the migration
// files. schema_source_postgres and schema_source_sqlite read it from a live database.
//
// A view the source cannot type precisely may carry its definition, the SELECT text, in
// sqlmodel.View.Definition: the generator analyses it to type the view's columns.
//
// The source keeps ownership of the catalogue it returns. The generator copies it before
// adding custom functions and column type overrides and never changes the original, so a
// source may cache one catalogue and return it from every call, and concurrent runs, of
// one generator or of several, may share it. The source must not change a catalogue it
// has returned while a run may still be reading it.
type SchemaSourcePort interface {
	// BuildCatalogue builds the schema catalogue.
	//
	// Returns *sqlmodel.Catalogue which holds the schema; the source keeps ownership, and
	// the generator works on a copy.
	// Returns []diag.SourceError which holds any diagnostics found while building it.
	// Returns error when the catalogue cannot be built.
	BuildCatalogue(ctx context.Context) (*sqlmodel.Catalogue, []diag.SourceError, error)
}
