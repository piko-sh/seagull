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

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/schema/internal/rowscan"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// parameterModeIn is the pg_proc.proargmodes code for an IN parameter, and the mode
	// assumed when proargmodes is NULL because every parameter is IN.
	parameterModeIn = "i"

	// parameterModeInOut is the pg_proc.proargmodes code for an INOUT parameter.
	parameterModeInOut = "b"

	// parameterModeVariadic is the pg_proc.proargmodes code for a VARIADIC parameter.
	parameterModeVariadic = "v"

	// procKindAggregate is the pg_proc.prokind code for an aggregate function.
	procKindAggregate = "a"

	// procKindWindow is the pg_proc.prokind code for a window function.
	procKindWindow = "w"

	// functionParametersQuery lists the functions, aggregates, and window functions of the
	// covered schemas with one row per declared parameter, in declaration order.
	functionParametersQuery = `SELECT
			n.nspname,
			p.oid::bigint,
			p.proname,
			p.prokind::text,
			p.proisstrict,
			p.proretset,
			p.provariadic <> 0,
			p.pronargdefaults::integer,
			format_type(p.prorettype, NULL),
			parameter.mode,
			parameter.name,
			parameter.type_name
		 FROM pg_proc p
		 JOIN pg_namespace n ON p.pronamespace = n.oid
		 LEFT JOIN LATERAL (
			SELECT
				declared.ordinal,
				COALESCE(p.proargmodes[declared.ordinal]::text, 'i') AS mode,
				COALESCE(p.proargnames[declared.ordinal], '') AS name,
				format_type(declared.type_oid, NULL) AS type_name
			FROM unnest(COALESCE(p.proallargtypes, p.proargtypes::oid[]))
				WITH ORDINALITY AS declared(type_oid, ordinal)
		 ) parameter ON true
		 WHERE ` + inCoveredSchema + ` AND p.prokind IN ('f', 'a', 'w')
		 ORDER BY n.nspname, p.proname, p.oid, parameter.ordinal`
)

// functionParameterRow holds a function's attributes alongside one of its declared
// parameters as a row of functionParametersQuery.
type functionParameterRow struct {
	// schemaName is the schema holding the function.
	schemaName string

	// functionName is the function's name.
	functionName string

	// procKind is the pg_proc.prokind code.
	procKind string

	// returnType is the formatted pg_proc.prorettype, "record" for functions returning a
	// table or several OUT parameters.
	returnType string

	// parameterMode is the parameter's pg_proc.proargmodes code, NULL when the function
	// declares no parameters.
	parameterMode sql.NullString

	// parameterName is the parameter's name, empty when unnamed and NULL when the function
	// declares no parameters.
	parameterName sql.NullString

	// parameterType is the parameter's formatted type, NULL when the function declares no
	// parameters.
	parameterType sql.NullString

	// functionID is the function's pg_proc OID, which distinguishes overloads.
	functionID int64

	// defaultCount is pg_proc.pronargdefaults, the number of trailing input parameters that
	// have defaults.
	defaultCount int

	// isStrict reports pg_proc.proisstrict, set when a NULL argument yields a NULL result.
	isStrict bool

	// returnsSet reports pg_proc.proretset, set when the result is a set of rows.
	returnsSet bool

	// isVariadic reports whether pg_proc.provariadic names a variadic element type.
	isVariadic bool
}

// functionParameter is one declared parameter of an introspected function.
type functionParameter struct {
	// mode is the pg_proc.proargmodes code.
	mode string

	// name is the parameter's name, empty when unnamed.
	name string

	// typeName is the parameter's formatted type.
	typeName string
}

// introspectedFunction gathers the rows of one function into its attributes and its
// parameters in declaration order.
type introspectedFunction struct {
	// schema is the schema holding the function.
	schema string

	// name is the function's name.
	name string

	// procKind is the pg_proc.prokind code.
	procKind string

	// returnType is the formatted return type.
	returnType string

	// parameters lists every declared parameter, including OUT and TABLE ones.
	parameters []functionParameter

	// defaultCount is the number of trailing input parameters that have defaults.
	defaultCount int

	// isStrict reports whether a NULL argument yields a NULL result.
	isStrict bool

	// returnsSet reports whether the result is a set of rows.
	returnsSet bool

	// isVariadic reports whether the last input parameter is VARIADIC.
	isVariadic bool
}

// introspectFunctions fills the covered schemas' Functions maps, filing each function
// under the key the catalogue gives the name the database stores, as a replayed CREATE
// FUNCTION is filed, so a quoted mixed-case function and an unquoted one of the same
// letters stay two functions.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the covered schemas.
//
// Returns error when the query, a scan, or closing the rows fails.
func (provider *Source) introspectFunctions(
	ctx context.Context,
	catalogue *sqlmodel.Catalogue,
) error {
	parameterRows, queryError := provider.queryFunctionParameters(ctx)
	if queryError != nil {
		return queryError
	}

	for _, function := range groupFunctionParameterRows(parameterRows) {
		schema := catalogue.Schemas[function.schema]
		if schema == nil {
			continue
		}
		signature := buildFunctionSignature(function, schema.Name, provider.typeNormaliser)
		key := catalogue.Key(signature.Name)
		schema.Functions[key] = append(schema.Functions[key], signature)
	}

	return nil
}

// queryFunctionParameters reads every function, aggregate, and window function in the
// covered schemas, one row per declared parameter.
//
// Returns parameterRows ([]functionParameterRow) which holds the rows ordered by schema,
// function and parameter position.
// Returns err (error) when the query, a scan, or closing the rows fails.
func (provider *Source) queryFunctionParameters(
	ctx context.Context,
) (parameterRows []functionParameterRow, err error) {
	rows, queryError := provider.database.QueryContext(ctx, functionParametersQuery)
	if queryError != nil {
		return nil, queryError
	}
	defer rowscan.Close(rows, &err)

	for rows.Next() {
		var row functionParameterRow
		scanError := rows.Scan(
			&row.schemaName,
			&row.functionID,
			&row.functionName,
			&row.procKind,
			&row.isStrict,
			&row.returnsSet,
			&row.isVariadic,
			&row.defaultCount,
			&row.returnType,
			&row.parameterMode,
			&row.parameterName,
			&row.parameterType,
		)
		if scanError != nil {
			return nil, scanError
		}
		parameterRows = append(parameterRows, row)
	}

	if rowsError := rows.Err(); rowsError != nil {
		return nil, rowsError
	}

	return parameterRows, nil
}

// groupFunctionParameterRows gathers consecutive rows of the same function into one
// introspected function, preserving row order.
//
// Takes rows ([]functionParameterRow) which are ordered by function and parameter
// position.
//
// Returns []introspectedFunction which holds one entry per function.
func groupFunctionParameterRows(rows []functionParameterRow) []introspectedFunction {
	var functions []introspectedFunction
	for index := range rows {
		row := &rows[index]
		if index == 0 || row.functionID != rows[index-1].functionID {
			functions = append(functions, introspectedFunction{
				schema:       row.schemaName,
				name:         row.functionName,
				procKind:     row.procKind,
				returnType:   row.returnType,
				parameters:   nil,
				defaultCount: row.defaultCount,
				isStrict:     row.isStrict,
				returnsSet:   row.returnsSet,
				isVariadic:   row.isVariadic,
			})
		}
		if row.parameterType.Valid {
			current := &functions[len(functions)-1]
			current.parameters = append(current.parameters, functionParameter{
				mode:     row.parameterMode.String,
				name:     row.parameterName.String,
				typeName: row.parameterType.String,
			})
		}
	}
	return functions
}

// buildFunctionSignature converts an introspected function into the signature the
// analyser resolves calls against.
//
// Only IN, INOUT, and VARIADIC parameters become arguments; OUT and TABLE parameters
// describe the result. The trailing defaultCount arguments are optional, so the minimum
// argument count is the number of arguments without a default. A VARIADIC parameter
// without a default therefore needs at least one value, matching PostgreSQL.
//
// Takes function (introspectedFunction) which is the gathered function.
// Takes schemaName (string) which is the owning schema.
// Takes typeNormaliser (dialect.TypeNormaliserPort) which maps each formatted type name.
//
// Returns *sqlmodel.Function which describes the function.
func buildFunctionSignature(
	function introspectedFunction,
	schemaName string,
	typeNormaliser dialect.TypeNormaliserPort,
) *sqlmodel.Function {
	var inputs []functionParameter
	for _, parameter := range function.parameters {
		if isInputParameterMode(parameter.mode) {
			inputs = append(inputs, parameter)
		}
	}

	minimumArguments := max(len(inputs)-function.defaultCount, 0)
	arguments := make([]sqlmodel.FunctionArgument, 0, len(inputs))
	for index, parameter := range inputs {
		arguments = append(arguments, sqlmodel.FunctionArgument{
			Name:       parameter.name,
			Type:       typeNormaliser.NormaliseTypeName(parameter.typeName),
			IsOptional: index >= minimumArguments,
			NotNull:    false,
		})
	}

	nullableBehaviour := sqlmodel.FunctionNullableCalledOnNull
	if function.isStrict {
		nullableBehaviour = sqlmodel.FunctionNullableReturnsNullOnNull
	}

	options := []sqlmodel.FunctionOption{
		sqlmodel.WithFunctionName(function.name),
		sqlmodel.WithMinArguments(minimumArguments),
	}
	if function.procKind == procKindAggregate || function.procKind == procKindWindow {
		options = append(options, sqlmodel.WithAggregate())
	}
	if function.returnsSet {
		options = append(options, sqlmodel.WithReturnsSet())
	}
	if function.isVariadic {
		options = append(options, sqlmodel.WithVariadic(minimumArguments))
	}

	signature := sqlmodel.NewFunction(
		arguments,
		typeNormaliser.NormaliseTypeName(function.returnType),
		nullableBehaviour,
		options...,
	)
	signature.Schema = schemaName
	signature.IsStrict = function.isStrict
	return signature
}

// isInputParameterMode reports whether a parameter of the given mode is passed by the
// caller. OUT ("o") and RETURNS TABLE ("t") parameters describe the result instead.
//
// Takes mode (string) which is the pg_proc.proargmodes code.
//
// Returns bool which is true for IN, INOUT, and VARIADIC parameters.
func isInputParameterMode(mode string) bool {
	switch mode {
	case parameterModeIn, parameterModeInOut, parameterModeVariadic:
		return true
	default:
		return false
	}
}
