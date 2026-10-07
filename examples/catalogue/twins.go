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
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"go.yaml.in/yaml/v3"
)

const (
	// configFile is the sgl configuration in every variant directory.
	configFile = "sgl.yaml"

	// configPermissions is the mode of the sgl.yaml a twin check rewrites.
	configPermissions = 0o644

	// wrapFormat prefixes an error with what it concerns.
	wrapFormat = "%s: %w"
)

// Equivalent is a cell that has no directory of its own because its engine's generated
// code is the same as another column's variant.
type Equivalent struct {
	// Concept is the cell's concept.
	Concept *Concept

	// Column is the cell's column, such as cockroachdb.
	Column Column

	// Twin is the column whose variant the cell reuses, such as postgres.
	Twin Column

	// TwinDirectory is the twin variant's directory, relative to the examples directory,
	// such as postgres/002_commands.
	TwinDirectory string

	// Dialect is the sgl.yaml dialect of the cell's engine.
	Dialect string
}

// String names an equivalent cell for messages.
//
// Returns string which is the concept, the cell's column and the twin's directory.
func (e Equivalent) String() string {
	return strconv.Itoa(e.Concept.Number) + " " + e.Concept.Slug + " (" + e.Column.ID + ", as " + e.TwinDirectory + ")"
}

// Module returns the examples module that holds the twin variant, such as postgres.
//
// Returns string which is the module's directory, relative to the examples directory.
func (e Equivalent) Module() string {
	return filepath.Dir(e.TwinDirectory)
}

// Package returns the twin variant's package path within its module, such as
// ./002_commands, ready for go test.
//
// Returns string which is the relative package path.
func (e Equivalent) Package() string {
	return "./" + filepath.Base(e.TwinDirectory)
}

// TwinResult is the outcome of checking one equivalent cell.
type TwinResult struct {
	// Output is what sgl printed, standard output followed by standard error.
	Output string

	// Cell is the equivalent cell checked.
	Cell Equivalent

	// Same is true when sgl generated the twin's committed code unchanged under the cell's
	// dialect.
	Same bool
}

// Equivalents lists every equivalent cell in learning order, then in column order. A cell
// whose twin column is unknown is left out; Validate reports it.
//
// Returns []Equivalent which holds the cells.
func (c *Curriculum) Equivalents() []Equivalent {
	var cells []Equivalent
	for index := range c.Concepts {
		concept := &c.Concepts[index]
		for _, column := range c.Columns {
			cell, ok := concept.Cells[column.ID]
			if !ok || cell.Status != StatusEquivalent {
				continue
			}
			twin, known := c.Column(cell.Twin)
			if !known {
				continue
			}
			cells = append(cells, Equivalent{
				Concept:       concept,
				Column:        column,
				Twin:          twin,
				TwinDirectory: c.VariantDirectory(concept, twin),
				Dialect:       c.Engines[column.Engine].Dialect,
			})
		}
	}
	return cells
}

// EquivalentsFor lists the equivalent cells of one engine, in learning order.
//
// Takes engine (string) which is the engine's name, such as cockroachdb.
//
// Returns []Equivalent which holds the engine's equivalent cells.
func (c *Curriculum) EquivalentsFor(engine string) []Equivalent {
	cells := c.Equivalents()
	return slices.DeleteFunc(cells, func(cell Equivalent) bool {
		return cell.Column.Engine != engine
	})
}

// RewriteDialect returns an sgl.yaml with every database's dialect set to dialect,
// leaving everything else, dialect_options included, as it was.
//
// Takes data ([]byte) which is the sgl.yaml.
// Takes dialect (string) which is the dialect to set.
//
// Returns []byte which is the rewritten sgl.yaml.
// Returns error when the file cannot be parsed, holds other than one YAML document, or
// has a database that names no dialect or no database at all.
func RewriteDialect(data []byte, dialect string) ([]byte, error) {
	var document yaml.Node
	if err := decodeDocument(data, &document); err != nil {
		return nil, fmt.Errorf(wrapFormat, configFile, err)
	}
	if document.Kind != yaml.DocumentNode || len(document.Content) == 0 {
		return nil, fmt.Errorf("%s: empty document", configFile)
	}
	databases := mappingValue(document.Content[0], "databases")
	if databases == nil || databases.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("%s: no databases list", configFile)
	}
	if len(databases.Content) == 0 {
		return nil, fmt.Errorf("%s: no database names a dialect", configFile)
	}
	for index, database := range databases.Content {
		value := mappingValue(database, "dialect")
		if value == nil || value.Kind != yaml.ScalarNode {
			return nil, fmt.Errorf("%s: database %d names no dialect", configFile, index+1)
		}
		value.Value = dialect
		value.Style = 0
	}
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(&document); err != nil {
		return nil, fmt.Errorf(wrapFormat, configFile, err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf(wrapFormat, configFile, err)
	}
	return out.Bytes(), nil
}

// CheckTwins checks every equivalent cell's claim: it copies the twin variant to a
// directory under scratch, sets sgl.yaml's dialect to the cell's engine, and runs the sgl
// binary's generate --check there, so the claim holds only when the committed code is
// what that dialect generates too. The check stops before the next cell once the context
// is done.
//
// Takes examplesDirectory (string) which is the examples directory.
// Takes scratch (string) which is an empty or absent directory the check copies the twins
// into; the caller removes it.
// Takes sgl (string) which is the sgl binary to run.
// Takes extra ([]string) which is appended to generate --check, such as --diff.
// Takes run (CommandRunner) which runs each sgl, such as RunCommand.
//
// Returns []TwinResult which holds one result per equivalent cell, in learning order.
// Returns error when the curriculum cannot be read, scratch is not empty, a twin cannot
// be copied, sgl cannot run, or the context is done; a cell whose code differs is a
// result, not an error.
func CheckTwins(ctx context.Context, examplesDirectory, scratch, sgl string, extra []string, run CommandRunner) ([]TwinResult, error) {
	curriculum, err := Load(examplesDirectory)
	if err != nil {
		return nil, err
	}
	if err := requireEmpty(scratch); err != nil {
		return nil, err
	}

	arguments := slices.Concat([]string{sgl, "generate", "--check"}, extra)
	cells := curriculum.Equivalents()
	results := make([]TwinResult, 0, len(cells))
	for index := range cells {
		cell := &cells[index]
		if ctx.Err() != nil {
			return nil, fmt.Errorf(wrapFormat, cell, context.Cause(ctx))
		}
		directory := filepath.Join(scratch, strconv.Itoa(index))
		if err := prepareTwin(filepath.Join(examplesDirectory, cell.TwinDirectory), directory, cell.Dialect); err != nil {
			return nil, fmt.Errorf(wrapFormat, cell, err)
		}
		stdout, stderr, runErr := run(ctx, directory, arguments)
		if _, exited := exitStatus(runErr); runErr != nil && !exited {
			return nil, fmt.Errorf("%s: running %s: %w", cell, sgl, runErr)
		}
		results = append(results, TwinResult{Cell: *cell, Same: runErr == nil, Output: string(stdout) + string(stderr)})
	}
	return results, nil
}

// mappingValue returns the value node of key in a mapping node, or nil.
//
// Takes node (*yaml.Node) which should be a mapping.
// Takes key (string) which is the key to find.
//
// Returns *yaml.Node which is the value, or nil when node is not a mapping or has no such
// key.
func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value == key {
			return node.Content[index+1]
		}
	}
	return nil
}

// prepareTwin copies a twin variant into target and sets its sgl.yaml's dialect.
//
// Takes source (string) which is the twin variant's directory.
// Takes target (string) which receives the copy; it must not exist.
// Takes dialect (string) which is the dialect to set.
//
// Returns error when the copy or the rewrite fails.
func prepareTwin(source, target, dialect string) error {
	if err := os.CopyFS(target, os.DirFS(source)); err != nil {
		return err
	}
	root, err := os.OpenRoot(target)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	data, err := root.ReadFile(configFile)
	if err != nil {
		return err
	}
	rewritten, err := RewriteDialect(data, dialect)
	if err != nil {
		return err
	}
	return root.WriteFile(configFile, rewritten, configPermissions)
}
