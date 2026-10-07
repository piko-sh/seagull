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
	"fmt"
	"maps"
	"slices"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/resolve"
	"seagull.sh/seagull/internal/sqlmodel"
)

// applyCreateEnum adds a new enum type to the catalogue, unless the statement said IF NOT
// EXISTS and the schema already holds a type of that name.
//
// Takes mutation (*ast.CatalogueMutation) which holds the enum name and values.
//
// Returns error (always nil).
func (b *catalogueBuilder) applyCreateEnum(mutation *ast.CatalogueMutation) error {
	schema := b.resolveSchema(mutation.SchemaName)
	if b.keepsExistingType(schema, mutation) {
		return nil
	}
	schema.Enums[b.key(mutation.EnumName)] = &sqlmodel.Enum{
		Name:    mutation.EnumName,
		Schema:  schema.Name,
		Values:  mutation.EnumValues,
		Origin:  mutation.Origin,
		Comment: "",
	}
	return nil
}

// applyAlterEnumAddValue adds new values to an existing enum type, last or where the
// mutation's placement puts them.
//
// A value the enum already holds is skipped when the statement said IF NOT EXISTS, and is
// an error otherwise, as the database reports it. Every type that names the enum takes
// its new values.
//
// Takes mutation (*ast.CatalogueMutation) which holds the values to add.
//
// Returns error when the enum is not found, a value already exists or the placement's
// anchor is not one of the enum's values.
func (b *catalogueBuilder) applyAlterEnumAddValue(mutation *ast.CatalogueMutation) error {
	enum, err := b.findEnum(mutation)
	if err != nil {
		return err
	}
	placement := mutation.Placement
	for _, value := range mutation.EnumValues {
		if slices.Contains(enum.Values, value) {
			if mutation.IfNotExists {
				continue
			}
			return duplicateObject("enum %s already has the value %q", enum.Name, value)
		}
		if err := insertEnumValue(enum, value, placement); err != nil {
			return err
		}
		if placement.Kind != ast.PlacementDefault {
			placement = ast.Placement{Anchor: value, Kind: ast.PlacementAfter}
		}
	}
	b.refreshEnumValues(enum)
	return nil
}

// findEnum finds the enum a mutation names, in the named schema or, for an unqualified
// name, along the search path.
//
// Takes mutation (*ast.CatalogueMutation) which names the enum.
//
// Returns *sqlmodel.Enum which is the enum.
// Returns error when there is no such enum.
func (b *catalogueBuilder) findEnum(mutation *ast.CatalogueMutation) (*sqlmodel.Enum, error) {
	schema, _ := b.lookupType(mutation.SchemaName, mutation.EnumName)
	if schema != nil {
		if enum := schema.Enums[b.key(mutation.EnumName)]; enum != nil {
			return enum, nil
		}
	}
	return nil, unknownObject("enum %s not found in schema %s", mutation.EnumName, b.searchedSchemas(mutation.SchemaName))
}

// keepsExistingType reports whether a CREATE TYPE IF NOT EXISTS names a type the schema
// already holds, so it changes nothing.
//
// Takes schema (*sqlmodel.Schema) which is the schema the type is created in.
// Takes mutation (*ast.CatalogueMutation) which names the type.
//
// Returns bool which is true when the statement leaves the existing type in place.
func (b *catalogueBuilder) keepsExistingType(schema *sqlmodel.Schema, mutation *ast.CatalogueMutation) bool {
	if !mutation.IfNotExists {
		return false
	}
	key := b.key(mutation.EnumName)
	return schema.Enums[key] != nil || schema.CompositeTypes[key] != nil || schema.Domains[key] != nil
}

// applyAlterEnumRenameValue renames a value within an existing enum type, and every type
// that names the enum takes its new values. The enum's values are replaced by a renamed
// copy, never changed in place, so a type that still shares the old list is unchanged.
//
// Takes mutation (*ast.CatalogueMutation) which holds the old and new enum value names.
//
// Returns error when the enum is not found, it has no value of the old name, or it
// already has a value of the new name.
func (b *catalogueBuilder) applyAlterEnumRenameValue(mutation *ast.CatalogueMutation) error {
	enum, err := b.findEnum(mutation)
	if err != nil {
		return err
	}

	const renameValuePairSize = 2
	if len(mutation.EnumValues) < renameValuePairSize {
		return nil
	}
	oldValue, newValue := mutation.EnumValues[0], mutation.EnumValues[1]
	index := slices.Index(enum.Values, oldValue)
	if index < 0 {
		return unknownObject("enum %s has no value %q", enum.Name, oldValue)
	}
	if slices.Contains(enum.Values, newValue) {
		return duplicateObject("enum %s already has the value %q", enum.Name, newValue)
	}
	renamed := slices.Clone(enum.Values)
	renamed[index] = newValue
	enum.Values = renamed
	b.refreshEnumValues(enum)
	return nil
}

// applyDropEnum removes an enum type from the catalogue.
//
// Takes mutation (*ast.CatalogueMutation) which identifies the enum to drop.
//
// Returns error when no enum of the name exists and the statement did not say IF EXISTS.
func (b *catalogueBuilder) applyDropEnum(mutation *ast.CatalogueMutation) error {
	key := b.key(mutation.EnumName)
	schema := b.schemaHolding(mutation.SchemaName, func(candidate *sqlmodel.Schema) bool { return candidate.Enums[key] != nil })
	if schema == nil {
		return missingDropTarget(mutation, "enum %s not found in schema %s", mutation.EnumName, b.searchedSchemas(mutation.SchemaName))
	}
	delete(schema.Enums, key)
	return nil
}

// applyCreateCompositeType adds a new composite type to the catalogue, unless the
// statement said IF NOT EXISTS and the schema already holds a type of that name. Each
// field's type is resolved as a column's is, so a field naming an enum, a composite type
// or a domain takes that type.
//
// Takes mutation (*ast.CatalogueMutation) which holds the composite type definition.
//
// Returns error (always nil).
func (b *catalogueBuilder) applyCreateCompositeType(mutation *ast.CatalogueMutation) error {
	schema := b.resolveSchema(mutation.SchemaName)
	if b.keepsExistingType(schema, mutation) {
		return nil
	}
	fields := mutation.Columns
	for index := range fields {
		fields[index].Origin = mutation.Origin
		b.resolveCustomColumnType(&fields[index])
	}
	schema.CompositeTypes[b.key(mutation.EnumName)] = &sqlmodel.CompositeType{
		Name:   mutation.EnumName,
		Schema: schema.Name,
		Fields: fields,
		Origin: mutation.Origin,
	}
	return nil
}

// applyCreateDomain adds a domain to the catalogue. Its base type is resolved as a
// column's type is, so a domain over another domain, an enum or a composite type takes
// that type, and a domain over a NOT NULL domain is NOT NULL too.
//
// Takes mutation (*ast.CatalogueMutation) which holds the domain's name and base type.
//
// Returns error when the mutation carries no base type.
func (b *catalogueBuilder) applyCreateDomain(mutation *ast.CatalogueMutation) error {
	if len(mutation.Columns) == 0 {
		return refused(errMalformedMutation, "CREATE DOMAIN %s has no base type", mutation.EnumName)
	}
	schema := b.resolveSchema(mutation.SchemaName)
	if b.keepsExistingType(schema, mutation) {
		return nil
	}
	base := mutation.Columns[0]
	b.resolveCustomColumnType(&base)
	schema.Domains[b.key(mutation.EnumName)] = &sqlmodel.Domain{
		Name:            mutation.EnumName,
		Schema:          schema.Name,
		BaseType:        base.SQLType,
		Origin:          mutation.Origin,
		ArrayDimensions: base.ArrayDimensions,
		NotNull:         !base.Nullable,
	}
	return nil
}

// applyDropType removes a composite type, enum or domain from the catalogue.
//
// Takes mutation (*ast.CatalogueMutation) which identifies the type to drop.
//
// Returns error when no type of the name exists and the statement did not say IF EXISTS.
func (b *catalogueBuilder) applyDropType(mutation *ast.CatalogueMutation) error {
	key := b.key(mutation.EnumName)
	schema := b.schemaHolding(mutation.SchemaName, func(candidate *sqlmodel.Schema) bool { return typeTaken(candidate, key) })
	if schema == nil {
		return missingDropTarget(mutation, "type %s not found in schema %s", mutation.EnumName, b.searchedSchemas(mutation.SchemaName))
	}
	delete(schema.CompositeTypes, key)
	delete(schema.Enums, key)
	delete(schema.Domains, key)
	return nil
}

// applyCreateSchema adds a new empty schema to the catalogue if it does not already
// exist.
//
// Takes mutation (*ast.CatalogueMutation) which holds the schema name.
//
// Returns error (always nil).
func (b *catalogueBuilder) applyCreateSchema(mutation *ast.CatalogueMutation) error {
	if _, exists := b.catalogue.Schemas[b.key(mutation.SchemaName)]; !exists {
		b.catalogue.Schemas[b.key(mutation.SchemaName)] = newEmptySchema(mutation.SchemaName)
	}
	return nil
}

// applyDropSchema removes a schema and all its contents from the catalogue.
//
// Takes mutation (*ast.CatalogueMutation) which identifies the schema to drop.
//
// Returns error (always nil).
func (b *catalogueBuilder) applyDropSchema(mutation *ast.CatalogueMutation) error {
	delete(b.catalogue.Schemas, b.key(mutation.SchemaName))
	return nil
}

// applyCreateView adds a view, resolving its columns from the definition and applying
// explicit column names in order. IF NOT EXISTS leaves an existing relation unchanged.
//
// Projected table columns retain their overrides. Unknown function calls produce Q005 at
// the CREATE VIEW statement. For engines that resolve views on each query, the stored
// definition lets columns follow later table changes.
//
// Takes mutation (*ast.CatalogueMutation) which holds the view definition.
//
// Returns error (always nil).
func (b *catalogueBuilder) applyCreateView(ctx context.Context, mutation *ast.CatalogueMutation) error {
	schema := b.resolveSchema(mutation.SchemaName)
	key := b.key(mutation.TableName)
	if mutation.IfNotExists && (schema.Views[key] != nil || schema.Tables[key] != nil) {
		return nil
	}

	var columns []sqlmodel.Column
	if mutation.ViewDefinition != nil {
		b.applyPendingColumnOverrides()
		var problems []*resolve.Error
		columns, problems = b.resolveViewColumns(ctx, mutation.ViewDefinition)
		b.reportViewProblems("view "+mutation.TableName, problems)
		nameDeclaredColumns(columns, mutation.Columns)
	}

	columns = padDeclaredColumns(columns, mutation.Columns)

	for index := range columns {
		columns[index].Origin = mutation.Origin
	}
	view := &sqlmodel.View{
		Name:       mutation.TableName,
		Schema:     schema.Name,
		Columns:    columns,
		Origin:     mutation.Origin,
		Definition: "",
		Comment:    "",
	}
	schema.Views[b.key(mutation.TableName)] = view
	b.followView(view, mutation)
	return nil
}

// resolveViewColumns analyses a view definition query to determine the output column
// names, types, and nullability. A column that projects a table or view column unchanged
// takes that column's type, Go type and nullability overrides.
//
// A definition holding a nil node where an expression or query belongs is not resolved:
// it gives no columns and the Q030 problem that says so.
//
// Takes definition (*ast.RawQueryAnalysis) which holds the parsed view query.
//
// Returns []sqlmodel.Column which holds the resolved view columns.
// Returns []*resolve.Error which holds the problems found resolving them.
func (b *catalogueBuilder) resolveViewColumns(
	ctx context.Context,
	definition *ast.RawQueryAnalysis,
) ([]sqlmodel.Column, []*resolve.Error) {
	if problem := resolve.ValidateAnalysis(definition); problem != nil {
		return nil, []*resolve.Error{problem}
	}
	builder := resolve.NewCatalogueScopeBuilder(b.engine, b.catalogue)
	scope := builder.TypeResolver.NewRootScope(resolve.ScopeKindQuery)

	problems := builder.BuildQueryScope(ctx, definition, scope)

	outputColumns, _, outputProblems := builder.TypeResolver.ResolveOutputColumns(
		ctx, definition.OutputColumns, scope,
	)
	problems = append(problems, outputProblems...)

	if len(definition.CompoundBranches) > 0 {
		problems = append(problems, builder.ResolveCompoundBranches(ctx, definition.CompoundBranches, outputColumns, scope)...)
	}

	columns := make([]sqlmodel.Column, len(outputColumns))
	for index := range outputColumns {
		columns[index] = sqlmodel.NewColumn(outputColumns[index].Name, outputColumns[index].SQLType, outputColumns[index].Nullable)
		b.inheritColumnOverrides(&columns[index], &outputColumns[index])
	}
	return columns, problems
}

// inheritColumnOverrides copies source-column overrides onto an unchanged view
// projection.
//
// A cast to another SQL type inherits only the nullability override and the struct tags.
//
// Takes column (*sqlmodel.Column) which is the view column.
// Takes output (*sqlmodel.OutputColumn) which says where the column came from.
func (b *catalogueBuilder) inheritColumnOverrides(column *sqlmodel.Column, output *sqlmodel.OutputColumn) {
	if output.SourceTable == "" || output.SourceColumn == "" {
		return
	}
	source := b.catalogue.FindColumn(output.SourceSchema, output.SourceTable, output.SourceColumn)
	if source == nil {
		return
	}
	if source.NullableOverride != nil {
		column.NullableOverride = new(*source.NullableOverride)
	}
	column.StructTags = maps.Clone(source.StructTags)
	if !resolve.RestatesColumnType(output.SQLType, source.SQLType) {
		return
	}
	column.SQLTypeOverride = source.SQLTypeOverride
	column.GoTypeOverride = cloneGoType(source.GoTypeOverride)
	if source.NonNullElements != nil {
		column.NonNullElements = new(*source.NonNullElements)
	}
}

// reportViewProblems reports, at the CREATE VIEW or CREATE TABLE ...
//
// AS SELECT statement, every call in the query to a function the catalogue does not hold,
// and an analysis the dialect left with a nil node, which leaves the relation without the
// columns of its query. Other problems are left to the queries that read the relation.
//
// Takes viewName (string) which describes the relation, such as "view totals" or "table
// totals".
// Takes problems ([]*resolve.Error) which are the problems found resolving its columns.
func (b *catalogueBuilder) reportViewProblems(viewName string, problems []*resolve.Error) {
	for _, problem := range problems {
		if problem == nil || (problem.Code != diag.CodeUnknownFunction && problem.Code != diag.CodeInternalNilGuard) {
			continue
		}
		b.statementDiagnostics = append(b.statementDiagnostics, diagbuild.Located(
			b.statement.filename,
			diagbuild.TextSpan{Line: max(b.statement.line, 1), Column: 1, EndLine: 0, EndColumn: 0},
			problem.Code,
			problem.Severity,
			fmt.Sprintf("%s: %s", viewName, problem.Message),
			problem.Suggestion,
		))
	}
}

// applyDropView removes a view from the catalogue.
//
// Takes mutation (*ast.CatalogueMutation) which identifies the view to drop.
//
// Returns error when no view of the name exists and the statement did not say IF EXISTS.
func (b *catalogueBuilder) applyDropView(mutation *ast.CatalogueMutation) error {
	key := b.key(mutation.TableName)
	schema := b.schemaHolding(mutation.SchemaName, func(candidate *sqlmodel.Schema) bool { return candidate.Views[key] != nil })
	if schema == nil {
		return missingDropTarget(mutation, "view %s not found in schema %s", mutation.TableName, b.searchedSchemas(mutation.SchemaName))
	}
	delete(schema.Views, key)
	return nil
}

// padDeclaredColumns extends a view's resolved columns with the name-only columns the
// CREATE VIEW declares past them, and stands in for them all when none resolved.
//
// Takes columns ([]sqlmodel.Column) which are the resolved columns.
// Takes declared ([]sqlmodel.Column) which are the declared name-only columns.
//
// Returns []sqlmodel.Column which are the view's columns.
func padDeclaredColumns(columns, declared []sqlmodel.Column) []sqlmodel.Column {
	if columns == nil {
		return declared
	}
	if len(columns) >= len(declared) {
		return columns
	}
	merged := make([]sqlmodel.Column, len(declared))
	for index := range declared {
		if index < len(columns) {
			merged[index] = columns[index]
			if merged[index].Name == "" {
				merged[index].Name = declared[index].Name
			}
			continue
		}
		merged[index] = declared[index]
	}
	return merged
}

// nameDeclaredColumns gives a view's resolved columns, in order, the names the CREATE
// VIEW declares for them; a column past the declared list keeps its own name.
//
// Takes columns ([]sqlmodel.Column) which are the resolved columns, renamed in place.
// Takes declared ([]sqlmodel.Column) which carry the declared names.
func nameDeclaredColumns(columns, declared []sqlmodel.Column) {
	for index := range min(len(columns), len(declared)) {
		if declared[index].Name != "" {
			columns[index].Name = declared[index].Name
		}
	}
}
