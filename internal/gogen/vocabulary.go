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

package gogen

import (
	"go/ast"
	"unicode"
	"unicode/utf8"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// sentinelPrefix starts the name of every generated sentinel error.
	sentinelPrefix = "err"

	// errorValuePrefix starts the name of every exported generated error value.
	errorValuePrefix = "Err"

	// scanIntoSuffix completes the name of the inline JSON scan adapter.
	scanIntoSuffix = "ScanInto"

	// SuffixWrapError is the helper-prefix suffix of the helper that wraps an error a
	// generated method returns with a label.
	SuffixWrapError = "WrapError"

	// SuffixWrapResult is the helper-prefix suffix of the helper that wraps the error a call
	// returns beside a value with a label.
	SuffixWrapResult = "WrapResult"
)

// Vocabulary holds the names generated code is written with: the helper identifier
// prefix, the error text prefix, the directive prefixes stripped from SQL constants, and
// the JSON scan adapter. It is derived from an emit.Request once per emission and passed
// to every builder that names a generated helper.
type Vocabulary struct {
	// helperPrefix prefixes every generated helper identifier.
	helperPrefix string

	// errorPrefix prefixes the text of every generated error.
	errorPrefix string

	// jsonScanInto names the configured JSON scan adapter, or is zero for the inline one.
	jsonScanInto sqlmodel.GoType

	// directives holds the directive prefixes whose comment lines are stripped from SQL.
	directives namespace.Set

	// docComments keeps the doc comments of rendered helper sources.
	docComments bool

	// wrapsErrors wraps the errors generated methods return with a label naming the method.
	wrapsErrors bool
}

// NewVocabulary derives the vocabulary of one emission from its request.
//
// Takes request (*emit.Request) which supplies the style and the directive prefixes.
//
// Returns Vocabulary which names the generated helpers for that request.
func NewVocabulary(request *emit.Request) Vocabulary {
	directives := namespace.Set{Canonical: "", Aliases: nil, Foreign: nil}
	if len(request.DirectivePrefixes) > 0 {
		directives.Canonical = request.DirectivePrefixes[0]
		directives.Aliases = request.DirectivePrefixes[1:]
	}
	return Vocabulary{
		helperPrefix: request.Style.HelperPrefix,
		errorPrefix:  request.Style.ErrorPrefix,
		jsonScanInto: request.Style.JSONScanInto,
		directives:   directives,
		docComments:  request.Style.DocComments,
		wrapsErrors:  request.Style.WrapErrors,
	}
}

// Helper returns the name of a generated helper, such as sglMaxBindVariables for the
// suffix MaxBindVariables.
//
// Takes suffix (string) which is the helper name without its prefix.
//
// Returns string which is the prefixed helper identifier.
func (v Vocabulary) Helper(suffix string) string {
	return v.helperPrefix + suffix
}

// Sentinel returns the name of an unexported generated sentinel error, such as
// errSglTooManyBindVariables for the suffix TooManyBindVariables.
//
// Takes suffix (string) which is the sentinel name without its prefix.
//
// Returns string which is the sentinel identifier.
func (v Vocabulary) Sentinel(suffix string) string {
	return v.sentinelPrefix() + suffix
}

// ErrorValue returns the name of an exported generated error value, such as
// ErrSglUnknownColumn for the suffix UnknownColumn.
//
// The name is Err, then the error prefix in Go casing, then the suffix, so it reads as
// the error text it carries and stays distinct across presets: an error prefix of acme
// yields ErrAcmeUnknownColumn and acme_store yields ErrAcmeStoreUnknownColumn. The value
// is exported so callers of the generated code can match it with errors.Is.
//
// Takes suffix (string) which is the error value's name without its prefix.
//
// Returns string which is the exported error value identifier.
func (v Vocabulary) ErrorValue(suffix string) string {
	return errorValuePrefix + goast.SnakeToPascalCase(v.errorPrefix) + suffix
}

// Message returns generated error text prefixed with the error prefix, such as "sgl: too
// many bind variables".
//
// Takes text (string) which is the error text without its prefix.
//
// Returns string which is the prefixed error text.
func (v Vocabulary) Message(text string) string {
	return v.errorPrefix + ": " + text
}

// WrapsErrors reports whether generated methods wrap the errors they return with a label
// naming the method, as emit.Style.WrapErrors asks.
//
// Returns bool which is true when they do.
func (v Vocabulary) WrapsErrors() bool {
	return v.wrapsErrors
}

// WrapError returns the call that wraps an error a generated method returns with a label,
// as `sglWrapError(label, err)`, which returns nil for a nil error.
//
// Takes label (ast.Expr) which is the label text, such as "sgl: GetBoat".
// Takes failure (ast.Expr) which is the error.
//
// Returns ast.Expr which is the call.
func (v Vocabulary) WrapError(label ast.Expr, failure ast.Expr) ast.Expr {
	return goastutil.CallExpr(goastutil.CachedIdent(v.Helper(SuffixWrapError)), label, failure)
}

// DocComments reports whether rendered helper sources keep the comments that document
// their declarations, specs and fields, as the style's DocComments asks.
//
// Returns bool which is false when the helpers are rendered without them.
func (v Vocabulary) DocComments() bool {
	return v.docComments
}

// Directives returns the directive prefixes whose comment lines are stripped from the SQL
// constants of the generated code.
//
// Returns namespace.Set which holds the directive prefixes.
func (v Vocabulary) Directives() namespace.Set {
	return v.directives
}

// ScanIntoCall wraps a scan target in the JSON scan adapter. A configured adapter in
// another package is registered with the file's import tracker and referred to by the
// qualifier the tracker gives its package, so the call and the import always agree.
//
// Takes target (ast.Expr) which is the address the decoded value is stored in.
// Takes imports (*goast.ImportTracker) which collects the imports of the file the call is
// written in.
//
// Returns ast.Expr which is the adapter call.
func (v Vocabulary) ScanIntoCall(target ast.Expr, imports *goast.ImportTracker) ast.Expr {
	if v.jsonScanInto.Package == "" {
		return goastutil.CallExpr(goastutil.CachedIdent(v.InlineScanInto()), target)
	}
	adapter := sqlmodel.GoType{Composite: nil, Package: v.jsonScanInto.Package, Name: v.jsonScanInto.Name}
	return goastutil.CallExpr(imports.AddType(adapter), target)
}

// InlineScanInto returns the name of the inline JSON scan adapter.
//
// Returns string which is the helper prefix followed by ScanInto.
func (v Vocabulary) InlineScanInto() string {
	return v.Helper(scanIntoSuffix)
}

// sentinelPrefix returns err followed by the helper prefix with its first letter in upper
// case, so the sentinel errors stay unexported.
//
// Returns string which is the sentinel-error prefix.
func (v Vocabulary) sentinelPrefix() string {
	first, size := utf8.DecodeRuneInString(v.helperPrefix)
	if size == 0 {
		return sentinelPrefix
	}
	return sentinelPrefix + string(unicode.ToUpper(first)) + v.helperPrefix[size:]
}
