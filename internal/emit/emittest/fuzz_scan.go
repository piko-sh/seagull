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

package emittest

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
)

// fuzzScanOrderProblems checks flat model rows against their declared field order.
//
// Takes checked (*checkedPackage) which supplies resolved field selections.
//
// Returns []string which describes reordered or repeated scan destinations.
func fuzzScanOrderProblems(checked *checkedPackage) []string {
	var problems []string
	buffers := fuzzScanBuffers(checked)
	for _, file := range checked.files {
		for node := range ast.Preorder(file) {
			call, ok := node.(*ast.CallExpr)
			if ok {
				problems = append(problems, fuzzScanCallProblems(checked, buffers, call)...)
			}
		}
	}
	return problems
}

// fuzzScanCallProblems checks the ordered destinations of one Scan call.
//
// Takes checked (*checkedPackage) which resolves the selected fields.
// Takes buffers (map[types.Object][]ast.Expr) which holds the package's scan buffers.
// Takes call (*ast.CallExpr) which may invoke a row scanner.
//
// Returns []string which describes destinations out of field order.
func fuzzScanCallProblems(checked *checkedPackage, buffers map[types.Object][]ast.Expr, call *ast.CallExpr) []string {
	method, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || method.Sel.Name != "Scan" {
		return nil
	}
	var problems []string
	arguments := fuzzScanArguments(buffers, checked, call)
	for index, argument := range arguments {
		selection := fuzzScanField(checked, argument)
		if selection == nil {
			continue
		}
		_, structure, local := checked.packageStruct(dereference(selection.Recv()))
		if local && structure.NumFields() == len(arguments) && selection.Index()[0] != index {
			problems = append(problems, fmt.Sprintf("%s: Scan argument %d uses field %s at position %d",
				checked.position(call), index, selection.Obj().Name(), selection.Index()[0]))
		}
	}
	return problems
}

// fuzzScanField resolves an addressed field in a flat model row.
//
// Takes checked (*checkedPackage) which supplies resolved field selections.
// Takes argument (ast.Expr) which is a potential scan destination.
//
// Returns *types.Selection which is nil for scalar or nested destinations.
func fuzzScanField(checked *checkedPackage, argument ast.Expr) *types.Selection {
	if call, ok := ast.Unparen(argument).(*ast.CallExpr); ok {
		var found *types.Selection
		for _, input := range call.Args {
			selection := fuzzScanField(checked, input)
			if selection == nil {
				continue
			}
			if found != nil {
				return nil
			}
			found = selection
		}
		return found
	}
	address, ok := ast.Unparen(argument).(*ast.UnaryExpr)
	if !ok || address.Op != token.AND {
		return nil
	}
	field, ok := ast.Unparen(address.X).(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	selection := checked.info.Selections[field]
	if selection == nil || selection.Kind() != types.FieldVal || len(selection.Index()) != 1 {
		return nil
	}
	return selection
}
