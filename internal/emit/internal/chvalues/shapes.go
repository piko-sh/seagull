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

package chvalues

import (
	"fmt"
	"go/ast"
	"strconv"
	"strings"

	"piko.sh/goastutil"

	shapesource "seagull.sh/seagull/internal/emit/internal/chvalues/source"
	"seagull.sh/seagull/internal/gogen/helpers"
	"seagull.sh/seagull/internal/sqlmodel"
)

// Shapes collects immutable descriptors for one emitted package. Its zero value is ready
// to use; descriptor order follows the emitter's deterministic query order.
type Shapes struct {
	// err retains the first descriptor parsing failure.
	err error

	// indices maps encoded shapes to their descriptor positions.
	indices map[string]int

	// descriptions holds compiled shapes in first-use order.
	descriptions []shapesource.ShapeDescription
}

// Bind binds a named parameter using a descriptor compiled into the generated package.
//
// Takes vocabulary (helpers.Vocabulary) which names the generated helper identifiers.
// Takes access (ast.Expr) which reads the parameter value.
// Takes parameter (*sqlmodel.QueryParameter) which describes the parameter's SQL type.
//
// Returns ast.Expr which calls the generated value formatter with a compiled descriptor.
func (shapes *Shapes) Bind(vocabulary helpers.Vocabulary, access ast.Expr, parameter *sqlmodel.QueryParameter) ast.Expr {
	return goastutil.CallExpr(goastutil.CachedIdent(vocabulary.Helper("ClickHouseShapedValue")),
		access, shapes.reference(vocabulary, ParameterShape(parameter)))
}

// AppendRow checks and converts a native row using its generated descriptor.
//
// Takes vocabulary (helpers.Vocabulary) which names the generated helper identifiers.
// Takes appendRow (ast.Expr) which is the native batch append method.
// Takes parameters ([]*sqlmodel.QueryParameter) which describes each row value in order.
// Takes values ([]ast.Expr) which reads the row values in append order.
//
// Returns ast.Expr which calls the generated row adapter with its compiled descriptor.
func (shapes *Shapes) AppendRow(vocabulary helpers.Vocabulary, appendRow ast.Expr, parameters []*sqlmodel.QueryParameter, values []ast.Expr) ast.Expr {
	shape := rowShape(parameters)
	arguments := append([]ast.Expr{appendRow, shapes.reference(vocabulary, shape), goastutil.StrLit(shape)}, values...)
	return goastutil.CallExpr(goastutil.CachedIdent(vocabulary.Helper("AppendNativeShapedRow")), arguments...)
}

// reference interns a shape and returns the expression that reads its descriptor.
//
// Takes vocabulary (helpers.Vocabulary) which names the generated helper identifiers.
// Takes text (string) which is the encoded shape to intern.
//
// Returns ast.Expr which indexes the package's compiled shape table.
func (shapes *Shapes) reference(vocabulary helpers.Vocabulary, text string) ast.Expr {
	if shapes.indices == nil {
		shapes.indices = make(map[string]int)
	}
	index, found := shapes.indices[text]
	if !found {
		description, err := shapesource.DescribeShape(text)
		if err != nil && shapes.err == nil {
			shapes.err = fmt.Errorf("compiling ClickHouse shape: %w", err)
		}
		index = len(shapes.descriptions)
		shapes.indices[text] = index
		shapes.descriptions = append(shapes.descriptions, description)
	}
	return &ast.IndexExpr{X: goastutil.CachedIdent(vocabulary.Helper("ClickHouseShapes")), Index: goastutil.IntLit(index)}
}

// code writes the descriptors as composite literals; none are parsed at runtime.
//
// Takes vocabulary (helpers.Vocabulary) which names the generated helper identifiers.
//
// Returns helpers.Code which declares the collected shape descriptors.
// Returns error when a collected shape could not be parsed.
func (shapes *Shapes) code(vocabulary helpers.Vocabulary) (helpers.Code, error) {
	if shapes != nil && shapes.err != nil {
		return helpers.Code{}, shapes.err
	}
	var text strings.Builder
	text.WriteString("[]" + vocabulary.Helper("ClickHouseShape") + "{")
	if shapes != nil {
		for _, description := range shapes.descriptions {
			writeShape(&text, description, vocabulary)
			text.WriteByte(',')
		}
	}
	text.WriteByte('}')
	return helpers.Code{Text: text.String(), Imports: nil}, nil
}

// writeShape appends a descriptor and its children as Go composite literals.
//
// Takes text (*strings.Builder) which receives the Go composite literal.
// Takes shape (shapesource.ShapeDescription) which is the compiled descriptor to write.
// Takes vocabulary (helpers.Vocabulary) which names the generated helper identifiers.
func writeShape(text *strings.Builder, shape shapesource.ShapeDescription, vocabulary helpers.Vocabulary) {
	fmt.Fprintf(text, "{kind:%d,digits:%d,prepared:true,checked:%t,driverMap:%t,names:[]string{", shape.Kind, shape.Digits, shape.Checked, shape.DriverMap)
	for _, name := range shape.Names {
		text.WriteString(strconv.Quote(name))
		text.WriteByte(',')
	}
	text.WriteString("},elements:[]" + vocabulary.Helper("ClickHouseShape") + "{")
	for _, element := range shape.Elements {
		writeShape(text, element, vocabulary)
		text.WriteByte(',')
	}
	text.WriteString("}}")
}
