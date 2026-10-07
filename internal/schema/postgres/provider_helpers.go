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

package postgres

import (
	"context"

	"seagull.sh/seagull/internal/schema/internal/rowscan"
	"seagull.sh/seagull/internal/sqlmodel"
)

// schemaObject names an enum or composite type by its schema and name.
type schemaObject struct {
	// schema is the schema holding the object.
	schema string

	// name is the object's name.
	name string
}

// compositeField is one attribute of a PostgreSQL composite type.
type compositeField struct {
	// attributeName is the field identifier inside the composite.
	attributeName string

	// typeDescription describes the field's declared type.
	typeDescription typeDescription
}

// introspectEnums fills the covered schemas' Enums maps.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the covered schemas.
//
// Returns err (error) when the query, a scan, or closing the rows fails.
func (provider *Source) introspectEnums(
	ctx context.Context,
	catalogue *sqlmodel.Catalogue,
) (err error) {
	rows, queryError := provider.database.QueryContext(ctx, enumsQuery)
	if queryError != nil {
		return queryError
	}
	defer rowscan.Close(rows, &err)

	enumMap := make(map[schemaObject][]string)
	var enumOrder []schemaObject

	for rows.Next() {
		var enum schemaObject
		var enumLabel string

		if scanError := rows.Scan(&enum.schema, &enum.name, &enumLabel); scanError != nil {
			return scanError
		}

		if _, exists := enumMap[enum]; !exists {
			enumOrder = append(enumOrder, enum)
		}
		enumMap[enum] = append(enumMap[enum], enumLabel)
	}

	if rowError := rows.Err(); rowError != nil {
		return rowError
	}

	for _, enum := range enumOrder {
		schema := catalogue.Schemas[enum.schema]
		if schema == nil {
			continue
		}
		schema.Enums[enum.name] = &sqlmodel.Enum{
			Name:    enum.name,
			Schema:  schema.Name,
			Values:  enumMap[enum],
			Comment: "",
			Origin:  sqlmodel.MigrationOrigin{},
		}
	}

	return nil
}

// introspectCompositeTypes fills the covered schemas' CompositeTypes maps.
//
// Takes resolver (*typeResolver) which resolves field types.
// Takes catalogue (*sqlmodel.Catalogue) which holds the covered schemas.
//
// Returns err (error) when the query, a scan, or closing the rows fails.
func (provider *Source) introspectCompositeTypes(
	ctx context.Context,
	resolver *typeResolver,
	catalogue *sqlmodel.Catalogue,
) (err error) {
	rows, queryError := provider.database.QueryContext(ctx, compositeTypesQuery)
	if queryError != nil {
		return queryError
	}
	defer rowscan.Close(rows, &err)

	compositeMap := make(map[schemaObject][]compositeField)
	var compositeOrder []schemaObject

	for rows.Next() {
		var composite schemaObject
		var field compositeField

		destinations := append([]any{&composite.schema, &composite.name, &field.attributeName}, field.typeDescription.scanDestinations()...)
		if scanError := rows.Scan(destinations...); scanError != nil {
			return scanError
		}

		if _, exists := compositeMap[composite]; !exists {
			compositeOrder = append(compositeOrder, composite)
		}
		compositeMap[composite] = append(compositeMap[composite], field)
	}

	if rowError := rows.Err(); rowError != nil {
		return rowError
	}

	assembleCompositeTypes(catalogue, resolver, compositeMap, compositeOrder)

	return nil
}

// markHiddenColumns marks every table column the server hides from SELECT *, as
// CockroachDB hides the rowid it gives a table without a primary key, so the catalogue
// agrees with what migration replay records. A server that hides no column, such as
// PostgreSQL, is left alone.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the introspected tables.
//
// Returns err (error) when a query, a scan, or closing the rows fails.
func (provider *Source) markHiddenColumns(
	ctx context.Context,
	catalogue *sqlmodel.Catalogue,
) (err error) {
	var supported int64
	if supportError := provider.database.QueryRowContext(ctx, hiddenColumnsSupportQuery).Scan(&supported); supportError != nil {
		return supportError
	}
	if supported == 0 {
		return nil
	}
	rows, queryError := provider.database.QueryContext(ctx, hiddenColumnsQuery)
	if queryError != nil {
		return queryError
	}
	defer rowscan.Close(rows, &err)

	for rows.Next() {
		var schemaName, tableName, columnName string
		if scanError := rows.Scan(&schemaName, &tableName, &columnName); scanError != nil {
			return scanError
		}
		schema := catalogue.Schemas[schemaName]
		if schema == nil || schema.Tables[tableName] == nil {
			continue
		}
		columns := schema.Tables[tableName].Columns
		for index := range columns {
			if columns[index].Name == columnName {
				columns[index].Hidden = true
			}
		}
	}

	return rows.Err()
}

// introspectExtensions fills the catalogue's Extensions set.
//
// Ignores the plpgsql extension.
//
// Takes catalogue (*sqlmodel.Catalogue) which receives entries.
//
// Returns err (error) when the query, a scan, or closing the rows fails.
func (provider *Source) introspectExtensions(
	ctx context.Context,
	catalogue *sqlmodel.Catalogue,
) (err error) {
	rows, queryError := provider.database.QueryContext(ctx, extensionsQuery)
	if queryError != nil {
		return queryError
	}
	defer rowscan.Close(rows, &err)

	for rows.Next() {
		var extensionName string
		if scanError := rows.Scan(&extensionName); scanError != nil {
			return scanError
		}
		catalogue.Extensions[extensionName] = struct{}{}
	}

	return rows.Err()
}

// assembleCompositeTypes materialises the composite types onto their schemas.
//
// Every field is nullable: PostgreSQL cannot constrain a composite attribute to NOT NULL.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the covered schemas.
// Takes resolver (*typeResolver) which resolves field types.
// Takes compositeMap (map[schemaObject][]compositeField) which maps a composite to its
// fields in declaration order.
// Takes compositeOrder ([]schemaObject) which preserves discovery order.
func assembleCompositeTypes(
	catalogue *sqlmodel.Catalogue,
	resolver *typeResolver,
	compositeMap map[schemaObject][]compositeField,
	compositeOrder []schemaObject,
) {
	for _, composite := range compositeOrder {
		schema := catalogue.Schemas[composite.schema]
		if schema == nil {
			continue
		}
		fields := compositeMap[composite]
		columns := make([]sqlmodel.Column, 0, len(fields))

		for _, field := range fields {
			column := resolver.column(composite.schema+"."+composite.name, field.attributeName, field.typeDescription, true)
			column.Nullable = true
			columns = append(columns, column)
		}

		schema.CompositeTypes[composite.name] = &sqlmodel.CompositeType{
			Name:   composite.name,
			Schema: schema.Name,
			Fields: columns,
			Origin: sqlmodel.MigrationOrigin{},
		}
	}
}
