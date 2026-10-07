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

package sqlmodel

import "strconv"

var (
	// typeCategoryNames names each SQLTypeCategory in engine-neutral words, indexed by the
	// category's value.
	typeCategoryNames = [...]string{
		TypeCategoryInteger:        "integer",
		TypeCategoryFloat:          "float",
		TypeCategoryDecimal:        "decimal",
		TypeCategoryBoolean:        "boolean",
		TypeCategoryText:           "text",
		TypeCategoryBinary:         "binary",
		TypeCategoryTemporal:       "temporal",
		TypeCategoryJSON:           "json",
		TypeCategoryUUID:           "uuid",
		TypeCategoryNetwork:        "network",
		TypeCategoryGeometric:      "geometric",
		TypeCategoryEnum:           "enum",
		TypeCategoryComposite:      "composite",
		TypeCategoryArray:          "array",
		TypeCategoryRange:          "range",
		TypeCategoryStruct:         "struct",
		TypeCategoryMap:            "map",
		TypeCategoryUnion:          "union",
		TypeCategoryUnknown:        "unknown",
		TypeCategoryAggregateState: "aggregate-state",
	}

	// queryCommandNames names each QueryCommand as the sgl.query directive spells it,
	// indexed by the command's value.
	queryCommandNames = [...]string{
		QueryCommandOne:        "one",
		QueryCommandMany:       "many",
		QueryCommandExec:       "exec",
		QueryCommandExecResult: "execresult",
		QueryCommandExecRows:   "execrows",
		QueryCommandBatch:      "batch",
		QueryCommandStream:     "stream",
		QueryCommandCopyFrom:   "copyfrom",
		QueryCommandAsyncExec:  "asyncexec",
	}

	// parameterDirectiveKindNames names each ParameterDirectiveKind, indexed by the kind's
	// value.
	parameterDirectiveKindNames = [...]string{
		ParameterDirectiveNone:     "none",
		ParameterDirectiveParam:    "param",
		ParameterDirectiveSortable: "sortable",
	}

	// parameterContextNames names each ParameterContext, indexed by the context's value.
	parameterContextNames = [...]string{
		ParameterContextComparison:       "comparison",
		ParameterContextAssignment:       "assignment",
		ParameterContextFunctionArgument: "function-argument",
		ParameterContextCast:             "cast",
		ParameterContextInList:           "in-list",
		ParameterContextBetween:          "between",
		ParameterContextLimit:            "limit",
		ParameterContextOffset:           "offset",
		ParameterContextLike:             "like",
		ParameterContextUnknown:          "unknown",
	}

	// generatedKindNames names each GeneratedKind, indexed by the kind's value.
	generatedKindNames = [...]string{
		GeneratedKindNone:    "none",
		GeneratedKindVirtual: "virtual",
		GeneratedKindStored:  "stored",
	}

	// functionDataAccessNames names each FunctionDataAccess, indexed by its value.
	functionDataAccessNames = [...]string{
		DataAccessUnknown:      "unknown",
		DataAccessReadOnly:     "read-only",
		DataAccessModifiesData: "modifies-data",
	}

	// functionNullableBehaviourNames names each FunctionNullableBehaviour, indexed by its
	// value.
	functionNullableBehaviourNames = [...]string{
		FunctionNullableCalledOnNull:         "called-on-null",
		FunctionNullableReturnsNullOnNull:    "returns-null-on-null",
		FunctionNullableNeverNull:            "never-null",
		FunctionNullableNullWhenArgumentNull: "null-when-argument-null",
	}

	// constraintKindNames names each ConstraintKind, indexed by the kind's value.
	constraintKindNames = [...]string{
		ConstraintPrimaryKey: "primary-key",
		ConstraintForeignKey: "foreign-key",
		ConstraintUnique:     "unique",
		ConstraintCheck:      "check",
		ConstraintNotNull:    "not-null",
	}
)

// String returns the category's engine-neutral name, such as integer or text.
//
// Returns string which is the name, or SQLTypeCategory(n) for an undeclared value.
func (category SQLTypeCategory) String() string {
	return enumName(category, typeCategoryNames[:], "SQLTypeCategory")
}

// String returns the command as the sgl.query directive spells it, such as one or exec.
//
// Returns string which is the name, or QueryCommand(n) for an undeclared value.
func (command QueryCommand) String() string {
	return enumName(command, queryCommandNames[:], "QueryCommand")
}

// String returns the kind's name, such as none or sortable.
//
// Returns string which is the name, or ParameterDirectiveKind(n) for an undeclared value.
func (kind ParameterDirectiveKind) String() string {
	return enumName(kind, parameterDirectiveKindNames[:], "ParameterDirectiveKind")
}

// String returns the context's name, such as comparison or function-argument.
//
// Returns string which is the name, or ParameterContext(n) for an undeclared value.
func (context ParameterContext) String() string {
	return enumName(context, parameterContextNames[:], "ParameterContext")
}

// String returns the generated kind's name, such as none or stored.
//
// Returns string which is the name, or GeneratedKind(n) for an undeclared value.
func (kind GeneratedKind) String() string {
	return enumName(kind, generatedKindNames[:], "GeneratedKind")
}

// String returns the data access's name, such as read-only.
//
// Returns string which is the name, or FunctionDataAccess(n) for an undeclared value.
func (access FunctionDataAccess) String() string {
	return enumName(access, functionDataAccessNames[:], "FunctionDataAccess")
}

// String returns the behaviour's name, such as returns-null-on-null.
//
// Returns string which is the name, or FunctionNullableBehaviour(n) for an undeclared
// value.
func (behaviour FunctionNullableBehaviour) String() string {
	return enumName(behaviour, functionNullableBehaviourNames[:], "FunctionNullableBehaviour")
}

// String returns the constraint kind's name, such as primary-key.
//
// Returns string which is the name, or ConstraintKind(n) for an undeclared value.
func (kind ConstraintKind) String() string {
	return enumName(kind, constraintKindNames[:], "ConstraintKind")
}

// enumName looks a value up in its name table, falling back to the type name and the
// number for a value the table does not declare.
//
// Takes value (E) which is the enum value.
// Takes names ([]string) which is the name table indexed by value.
// Takes typeName (string) which names the type for the fallback.
//
// Returns string which is the declared name or typeName(n).
func enumName[E ~uint8](value E, names []string, typeName string) string {
	if int(value) < len(names) && names[value] != "" {
		return names[value]
	}
	return typeName + "(" + strconv.Itoa(int(value)) + ")"
}
