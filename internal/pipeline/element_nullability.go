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

package pipeline

import (
	"context"

	cataloguebuilder "seagull.sh/seagull/internal/catalogue"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/gotypes"
	"seagull.sh/seagull/internal/resolve"
	"seagull.sh/seagull/internal/sqlmodel"
)

// elementNullability returns which elements of arrays and lists, and which values of
// maps, the generation holds as nullable: those the dialect's engine can hold NULL in,
// unless the preset or a type override of their type declares them non-null.
//
// A column's own choice is read from the column when the policy is applied.
//
// Takes catalogue (*sqlmodel.Catalogue) which resolves a type override's enum and
// composite type names.
//
// Returns *resolve.ElementNullability which decides them.
func (p *Pipeline) elementNullability(catalogue *sqlmodel.Catalogue) *resolve.ElementNullability {
	var holds func(sqlmodel.SQLType) bool
	if holder, ok := p.dialect.(dialect.ElementNullabilityPort); ok {
		holds = holder.HoldsNullElements
	}
	mapper := gotypes.NewTypeMapper(p.dialect.BuiltinTypes()).WithSchema(catalogue)
	var choices []resolve.ElementTypeChoice
	for _, override := range p.typeOverrides {
		if override.SQLTypeName == "" || override.NonNullElements == nil {
			continue
		}
		choices = append(choices, resolve.ElementTypeChoice{
			SQLType: mapper.ResolveOverrideType(override),
			NonNull: *override.NonNullElements,
		})
	}
	return resolve.NewElementNullability(holds, p.settings.NonNullElements, choices)
}

// parameterElementNullability adapts element nullability for parameter binding. If the
// driver cannot bind NULL elements, only explicit parameter or column declarations remain
// nullable, allowing the emitter's CheckerPort to reject them.
//
// Takes elements (*resolve.ElementNullability) which decides the elements of the
// generation's values.
//
// Returns *resolve.ElementNullability which decides the parameters' elements.
// Returns error which wraps fault.ErrPanic when the emitter's BindsNullElements panicked.
func (p *Pipeline) parameterElementNullability(ctx context.Context, elements *resolve.ElementNullability) (*resolve.ElementNullability, error) {
	binder, binds := p.emitter.(emit.NullElementBinderPort)
	if !binds {
		return elements, nil
	}
	info := p.dialect.Info()
	bound, err := bindsNullElements(ctx, binder, new(info.Facts()))
	if err != nil {
		return nil, err
	}
	if bound {
		return elements, nil
	}
	return elements.DeclaredOnly(), nil
}

// modelCatalogue returns a copy of the catalogue whose table and view columns hold the
// element nullability the generated models are written with, so a model's array field is
// a slice of pointers where a query's result would be.
//
// The catalogue given is not changed, so the analysis, and every parameter typed from a
// column, keeps the declared types. A nil schema, table or view entry is passed over.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the schema.
// Takes elements (*resolve.ElementNullability) which decides the elements' nullability.
//
// Returns *sqlmodel.Catalogue which is the marked copy.
func modelCatalogue(catalogue *sqlmodel.Catalogue, elements *resolve.ElementNullability) *sqlmodel.Catalogue {
	copied := cataloguebuilder.Clone(catalogue)
	if copied == nil {
		return nil
	}
	for _, schema := range copied.Schemas {
		if schema == nil {
			continue
		}
		for _, table := range schema.Tables {
			if table != nil {
				markColumnElements(table.Columns, elements)
			}
		}
		for _, view := range schema.Views {
			if view != nil {
				markColumnElements(view.Columns, elements)
			}
		}
	}
	return copied
}

// markColumnElements marks the elements of each column's values as the element
// nullability decides, reading the column's own choice from it. A column whose array
// dimensions the catalogue keeps apart from its type gets its marked element type back.
//
// Takes columns ([]sqlmodel.Column) which are updated in place.
// Takes elements (*resolve.ElementNullability) which decides the elements' nullability.
func markColumnElements(columns []sqlmodel.Column, elements *resolve.ElementNullability) {
	for index := range columns {
		column := &columns[index]
		marked := elements.Apply(column.ValueType(), column.NonNullElements)
		for range min(max(column.ArrayDimensions, 0), sqlmodel.MaxArrayDimensions) {
			if marked.ElementType == nil {
				break
			}
			marked = *marked.ElementType
		}
		column.SQLType = marked
	}
}
