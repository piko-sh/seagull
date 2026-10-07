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

package pgx

import (
	"embed"
	"go/ast"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/gogen/helpers"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// driverValueHelperFile is the generated file holding the binary-parameter wrapper.
	driverValueHelperFile = "driver_values.go"

	// suffixDriverBinary is the helper-prefix suffix of the binary-parameter wrapper type.
	suffixDriverBinary = "DriverBinary"

	// identWrappedValue is the wrapper's field holding the parameter.
	identWrappedValue = "value"
)

// sources holds the sources of the helpers the emitter adds to a generated package.
//
//go:embed internal/source/driver_values.go
var sources embed.FS

// preparedSources caches immutable parsed helper templates for this emitter.
var preparedSources = helpers.NewSources(sources, "internal/source")

// pgxBinder adapts parameters that pgx would otherwise bind incorrectly. Binary
// parameters with overridden Go types use sglDriverBinary to send their marshalled bytes
// instead of their driver.Valuer text.
type pgxBinder struct {
	// types maps the request's SQL types.
	types sqlmodel.GoTypeResolver

	// vocabulary names the generated helpers of the request.
	vocabulary gogen.Vocabulary
}

var (
	_ gogen.ParameterBinder = pgxBinder{}

	_ gogen.HelperProvider = pgxBinder{}
)

// Bind returns the expression that binds a parameter: wrapped in the generated
// sglDriverBinary for a binary parameter whose Go type an override chose, and the access
// as it is otherwise.
//
// Takes access (ast.Expr) which reads the parameter's value.
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
//
// Returns ast.Expr which binds the parameter.
func (binder pgxBinder) Bind(access ast.Expr, parameter *sqlmodel.QueryParameter, _ *goast.ImportTracker) ast.Expr {
	if !bindsAsDriverBinary(parameter, binder.types) {
		return access
	}
	return &ast.CompositeLit{
		Type: goastutil.CachedIdent(binder.vocabulary.Helper(suffixDriverBinary)),
		Elts: []ast.Expr{&ast.KeyValueExpr{Key: goastutil.CachedIdent(identWrappedValue), Value: access}},
	}
}

// Helpers returns driver_values.go when a query of the request binds a parameter through
// the generated sglDriverBinary, and nothing otherwise.
//
// Takes request (*emit.Request) which is the request being emitted. Helpers does not
// change it.
//
// Returns []sqlmodel.GeneratedFile which holds the helper file, or none.
// Returns error when the helper cannot be rendered.
func (binder pgxBinder) Helpers(request *emit.Request) ([]sqlmodel.GeneratedFile, error) {
	if !anyParameterBindsAsDriverBinary(request.Queries, binder.types) {
		return nil, nil
	}
	file, err := preparedSources.File(driverValueHelperFile, driverValueHelperFile, binder.vocabulary, &helpers.Data{
		Flags:   nil,
		Values:  nil,
		Renames: nil,
		Package: request.Package,
	})
	if err != nil {
		return nil, err
	}
	return []sqlmodel.GeneratedFile{file}, nil
}

// bindsAsDriverBinary reports a binary parameter whose Go type an override chose, other
// than one of Go's own types and a slice parameter, which the generated sglDriverBinary
// binds as the bytes the value marshals itself to.
//
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
// Takes types (sqlmodel.GoTypeResolver) which maps the request's SQL types.
//
// Returns bool which is true when the parameter is wrapped.
func bindsAsDriverBinary(parameter *sqlmodel.QueryParameter, types sqlmodel.GoTypeResolver) bool {
	if parameter.IsSlice || parameter.SQLType.Category != sqlmodel.TypeCategoryBinary {
		return false
	}
	return gogen.OverrideChosen(types, parameter.GoTypeOverride, parameter.SQLType) &&
		!gogen.PredeclaredGoType(gogen.ChosenGoType(types, parameter.GoTypeOverride, parameter.SQLType, parameter.Nullable))
}

// anyParameterBindsAsDriverBinary reports whether any query binds a parameter through the
// generated sglDriverBinary, so the package needs it.
//
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the queries.
// Takes types (sqlmodel.GoTypeResolver) which maps the request's SQL types.
//
// Returns bool which is true when some parameter is wrapped.
func anyParameterBindsAsDriverBinary(queries []*sqlmodel.AnalysedQuery, types sqlmodel.GoTypeResolver) bool {
	for _, query := range queries {
		for index := range query.Parameters {
			if bindsAsDriverBinary(&query.Parameters[index], types) {
				return true
			}
		}
	}
	return false
}
