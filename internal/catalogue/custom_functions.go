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
	"errors"
	"fmt"
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/sqlmodel"
)

// ValidateCustomFunctions checks every custom function declaration, so that a generator
// is never built with one it would have to drop or guess at.
//
// Takes customFunctions ([]sqlmodel.CustomFunctionConfig) which are the declarations.
//
// Returns error which wraps fault.ErrInvalidConfig and names the first unusable
// declaration by its position and name, or nil when every declaration is usable.
func ValidateCustomFunctions(customFunctions []sqlmodel.CustomFunctionConfig) error {
	for index := range customFunctions {
		if err := validateCustomFunction(&customFunctions[index]); err != nil {
			name := customFunctions[index].Name
			if name == "" {
				return fmt.Errorf("custom function %d: %w: %w", index, err, fault.ErrInvalidConfig)
			}
			return fmt.Errorf("custom function %d (%s): %w: %w", index, name, err, fault.ErrInvalidConfig)
		}
	}
	return nil
}

// MergeCustomFunctions adds the declared custom functions to the catalogue's default
// schema, creating that schema when the catalogue has none yet. A later CREATE FUNCTION
// with the same name and argument types replaces the declaration.
//
// A declared name is read as SQL would read it written without quotes: it is folded by
// the catalogue's identifier rules, and the function is filed under the key those rules
// give it.
//
// The declarations must have passed ValidateCustomFunctions; a declaration that has not
// is still added, with the default for any field it could not read.
//
// Takes catalogue (*sqlmodel.Catalogue) which receives the functions.
// Takes engine (dialect.TypeSystemPort) which normalises the declared type names.
// Takes customFunctions ([]sqlmodel.CustomFunctionConfig) which are the declarations.
func MergeCustomFunctions(
	catalogue *sqlmodel.Catalogue,
	engine dialect.TypeSystemPort,
	customFunctions []sqlmodel.CustomFunctionConfig,
) {
	if catalogue == nil || len(customFunctions) == 0 {
		return
	}
	if catalogue.Schemas == nil {
		catalogue.Schemas = make(map[string]*sqlmodel.Schema)
	}
	key := catalogue.Key(catalogue.DefaultSchema)
	schema := catalogue.Schemas[key]
	if schema == nil {
		schema = newEmptySchema(catalogue.DefaultSchema)
		catalogue.Schemas[key] = schema
	}
	if schema.Functions == nil {
		schema.Functions = make(map[string][]*sqlmodel.Function)
	}

	for index := range customFunctions {
		function := convertCustomFunction(&customFunctions[index], engine)
		function.Schema = schema.Name
		function.Name = catalogue.Identifiers.Fold(function.Name, false)
		functionName := catalogue.Key(function.Name)
		schema.Functions[functionName] = append(schema.Functions[functionName], function)
	}
}

// validateCustomFunction checks one custom function declaration.
//
// Takes config (*sqlmodel.CustomFunctionConfig) which is the declaration.
//
// Returns error which describes the first problem, or nil.
func validateCustomFunction(config *sqlmodel.CustomFunctionConfig) error {
	switch {
	case strings.TrimSpace(config.Name) == "":
		return errors.New("the name is empty")
	case strings.TrimSpace(config.ReturnType) == "":
		return errors.New("the return type is empty")
	case config.MinArguments < 0:
		return fmt.Errorf("the minimum argument count %d is negative", config.MinArguments)
	case config.MinArguments > len(config.Arguments):
		return fmt.Errorf("the minimum argument count %d is larger than the %d declared arguments",
			config.MinArguments, len(config.Arguments))
	case config.IsVariadic && len(config.Arguments) == 0:
		return errors.New("a variadic function needs at least one argument to repeat")
	}
	for index, argument := range config.Arguments {
		if strings.TrimSpace(argument) == "" {
			return fmt.Errorf("argument %d has no type", index+1)
		}
	}
	if _, err := parseNullableBehaviour(config.Nullable); err != nil {
		return err
	}
	if _, err := parseDataAccess(config.DataAccess); err != nil {
		return err
	}
	return nil
}

// convertCustomFunction converts a custom function declaration into the catalogue's form
// of a function. Arguments carry no names, since a declaration gives only their types, so
// a parameter bound to one is named from the call instead.
//
// Takes config (*sqlmodel.CustomFunctionConfig) which is the declaration.
// Takes engine (dialect.TypeSystemPort) which normalises the declared type names.
//
// Returns *sqlmodel.Function which is the function.
func convertCustomFunction(
	config *sqlmodel.CustomFunctionConfig,
	engine dialect.TypeSystemPort,
) *sqlmodel.Function {
	arguments := make([]sqlmodel.FunctionArgument, len(config.Arguments))
	for argumentIndex, typeName := range config.Arguments {
		arguments[argumentIndex] = sqlmodel.FunctionArgument{
			Name:       "",
			Type:       engine.NormaliseTypeName(typeName),
			IsOptional: false,
			NotNull:    false,
		}
	}

	minimumArguments := config.MinArguments
	if minimumArguments == 0 {
		minimumArguments = len(config.Arguments)
	}

	nullableBehaviour, _ := parseNullableBehaviour(config.Nullable)
	dataAccess, _ := parseDataAccess(config.DataAccess)

	return &sqlmodel.Function{
		Name:                   config.Name,
		Arguments:              arguments,
		ReturnType:             engine.NormaliseTypeName(config.ReturnType),
		IsAggregate:            config.IsAggregate,
		NullableBehaviour:      nullableBehaviour,
		NullingArgument:        0,
		IsVariadic:             config.IsVariadic,
		ReturnTypeFromArgument: 0,
		MinArguments:           minimumArguments,
		Schema:                 "",
		Language:               "",
		BodySQL:                "",
		ResultName:             "",
		Origin:                 sqlmodel.MigrationOrigin{},
		ReturnsSet:             false,
		IsStrict:               false,
		DataAccess:             dataAccess,
	}
}

// parseNullableBehaviour converts a nullable string value into the corresponding
// FunctionNullableBehaviour enum. An empty value, "null_on_null", or
// "returns_null_on_null" selects the returns-null-on-null default.
//
// Takes nullable (string) which is the raw nullable configuration value.
//
// Returns sqlmodel.FunctionNullableBehaviour which is the parsed enum value.
// Returns error when the value is non-empty and unrecognised.
func parseNullableBehaviour(nullable string) (sqlmodel.FunctionNullableBehaviour, error) {
	switch strings.ToLower(nullable) {
	case "", "null_on_null", "returns_null_on_null":
		return sqlmodel.FunctionNullableReturnsNullOnNull, nil
	case "never_null":
		return sqlmodel.FunctionNullableNeverNull, nil
	case "called_on_null":
		return sqlmodel.FunctionNullableCalledOnNull, nil
	default:
		return sqlmodel.FunctionNullableReturnsNullOnNull, fmt.Errorf(
			"unrecognised nullable behaviour %q (expected null_on_null, never_null, or called_on_null)",
			nullable,
		)
	}
}

// parseDataAccess converts a data access value into the catalogue's data access. An empty
// value or "read_only" selects read-only, the default, so a declared function never sends
// a query to the writer unless it says it changes data.
//
// Takes dataAccess (string) which is the raw configuration value.
//
// Returns sqlmodel.FunctionDataAccess which is the parsed data access.
// Returns error when the value is non-empty and unrecognised.
func parseDataAccess(dataAccess string) (sqlmodel.FunctionDataAccess, error) {
	switch strings.ToLower(dataAccess) {
	case "", "read_only":
		return sqlmodel.DataAccessReadOnly, nil
	case "modifies_data":
		return sqlmodel.DataAccessModifiesData, nil
	default:
		return sqlmodel.DataAccessReadOnly, fmt.Errorf(
			"unrecognised data access %q (expected read_only or modifies_data)", dataAccess)
	}
}
