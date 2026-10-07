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

package methods

import (
	"go/ast"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/goast"
)

const (
	// yieldArguments is how many arguments an iterator's yield takes: a row and an error.
	yieldArguments = 2

	// pairResults is how many results a method returning a value beside its error has.
	pairResults = 2
)

// errorLabeller rewrites one generated method so each error it returns carries a label
// naming the method.
type errorLabeller struct {
	// first is the type of the method's first result, which a call returning a value beside
	// its error is wrapped for.
	first ast.Expr

	// label is the text every error is labelled with, such as "sgl: GetBoat".
	label string

	// vocabulary names the wrapping helpers.
	vocabulary gogen.Vocabulary

	// results is the number of method results.
	results int

	// endsInError reports that the method's last result is an error.
	endsInError bool
}

// visit labels the errors a block returns and yields, looking into a function literal
// only for what it yields, since its returns are its own.
//
// Takes block (*ast.BlockStmt) which is the block.
// Takes nested (bool) which is true inside a function literal.
func (labeller *errorLabeller) visit(block *ast.BlockStmt, nested bool) {
	ast.Inspect(block, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.FuncLit:
			labeller.visit(typed.Body, true)
			return false
		case *ast.ReturnStmt:
			if !nested && labeller.endsInError {
				labeller.labelReturn(typed)
			}
		case *ast.CallExpr:
			labeller.labelYield(typed)
		}
		return true
	})
}

// labelReturn labels the error a return statement hands back: its last value, or, for a
// method returning a value beside its error from one call, the call's error.
//
// Takes statement (*ast.ReturnStmt) which is the statement; it is changed in place.
func (labeller *errorLabeller) labelReturn(statement *ast.ReturnStmt) {
	switch {
	case len(statement.Results) == labeller.results:
		last := statement.Results[labeller.results-1]
		if !labeller.settled(last) {
			statement.Results[labeller.results-1] = labeller.vocabulary.WrapError(goastutil.StrLit(labeller.label), last)
		}
	case len(statement.Results) == 1 && labeller.results == pairResults:
		call, isCall := statement.Results[0].(*ast.CallExpr)
		if !isCall || labeller.settled(call) {
			return
		}
		wrapper := goastutil.CallExpr(
			&ast.IndexExpr{X: goastutil.CachedIdent(labeller.vocabulary.Helper(gogen.SuffixWrapResult)), Index: labeller.first},
			goastutil.StrLit(labeller.label))
		statement.Results[0] = goastutil.CallExpr(wrapper, call)
	}
}

// labelYield labels the error a stream's iterator yields beside a row.
//
// Takes call (*ast.CallExpr) which is a call; only a call of yield is changed.
func (labeller *errorLabeller) labelYield(call *ast.CallExpr) {
	function, isIdent := call.Fun.(*ast.Ident)
	if !isIdent || function.Name != identYield || len(call.Args) != yieldArguments {
		return
	}
	if failure := call.Args[yieldArguments-1]; !labeller.settled(failure) {
		call.Args[yieldArguments-1] = labeller.vocabulary.WrapError(goastutil.StrLit(labeller.label), failure)
	}
}

// settled reports whether an error expression needs no label: nil, or a call already
// labelling one.
//
// Takes failure (ast.Expr) which is the expression.
//
// Returns bool which is true when the expression is left as it is.
func (labeller *errorLabeller) settled(failure ast.Expr) bool {
	if identifier, isIdent := failure.(*ast.Ident); isIdent {
		return identifier.Name == goast.IdentNil
	}
	call, isCall := failure.(*ast.CallExpr)
	if !isCall {
		return false
	}
	if inner, wrapsResult := call.Fun.(*ast.CallExpr); wrapsResult {
		call = inner
	}
	switch function := call.Fun.(type) {
	case *ast.Ident:
		return function.Name == labeller.vocabulary.Helper(gogen.SuffixWrapError)
	case *ast.IndexExpr:
		named, isIdent := function.X.(*ast.Ident)
		return isIdent && named.Name == labeller.vocabulary.Helper(gogen.SuffixWrapResult)
	default:
		return false
	}
}

// LabelErrors makes a generated method return every error it meets labelled with a name,
// through the package's sglWrapError and sglWrapResult helpers, so errors.Is still finds
// the error beneath: the error each return statement of the method's own body hands back,
// nil left alone, and the error a stream's iterator yields.
//
// An error already labelled, such as a batch's naming the row its statement starts at, is
// left as it is.
//
// Takes vocabulary (gogen.Vocabulary) which names the helpers and the error prefix.
// Takes name (string) which names the method in the label, after the error prefix.
// Takes declaration (*ast.FuncDecl) which is the method; it is changed in place.
func LabelErrors(vocabulary gogen.Vocabulary, name string, declaration *ast.FuncDecl) {
	if declaration == nil || declaration.Body == nil {
		return
	}
	labeller := &errorLabeller{first: nil, label: vocabulary.Message(name), vocabulary: vocabulary, results: 0, endsInError: false}
	if results := declaration.Type.Results; results != nil && len(results.List) > 0 {
		labeller.results = results.NumFields()
		labeller.first = results.List[0].Type
		last, isIdent := results.List[len(results.List)-1].Type.(*ast.Ident)
		labeller.endsInError = isIdent && last.Name == goast.IdentError
	}
	labeller.visit(declaration.Body, false)
}
