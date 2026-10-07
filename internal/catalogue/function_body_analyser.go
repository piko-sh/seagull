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

package catalogue

import (
	"context"
	"errors"
	"fmt"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/resolve"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// functionBodyParameterAlias is the synthetic table alias used to host function-body
	// parameter bindings inside the parameter scope. The double-underscore prefix avoids
	// accidental collision with user-declared catalogue tables.
	functionBodyParameterAlias = "__sgl_function_body__"
)

// analyseFunctionBody infers a function's ReturnType from the definition's Body by
// binding BodyParameters into a fresh scope and resolving the body through the existing
// TypeResolver.
//
// The analyser registers each parameter into a ScopeKindParameter root scope as an
// Unknown-typed column-like binding, so polymorphic bodies degrade gracefully when
// downstream resolvers cannot narrow them. ReturnType is only populated when the engine
// declared none, by leaving it TypeCategoryUnknown with no engine name. Any other type,
// an integer without an engine name included, keeps the engine's choice.
//
// Takes definition (*ast.FunctionDefinition) which must already have Body and
// BodyParameters populated when a body is available. A definition with a nil Body is a
// no-op success so callers can route every CREATE FUNCTION mutation through the analyser
// without first inspecting the definition.
// Takes resolver (*resolve.TypeResolver) which carries the catalogue and engine
// type-system port used for bottom-up expression inference.
//
// Returns error when the definition or its signature is nil, a body has no resolver, or
// bottom-up resolution of the body returns an error. TypeCategoryUnknown is not an error;
// callers may want a diagnostic but the function still registers.
func analyseFunctionBody(ctx context.Context, definition *ast.FunctionDefinition, resolver *resolve.TypeResolver) error {
	if definition == nil || definition.Function == nil {
		return errors.New("analyse function body: nil signature")
	}
	if definition.Body == nil {
		return nil
	}
	if resolver == nil {
		return errors.New("analyse function body: nil resolver")
	}
	scope := newFunctionBodyScope(resolver.NewRootScope(resolve.ScopeKindParameter), definition.BodyParameters)
	inferred, _, inferErr := resolver.ResolveExpressionType(ctx, definition.Body, scope, new(false))
	if inferErr != nil {
		return fmt.Errorf("analyse function body for %q: %w", definition.Name, inferErr)
	}
	if definition.ReturnType.Category == sqlmodel.TypeCategoryUnknown && definition.ReturnType.EngineName == "" {
		definition.ReturnType = inferred
	}
	return nil
}

// newFunctionBodyScope constructs a scope chain rooted in a ScopeKindParameter scope
// where each declared parameter is bound as an Unknown-typed column on a synthetic
// parameter table.
//
// The synthetic table is aliased to functionBodyParameterAlias so the existing
// column-resolution path can find each parameter when the body expression references it
// as a bare identifier. The table is flagged IsWithoutRowID so that implicit rowid
// lookups during resolveUnqualifiedColumn do not synthesise a rowid binding on the
// parameter scope.
//
// Takes scope (*resolve.ScopeChain) which is the empty parameter scope, rooted in the
// dialect's scope rules.
// Takes parameters ([]string) which is the lexical parameter list, possibly empty for a
// function with no arguments.
//
// Returns *resolve.ScopeChain which is the root scope ready to resolve the body. The
// returned scope is always non-nil so callers do not need a guard.
func newFunctionBodyScope(scope *resolve.ScopeChain, parameters []string) *resolve.ScopeChain {
	columns := make([]ast.ScopedColumn, 0, len(parameters))
	for _, name := range parameters {
		columns = append(columns, ast.ScopedColumn{
			Name:     name,
			SQLType:  sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
			Nullable: true,
		})
	}
	scope.Tables[scope.Key(functionBodyParameterAlias)] = &resolve.ScopedTable{
		Name:           functionBodyParameterAlias,
		Alias:          functionBodyParameterAlias,
		Columns:        columns,
		CatalogueTable: nil,
		Function:       false,
		IsWithoutRowID: true,
		Schema:         "",
		JoinKind:       ast.JoinInner,
	}
	return scope
}
