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
	"database/sql"

	"seagull.sh/seagull/internal/schema/internal/rowscan"
	"seagull.sh/seagull/internal/sqlmodel"
)

// listViews records the covered schemas' views, as viewsQuery selects them, in their
// schemas, closing the result set before the caller issues the metadata queries.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the covered schemas.
//
// Returns err (error) when the query, a scan, or closing the rows fails.
func (provider *Source) listViews(ctx context.Context, catalogue *sqlmodel.Catalogue) (err error) {
	rows, queryError := provider.database.QueryContext(ctx, viewsQuery)
	if queryError != nil {
		return queryError
	}
	defer rowscan.Close(rows, &err)

	for rows.Next() {
		var schemaName, name string
		var definition sql.NullString
		if scanError := rows.Scan(&schemaName, &name, &definition); scanError != nil {
			return scanError
		}
		schema := catalogue.Schemas[schemaName]
		if schema == nil {
			continue
		}
		schema.Views[name] = &sqlmodel.View{
			Name:       name,
			Schema:     schema.Name,
			Definition: definition.String,
			Columns:    nil,
			Comment:    "",
			Origin:     sqlmodel.MigrationOrigin{},
		}
	}
	return rows.Err()
}

// schemaColumns loads every listed table's and view's columns in one query, preserving
// declaration order.
//
// Takes resolver (*typeResolver) which resolves database type identifiers into SQL types.
// Takes catalogue (*sqlmodel.Catalogue) which holds the listed tables and views.
//
// Returns err (error) when metadata cannot be queried, scanned or closed successfully.
func (provider *Source) schemaColumns(ctx context.Context, resolver *typeResolver, catalogue *sqlmodel.Catalogue) (err error) {
	rows, queryError := provider.database.QueryContext(ctx, columnsQuery)
	if queryError != nil {
		return queryError
	}
	defer rowscan.Close(rows, &err)
	for rows.Next() {
		var schemaName, name string
		row, scanError := scanColumnRow(rows, &schemaName, &name)
		if scanError != nil {
			return scanError
		}
		schema := catalogue.Schemas[schemaName]
		if schema == nil {
			continue
		}
		if table := schema.Tables[name]; table != nil {
			table.Columns = append(table.Columns, columnFromRow(resolver, schemaName+"."+name, row))
		}
		if view := schema.Views[name]; view != nil {
			view.Columns = append(view.Columns, columnFromRow(resolver, schemaName+"."+name, row))
		}
	}
	return rows.Err()
}

// schemaConstraints loads the keys of every listed table with one ordered query.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the listed tables.
//
// Returns err (error) when metadata cannot be queried, scanned or closed successfully.
func (provider *Source) schemaConstraints(ctx context.Context, catalogue *sqlmodel.Catalogue) (err error) {
	rows, queryError := provider.database.QueryContext(ctx, constraintsQuery)
	if queryError != nil {
		return queryError
	}
	defer rowscan.Close(rows, &err)
	for rows.Next() {
		var schemaName, relation, name, kind, column string
		if scanError := rows.Scan(&schemaName, &relation, &name, &kind, &column, new(int)); scanError != nil {
			return scanError
		}
		table := listedTableNamed(catalogue, schemaName, relation)
		if table == nil {
			continue
		}
		switch kind {
		case "p":
			table.PrimaryKey = append(table.PrimaryKey, column)
		case "u":
			last := len(table.Constraints) - 1
			if last < 0 || table.Constraints[last].Name != name {
				table.Constraints = append(table.Constraints, sqlmodel.Constraint{
					Name:           name,
					Kind:           sqlmodel.ConstraintUnique,
					Columns:        nil,
					ForeignColumns: nil,
					ForeignTable:   "",
					Origin:         sqlmodel.MigrationOrigin{},
				})
				last++
			}
			table.Constraints[last].Columns = append(table.Constraints[last].Columns, column)
		}
	}
	return rows.Err()
}

// schemaIndexes loads the simple-column members of every listed table's indexes in their
// original order.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the listed tables.
//
// Returns err (error) when metadata cannot be queried, scanned or closed successfully.
func (provider *Source) schemaIndexes(ctx context.Context, catalogue *sqlmodel.Catalogue) (err error) {
	rows, queryError := provider.database.QueryContext(ctx, indexesQuery)
	if queryError != nil {
		return queryError
	}
	defer rowscan.Close(rows, &err)
	for rows.Next() {
		var schemaName, relation, name, column string
		var unique, primary bool
		if scanError := rows.Scan(&schemaName, &relation, &name, &unique, &primary, &column, new(int)); scanError != nil {
			return scanError
		}
		table := listedTableNamed(catalogue, schemaName, relation)
		if table == nil {
			continue
		}
		last := len(table.Indexes) - 1
		if last < 0 || table.Indexes[last].Name != name {
			table.Indexes = append(table.Indexes, sqlmodel.Index{
				Name:      name,
				IsUnique:  unique,
				IsPrimary: primary,
				Columns:   nil,
				Origin:    sqlmodel.MigrationOrigin{},
			})
			last++
		}
		table.Indexes[last].Columns = append(table.Indexes[last].Columns, column)
	}
	return rows.Err()
}

// listedTableNamed finds a table the catalogue lists.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the listed tables.
// Takes schemaName (string) which is the table's schema.
// Takes name (string) which is the table's name.
//
// Returns *sqlmodel.Table which is the table, or nil when the catalogue does not list it,
// such as a table an extension owns.
func listedTableNamed(catalogue *sqlmodel.Catalogue, schemaName, name string) *sqlmodel.Table {
	schema := catalogue.Schemas[schemaName]
	if schema == nil {
		return nil
	}
	return schema.Tables[name]
}
