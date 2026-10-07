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

package seagull_test

import (
	"testing"

	"seagull.sh/seagull"
	"seagull.sh/seagull/internal/apitest"
)

func TestFacadeAPI(t *testing.T) {
	surface := apitest.Surface{
		"Generator":         seagull.Generator{},
		"New":               seagull.New,
		"ContextWithLogger": seagull.ContextWithLogger,

		"Input":             seagull.Input{},
		"Result":            seagull.Result{},
		"DetailedResult":    seagull.DetailedResult{},
		"Report":            seagull.Report{},
		"DiagnosticsError":  seagull.DiagnosticsError{},
		"SchemaSourcePort":  (*seagull.SchemaSourcePort)(nil),
		"TypeOverride":      seagull.TypeOverride{},
		"CustomFunction":    seagull.CustomFunction{},
		"TypeSlots":         seagull.TypeSlots{},
		"Slot":              seagull.Slot{},
		"GoType":            seagull.GoType{},
		"QueryNames":        seagull.QueryNames{},
		"Interface":         seagull.Interface{},
		"JSONTags":          seagull.JSONTags{},
		"JSONCase":          seagull.JSONCase(""),
		"JSONCaseAsWritten": seagull.JSONCaseAsWritten,
		"JSONCaseSnake":     seagull.JSONCaseSnake,
		"JSONCaseCamel":     seagull.JSONCaseCamel,
		"JSONCasePascal":    seagull.JSONCasePascal,

		"Option":                       seagull.Option(nil),
		"WithEmitter":                  seagull.WithEmitter,
		"WithPreset":                   seagull.WithPreset,
		"WithDirectivePrefixes":        seagull.WithDirectivePrefixes,
		"WithForeignDirectivePrefixes": seagull.WithForeignDirectivePrefixes,
		"WithTypeOverrides":            seagull.WithTypeOverrides,
		"WithPackageNames":             seagull.WithPackageNames,
		"WithCustomFunctions":          seagull.WithCustomFunctions,
		"WithSchemaSource":             seagull.WithSchemaSource,
		"WithRuntimeTables":            seagull.WithRuntimeTables,
		"WithLogger":                   seagull.WithLogger,
		"WithObserver":                 seagull.WithObserver,
		"WithClock":                    seagull.WithClock,
		"WithMaxFileBytes":             seagull.WithMaxFileBytes,
		"WithMaxTotalBytes":            seagull.WithMaxTotalBytes,
		"WithMaxFiles":                 seagull.WithMaxFiles,
		"WithMaxQueries":               seagull.WithMaxQueries,
		"WithMaxDiagnostics":           seagull.WithMaxDiagnostics,
		"WithSkipFailingQueries":       seagull.WithSkipFailingQueries,

		"Preset":        seagull.Preset{},
		"Directives":    seagull.Directives{},
		"DefaultPreset": seagull.DefaultPreset,

		"WriteOption":         seagull.WriteOption(nil),
		"WriteFiles":          seagull.WriteFiles,
		"WithPrune":           seagull.WithPrune,
		"WithHeader":          seagull.WithHeader,
		"WithLegacyHeaders":   seagull.WithLegacyHeaders,
		"WithPermissions":     seagull.WithPermissions,
		"FormatDirectives":    seagull.FormatDirectives,
		"FormatDirectivesFor": seagull.FormatDirectivesFor,

		"ErrInvalidConfig": seagull.ErrInvalidConfig,
		"ErrLimit":         seagull.ErrLimit,
		"ErrParse":         seagull.ErrParse,
		"ErrPanic":         seagull.ErrPanic,
		"ErrDiagnostics":   seagull.ErrDiagnostics,
		"ErrNoSource":      seagull.ErrNoSource,
		"ErrInvalidPrefix": seagull.ErrInvalidPrefix,
		"ErrFileTooLarge":  seagull.ErrFileTooLarge,
		"ErrMalformedSQL":  seagull.ErrMalformedSQL,

		"SourceError":     seagull.SourceError{},
		"ErrorSeverity":   seagull.ErrorSeverity(0),
		"SeverityError":   seagull.SeverityError,
		"SeverityWarning": seagull.SeverityWarning,
		"SeverityHint":    seagull.SeverityHint,
		"CodeInfo":        seagull.CodeInfo{},
		"Codes":           seagull.Codes,
		"LookupCode":      seagull.LookupCode,
		"Presentation":    seagull.Presentation{},
		"Present":         seagull.Present,
		"WriteText":       seagull.WriteText,
		"DidYouMean":      seagull.DidYouMean,

		"CodeUnknownColumn":                    seagull.CodeUnknownColumn,
		"CodeAmbiguousColumn":                  seagull.CodeAmbiguousColumn,
		"CodeUnknownTable":                     seagull.CodeUnknownTable,
		"CodeExpressionTypeError":              seagull.CodeExpressionTypeError,
		"CodeUnknownFunction":                  seagull.CodeUnknownFunction,
		"CodeDuplicateQueryName":               seagull.CodeDuplicateQueryName,
		"CodeDirectiveSyntax":                  seagull.CodeDirectiveSyntax,
		"CodeMissingDirective":                 seagull.CodeMissingDirective,
		"CodeCommandOutputMismatch":            seagull.CodeCommandOutputMismatch,
		"CodeParseError":                       seagull.CodeParseError,
		"CodeSortableColumnMissing":            seagull.CodeSortableColumnMissing,
		"CodeMultipleStatements":               seagull.CodeMultipleStatements,
		"CodeGeneratedColumn":                  seagull.CodeGeneratedColumn,
		"CodeGroupByColumnMissing":             seagull.CodeGroupByColumnMissing,
		"CodeGroupByMissingEmbed":              seagull.CodeGroupByMissingEmbed,
		"CodeGroupByWrongCommand":              seagull.CodeGroupByWrongCommand,
		"CodeSliceBatchCopyFrom":               seagull.CodeSliceBatchCopyFrom,
		"CodeSliceDynamicRuntime":              seagull.CodeSliceDynamicRuntime,
		"CodeCompoundColumnCount":              seagull.CodeCompoundColumnCount,
		"CodeRuntimeBuilderBaseHasWhere":       seagull.CodeRuntimeBuilderBaseHasWhere,
		"CodeCountSemanticsWrapped":            seagull.CodeCountSemanticsWrapped,
		"CodeCountRewriteUnavailable":          seagull.CodeCountRewriteUnavailable,
		"CodeUnknownDirective":                 seagull.CodeUnknownDirective,
		"CodeUnknownKeywordArgument":           seagull.CodeUnknownKeywordArgument,
		"CodeInvalidKeywordArgumentValue":      seagull.CodeInvalidKeywordArgumentValue,
		"CodeDuplicateKeywordArgument":         seagull.CodeDuplicateKeywordArgument,
		"CodeInternalNilGuard":                 seagull.CodeInternalNilGuard,
		"CodeMissingRequired":                  seagull.CodeMissingRequired,
		"CodeUnclosedDirective":                seagull.CodeUnclosedDirective,
		"CodeParameterBindingSyntax":           seagull.CodeParameterBindingSyntax,
		"CodeInvalidListLiteral":               seagull.CodeInvalidListLiteral,
		"CodeUnterminatedString":               seagull.CodeUnterminatedString,
		"CodeUnknownOverrideColumn":            seagull.CodeUnknownOverrideColumn,
		"CodeUnknownOverrideMigrationColumn":   seagull.CodeUnknownOverrideMigrationColumn,
		"CodeMutuallyExclusiveKeywordArgument": seagull.CodeMutuallyExclusiveKeywordArgument,
		"CodeDirectiveLineLimit":               seagull.CodeDirectiveLineLimit,
		"CodeAsyncExecRecommended":             seagull.CodeAsyncExecRecommended,
		"CodeAsyncExecNotSupported":            seagull.CodeAsyncExecNotSupported,
		"CodeUnreferencedParameter":            seagull.CodeUnreferencedParameter,
		"CodeMigrationMixedTransaction":        seagull.CodeMigrationMixedTransaction,
		"CodeDirectiveWrongContext":            seagull.CodeDirectiveWrongContext,
		"CodeFunctionDataAccessUndeclared":     seagull.CodeFunctionDataAccessUndeclared,
		"CodeConflictDoNothingReturning":       seagull.CodeConflictDoNothingReturning,
		"CodeOptionalNonOneCommand":            seagull.CodeOptionalNonOneCommand,
		"CodeIgnoredMigrationFile":             seagull.CodeIgnoredMigrationFile,
		"CodeUnusableGoTypeOverride":           seagull.CodeUnusableGoTypeOverride,
		"CodeAliasPrefix":                      seagull.CodeAliasPrefix,
		"CodeForeignPrefix":                    seagull.CodeForeignPrefix,
		"CodeReturningNotSupported":            seagull.CodeReturningNotSupported,
		"CodeDuplicateMigrationVersion":        seagull.CodeDuplicateMigrationVersion,
		"CodeDepthLimit":                       seagull.CodeDepthLimit,
		"CodeSliceParameterShape":              seagull.CodeSliceParameterShape,
		"CodeOptionalNotNullable":              seagull.CodeOptionalNotNullable,
		"CodeSortableOrderByParameter":         seagull.CodeSortableOrderByParameter,
		"CodeParametersBeforeStatement":        seagull.CodeParametersBeforeStatement,
		"CodeEmitterUnsupported":               seagull.CodeEmitterUnsupported,
		"CodeBatchShape":                       seagull.CodeBatchShape,
		"CodeUnmatchedTypeOverride":            seagull.CodeUnmatchedTypeOverride,
		"CodeUngroupedColumn":                  seagull.CodeUngroupedColumn,
		"CodeEngineReading":                    seagull.CodeEngineReading,
		"CodeInsertShape":                      seagull.CodeInsertShape,
		"CodeRowType":                          seagull.CodeRowType,
		"CodeDiagnosticLimit":                  seagull.CodeDiagnosticLimit,
		"CodeDuplicateObject":                  seagull.CodeDuplicateObject,
		"CodeUnknownObject":                    seagull.CodeUnknownObject,
		"CodePartlyAnalysed":                   seagull.CodePartlyAnalysed,
		"CodeRecoveredPanic":                   seagull.CodeRecoveredPanic,
		"CodeViewDerivationUnsettled":          seagull.CodeViewDerivationUnsettled,

		"CodegenVersion": seagull.CodegenVersion,
		"Version":        seagull.Version,
	}

	apitest.Check(t, surface, "facade_test.golden.yaml")
}
