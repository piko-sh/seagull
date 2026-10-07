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

package models

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"maps"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/observe"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// modelsFile is the generated file holding the models and named types.
	modelsFile = "models.go"

	// enumReceiver is the receiver of a named enum type's methods.
	enumReceiver = "value"

	// methodValid names a named enum type's method reporting a declared value.
	methodValid = "Valid"
)

// tableKey finds a catalogue table by its schema and name.
type tableKey struct {
	// schema is the table's schema.
	schema string

	// name is the table's name.
	name string
}

// GenerateWithReferences generates models.go with the table models and named enum and
// struct types the naming plan declares, so rows and parameters using those types share
// their declarations. It records the models' identifiers before formatting and observes
// the formatting stage; a nil reference map records none.
//
// Takes packageName (string) which names the generated Go package.
// Takes catalogue (*sqlmodel.Catalogue) which holds the schema models to emit.
// Takes mappings (sqlmodel.GoTypeResolver) which resolves SQL column types to Go types.
// Takes naming (*emit.Naming) which holds the planned Go names.
// Takes style (*emit.Style) which controls the emitted model form.
// Takes referenced (map[string]struct{}) which contains identifiers already used by
// generated query declarations.
//
// Returns []sqlmodel.GeneratedFile which holds the models.go file, or nothing when the
// package declares no model and no named type.
// Returns error when the context is done, or model generation or formatting fails.
func GenerateWithReferences(ctx context.Context, packageName string, catalogue *sqlmodel.Catalogue, mappings sqlmodel.GoTypeResolver,
	naming *emit.Naming, style *emit.Style, referenced map[string]struct{}) ([]sqlmodel.GeneratedFile, error) {
	if cause := context.Cause(ctx); cause != nil {
		return nil, fmt.Errorf("generating models: %w", cause)
	}
	tables := tablesByName(catalogue)
	tracker := goast.NewPlannedImportTracker(naming.Imports)
	declarations := make([]ast.Decl, 0, len(naming.Models)+len(naming.Types))
	for index := range naming.Models {
		model := &naming.Models[index]
		table, found := tables[tableKey{schema: model.Schema, name: model.Relation}]
		if !found {
			return nil, fmt.Errorf("model %s names table %s.%s, which the catalogue does not hold", model.Type, model.Schema, model.Relation)
		}
		declarations = append(declarations, buildModelStruct(table, model, mappings, tracker, style))
	}
	for index := range naming.Types {
		declarations = append(declarations, buildNamedType(&naming.Types[index], mappings, tracker, style)...)
	}
	if len(declarations) == 0 {
		return nil, nil
	}

	if referenced != nil {
		maps.Copy(referenced, goast.UnqualifiedIdentifiers(declarations))
	}
	_, end := observe.From(ctx).Start(ctx, observe.StageFormat)
	content, err := goast.FormatFileWithAST(packageName, tracker, declarations)
	end(err)
	if err != nil {
		return nil, fmt.Errorf("formatting models file: %w", err)
	}
	content, err = goast.DocumentFields(content, fieldDocumentation(tables, naming, style))
	if err != nil {
		return nil, fmt.Errorf("documenting models file: %w", err)
	}
	return []sqlmodel.GeneratedFile{{Name: modelsFile, Content: content}}, nil
}

// fieldDocumentation returns the doc comment of every model field whose column has a
// comment, when the style asks for doc comments.
//
// Takes tables (map[tableKey]*sqlmodel.Table) which holds the catalogue's tables.
// Takes naming (*emit.Naming) which names the models and their fields.
// Takes style (*emit.Style) which says whether the models carry comments.
//
// Returns map[string]map[string][]string which maps each model to its fields' comment
// lines, or nil when there are none.
func fieldDocumentation(tables map[tableKey]*sqlmodel.Table, naming *emit.Naming, style *emit.Style) map[string]map[string][]string {
	if !style.DocComments {
		return nil
	}
	var docs map[string]map[string][]string
	for index := range naming.Models {
		model := &naming.Models[index]
		table := tables[tableKey{schema: model.Schema, name: model.Relation}]
		for columnIndex := range table.Columns {
			lines := emitkit.CommentLines(table.Columns[columnIndex].Comment)
			if model.Fields[columnIndex] == "" || len(lines) == 0 {
				continue
			}
			if docs == nil {
				docs = make(map[string]map[string][]string)
			}
			if docs[model.Type] == nil {
				docs[model.Type] = make(map[string][]string)
			}
			docs[model.Type][model.Fields[columnIndex]] = lines
		}
	}
	return docs
}

// tablesByName maps every catalogue table by its schema and name.
//
// Takes catalogue (*sqlmodel.Catalogue) which is the schema state, or nil for none.
//
// Returns map[tableKey]*sqlmodel.Table which holds the tables.
func tablesByName(catalogue *sqlmodel.Catalogue) map[tableKey]*sqlmodel.Table {
	tables := make(map[tableKey]*sqlmodel.Table)
	if catalogue == nil {
		return tables
	}
	for _, schema := range catalogue.Schemas {
		for _, table := range schema.Tables {
			tables[tableKey{schema: table.Schema, name: table.Name}] = table
		}
	}
	return tables
}

// buildModelStruct builds a table model declaration from the naming plan, applying column
// overrides and omitting hidden columns. The table comment documents the struct when the
// style enables doc comments.
//
// Takes table (*sqlmodel.Table) which defines the table schema.
// Takes model (*emit.ModelNaming) which names the struct and its fields.
// Takes mappings (sqlmodel.GoTypeResolver) which defines SQL-to-Go type mappings.
// Takes tracker (*goast.ImportTracker) which collects required imports.
// Takes style (*emit.Style) which shapes the fields' struct tags and says whether the
// struct carries its table's comment.
//
// Returns ast.Decl which is the type declaration for the model struct.
func buildModelStruct(table *sqlmodel.Table, model *emit.ModelNaming, mappings sqlmodel.GoTypeResolver, tracker *goast.ImportTracker, style *emit.Style) ast.Decl {
	var sourceNames []string
	var columnTags []map[string]string
	for index := range table.Columns {
		if model.Fields[index] != "" {
			sourceNames = append(sourceNames, table.Columns[index].Name)
			columnTags = append(columnTags, table.Columns[index].StructTags)
		}
	}
	tags := emitkit.FieldTags(style, sourceNames, columnTags)
	fields := make([]*ast.Field, 0, len(sourceNames))
	for index := range table.Columns {
		if model.Fields[index] == "" {
			continue
		}
		column := &table.Columns[index]
		nullable := column.Nullable
		if column.NullableOverride != nil {
			nullable = *column.NullableOverride
		}
		goType := goast.ResolveGoType(column.ValueType(), nullable, mappings)
		if override := goast.NullableOverride(column.GoTypeOverride, column.ValueType(), nullable); override != nil {
			goType = *override
		}
		fields = append(fields, &ast.Field{
			Names: []*ast.Ident{goastutil.CachedIdent(model.Fields[index])},
			Type:  tracker.AddType(goType),
			Tag:   goast.TagLiteral(tags[len(fields)]),
		})
	}
	declaration := goastutil.GenDeclType(model.Type, goastutil.StructType(fields...))
	declaration.Doc = documentation(style, table.Comment)
	return declaration
}

// documentation returns the doc comment a style gives a comment the catalogue records,
// such as COMMENT ON TABLE gives one.
//
// Takes style (*emit.Style) which says whether the models carry comments.
// Takes comment (string) which is the comment, or empty.
//
// Returns *ast.CommentGroup which is the doc comment, or nil when the style asks for none
// or there is no comment.
func documentation(style *emit.Style, comment string) *ast.CommentGroup {
	if !style.DocComments {
		return nil
	}
	return goast.DocComment(emitkit.CommentLines(comment))
}

// buildNamedType builds a named enum or struct and its supporting declarations.
//
// Enums receive value constants and a Valid method. Struct fields use their mapped Go
// types and SQL nullability.
//
// Takes named (*emit.NamedType) which is the named type.
// Takes mappings (sqlmodel.GoTypeResolver) which maps the struct's field types.
// Takes tracker (*goast.ImportTracker) which collects required imports.
// Takes style (*emit.Style) which shapes a struct's struct tags.
//
// Returns []ast.Decl which holds the declarations.
func buildNamedType(named *emit.NamedType, mappings sqlmodel.GoTypeResolver, tracker *goast.ImportTracker, style *emit.Style) []ast.Decl {
	if named.SQLType.Category == sqlmodel.TypeCategoryEnum {
		return buildNamedEnum(named)
	}
	structFields := named.SQLType.StructFields
	sourceNames := make([]string, len(structFields))
	for index := range structFields {
		sourceNames[index] = structFields[index].Name
	}
	tags := emitkit.FieldTags(style, sourceNames, nil)
	fields := make([]*ast.Field, 0, len(structFields))
	for index := range structFields {
		fieldType := structFields[index].SQLType
		fields = append(fields, &ast.Field{
			Names: []*ast.Ident{goastutil.CachedIdent(named.Fields[index])},
			Type:  tracker.AddType(goast.ResolveGoType(fieldType, fieldType.Nullable, mappings)),
			Tag:   goast.TagLiteral(tags[index]),
		})
	}
	return []ast.Decl{goastutil.GenDeclType(named.Name, goastutil.StructType(fields...))}
}

// buildNamedEnum constructs a named enum type: the string type, its constants, and its
// Valid method.
//
// Takes named (*emit.NamedType) which is the enum's named type.
//
// Returns []ast.Decl which holds the declarations.
func buildNamedEnum(named *emit.NamedType) []ast.Decl {
	typeIdentifier := goastutil.CachedIdent(named.Name)
	declarations := []ast.Decl{goastutil.GenDeclType(named.Name, goastutil.CachedIdent(goast.IdentString))}
	values := named.SQLType.EnumValues
	if len(values) > 0 {
		specs := make([]ast.Spec, len(values))
		cases := make([]ast.Expr, len(values))
		for index, value := range values {
			specs[index] = &ast.ValueSpec{
				Names:  []*ast.Ident{goastutil.CachedIdent(named.Constants[index])},
				Type:   typeIdentifier,
				Values: []ast.Expr{goastutil.StrLit(value)},
			}
			cases[index] = goastutil.CachedIdent(named.Constants[index])
		}
		declarations = append(declarations, &ast.GenDecl{Tok: token.CONST, Lparen: 1, Specs: specs})
		return append(declarations, validMethod(named.Name, cases))
	}
	return append(declarations, validMethod(named.Name, nil))
}

// validMethod constructs an enum type's Valid method: true for a declared value, and
// false for any other text.
//
// Takes typeName (string) which is the enum's type.
// Takes cases ([]ast.Expr) which are its constants.
//
// Returns ast.Decl which is the method declaration.
func validMethod(typeName string, cases []ast.Expr) ast.Decl {
	body := []ast.Stmt{goastutil.ReturnStmt(goastutil.CachedIdent("false"))}
	if len(cases) > 0 {
		body = []ast.Stmt{
			&ast.SwitchStmt{
				Tag: goastutil.CachedIdent(enumReceiver),
				Body: goastutil.BlockStmt(&ast.CaseClause{
					List: cases,
					Body: []ast.Stmt{goastutil.ReturnStmt(goastutil.CachedIdent("true"))},
				}),
			},
			goastutil.ReturnStmt(goastutil.CachedIdent("false")),
		}
	}
	return &ast.FuncDecl{
		Recv: goastutil.FieldList(goastutil.Field(enumReceiver, goastutil.CachedIdent(typeName))),
		Name: goastutil.CachedIdent(methodValid),
		Type: &ast.FuncType{Results: goastutil.FieldList(goastutil.Field("", goastutil.CachedIdent(goast.IdentBool)))},
		Body: goastutil.BlockStmt(body...),
	}
}
