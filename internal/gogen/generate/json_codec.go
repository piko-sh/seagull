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

package generate

import (
	"bytes"
	"context"
	"go/format"
	"go/token"
	"slices"

	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/gogen/helpers"
	"seagull.sh/seagull/internal/gogen/methods"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// jsonMarshalSuffix names the wrapper the helpers encode JSON through, after the helper
	// prefix.
	jsonMarshalSuffix = "JSONMarshal"

	// jsonUnmarshalSuffix names the wrapper the helpers decode JSON through, after the
	// helper prefix.
	jsonUnmarshalSuffix = "JSONUnmarshal"
)

// codecQualifiers lists the qualifiers the JSON codec file writes itself, which a decoded
// type's own import cannot take there.
var codecQualifiers = map[string]bool{"json": true, "reflect": true, "atomic": true}

// jsonCodecFile renders the JSON codec file when the package's files encode or decode
// JSON through its wrappers, and returns a file with empty content when they do not.
//
// Takes emission (*gogen.Emission) which is the package's generation.
// Takes added ([]sqlmodel.GeneratedFile) which holds the helper files added after the
// identifiers the emission records were collected.
//
// Returns sqlmodel.GeneratedFile which is the codec file, or one with empty content.
// Returns error when a query cannot be rendered, the file cannot be, or the context is
// done.
func jsonCodecFile(ctx context.Context, emission *gogen.Emission, added []sqlmodel.GeneratedFile) (sqlmodel.GeneratedFile, error) {
	if err := stopped(ctx, "the JSON codec for "+emission.Request.Package); err != nil {
		return sqlmodel.GeneratedFile{Name: "", Content: nil}, err
	}
	marshal, unmarshal := emission.Vocabulary.Helper(jsonMarshalSuffix), emission.Vocabulary.Helper(jsonUnmarshalSuffix)
	_, encodes := emission.Referenced[marshal]
	_, decodes := emission.Referenced[unmarshal]
	if !encodes && !decodes {
		addedNames := unqualifiedIdentifiers(added)
		_, encodes = addedNames[marshal]
		_, decodes = addedNames[unmarshal]
	}
	if !encodes && !decodes {
		return sqlmodel.GeneratedFile{Name: "", Content: nil}, nil
	}
	decoded, err := jsonDecodedTypes(ctx, emission)
	if err != nil {
		return sqlmodel.GeneratedFile{Name: "", Content: nil}, err
	}
	return helpers.JSONCodec(emission.Request.Package, emission.Vocabulary, decoded)
}

// jsonDecodedTypes lists the Go types of the columns the package's queries read through
// the JSON scan adapter, once each in the order the queries first read them, for
// SetJSONCodec to pretouch.
//
// A type whose import the codec file cannot write under the qualifier the package's other
// files use is left out, which costs only its pretouch.
//
// Takes emission (*gogen.Emission) which is the package's generation.
//
// Returns []helpers.Code which holds the types, each with the imports it needs.
// Returns error when a query cannot be rendered or the context is done.
func jsonDecodedTypes(ctx context.Context, emission *gogen.Emission) ([]helpers.Code, error) {
	var decoded []helpers.Code
	seen := make(map[string]bool)
	for _, analysed := range gogen.PlanOrder(emission.Request.Queries) {
		if err := stopped(ctx, "the JSON codec for "+emission.Request.Package); err != nil {
			return nil, err
		}
		query, err := emission.Query(analysed, goast.NewImportTracker())
		if err != nil {
			return nil, err
		}
		for index := range analysed.OutputColumns {
			if !scansJSON(emission, query, index) {
				continue
			}
			code, written := decodedTypeCode(emission, methods.ColumnGoType(query, index))
			if written && !seen[code.Text] {
				seen[code.Text] = true
				decoded = append(decoded, code)
			}
		}
	}
	return decoded, nil
}

// scansJSON reports whether a query reads an output column through the JSON scan adapter,
// as the generated scan does. That is a column its SQL wraps in the dialect's to-JSON
// function that the driver neither converts nor reads into a raw local.
//
// Takes emission (*gogen.Emission) which is the package's generation.
// Takes query (*gogen.Query) which is the rendered query.
// Takes index (int) which is the output column's index.
//
// Returns bool which is true when the column is decoded from JSON.
func scansJSON(emission *gogen.Emission, query *gogen.Query, index int) bool {
	column := &query.Analysed.OutputColumns[index]
	kind, wrapped := query.Rendered.Marks.Wrapped[index]
	return wrapped && kind == emitkit.ColumnWrapJSON && !emission.ReadsRaw(column) && !emission.ConvertsColumn(column, wrapped)
}

// decodedTypeCode writes a Go type as the codec file writes it, with the imports it
// needs, when each import's qualifier is the one the codec file assumes and the package's
// plan gives it.
//
// Takes emission (*gogen.Emission) which holds the package's planned imports.
// Takes goType (sqlmodel.GoType) which is the type.
//
// Returns helpers.Code which is the type's text and imports.
// Returns bool which is false when the type cannot be written in the codec file.
func decodedTypeCode(emission *gogen.Emission, goType sqlmodel.GoType) (helpers.Code, bool) {
	tracker := goast.NewPlannedImportTracker(emission.Request.Naming.Imports)
	expression := tracker.AddType(goType)
	var imports []string
	for importPath, qualifier := range tracker.Qualifiers() {
		if qualifier != helpers.AssumedPackageName(importPath) || codecQualifiers[qualifier] {
			return helpers.Code{Text: "", Imports: nil}, false
		}
		imports = append(imports, importPath)
	}
	slices.Sort(imports)
	var text bytes.Buffer
	if err := format.Node(&text, token.NewFileSet(), expression); err != nil {

		return helpers.Code{Text: "", Imports: nil}, false
	}
	return helpers.Code{Text: text.String(), Imports: imports}, true
}
