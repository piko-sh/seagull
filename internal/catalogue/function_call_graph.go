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
	"maps"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// signatureIndex resolves the names function bodies call to the overloads a call can
// reach: the catalogue's own functions, keyed as the catalogue keys them, ahead of the
// dialect's built-in functions, keyed by their lower-case names.
type signatureIndex struct {
	// catalogue supplies the identifier rules the catalogue keys its functions by.
	catalogue *sqlmodel.Catalogue

	// created maps the key of each created function's name, and of that name qualified by
	// its schema, to the overloads of every schema filed under it.
	created map[string][]*sqlmodel.Function

	// builtins maps the lower-case name of each built-in function, and of that name
	// qualified by its schema, to its overloads.
	builtins map[string][]*sqlmodel.Function
}

// lookup returns the overloads a called name reaches: the created functions its key
// names, or else the built-in functions of its lower-case name.
//
// Takes calledName (string) which is the name as the body calls it, optionally qualified
// by a schema.
//
// Returns []*sqlmodel.Function which holds the overloads.
// Returns bool which is false when the name reaches no function.
func (index signatureIndex) lookup(calledName string) ([]*sqlmodel.Function, bool) {
	if overloads, found := index.created[index.catalogue.Key(calledName)]; found {
		return overloads, true
	}
	overloads, found := index.builtins[strings.ToLower(calledName)]
	return overloads, found
}

// propagateDataAccess walks the function call graph to propagate data access levels from
// callees to callers. A function that calls any non-read-only function becomes
// data-modifying, and that promotion cascades to every caller transitively.
//
// The propagation is a worklist traversal over predecessor edges so its cost is roughly
// linear in the number of functions plus call edges, rather than re-scanning every
// signature on each pass until a fixed point settles.
//
// A called name reaches the catalogue's functions filed under its key, as the catalogue's
// identifier rules give it, so a call of PostgreSQL's quoted "Foo" and one of unquoted
// foo reach different functions. A name the catalogue does not hold is looked up among
// the dialect's built-in functions, ignoring case, so a body calling lower, trim or count
// stays read-only while one calling a built-in that writes, such as a sequence's nextval,
// is promoted. A name neither holds is unknown and promotes its caller, since nothing
// says it does not write.
//
// A table-valued function a body reads as a FROM item promotes its caller when the
// catalogue or the built-in functions hold it and it writes, as a FROM item calling a
// function that writes an audit row does. One neither holds is left out, since a dialect
// reads external data through table functions its built-in catalogue does not list, such
// as DuckDB's read_csv.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the schema state with function
// signatures to update.
// Takes calledFunctions (map[*sqlmodel.Function][]string) which records the names each
// function's body calls.
// Takes calledTableFunctions (map[*sqlmodel.Function][]string) which records the names of
// the table-valued functions each analysed body reads as FROM items.
// Takes builtins (*ast.FunctionCatalogue) which holds the dialect's built-in functions;
// nil holds none.
func propagateDataAccess(
	catalogue *sqlmodel.Catalogue,
	calledFunctions map[*sqlmodel.Function][]string,
	calledTableFunctions map[*sqlmodel.Function][]string,
	builtins *ast.FunctionCatalogue,
) {
	signatures := collectAllSignatures(catalogue)
	index := signatureIndex{
		catalogue: catalogue,
		created:   buildSignatureIndex(catalogue),
		builtins:  builtinSignatureIndex(builtins),
	}

	callers := buildCallersMap(signatures, calledFunctions)
	addTableCallers(callers, signatures, calledTableFunctions, index)
	indexNames := buildIndexNamesMap(callers, index)

	drainDataAccessWorklist(callers, indexNames, index)
}

// builtinSignatureIndex files the dialect's built-in functions under their lower-case
// names, and under those names qualified by the schema a built-in declares.
//
// Takes builtins (*ast.FunctionCatalogue) which holds the built-in functions; nil holds
// none.
//
// Returns map[string][]*sqlmodel.Function which maps each name to its overloads.
func builtinSignatureIndex(builtins *ast.FunctionCatalogue) map[string][]*sqlmodel.Function {
	index := make(map[string][]*sqlmodel.Function)
	if builtins == nil {
		return index
	}
	for name, overloads := range builtins.Functions {
		key := strings.ToLower(name)
		if _, defined := index[key]; !defined {
			index[key] = overloads
		}
		for _, overload := range overloads {
			if overload == nil || overload.Schema == "" {
				continue
			}
			qualifiedKey := strings.ToLower(overload.Schema) + "." + key
			if _, defined := index[qualifiedKey]; !defined {
				index[qualifiedKey] = overloads
			}
		}
	}
	return index
}

// buildCallersMap maps a called function name (as it appears in calledFunctions) to the
// signatures that invoke it, giving the predecessor edges the worklist propagates over.
//
// Takes signatures ([]*sqlmodel.Function) which holds every signature to scan for
// outgoing call edges.
// Takes calledFunctions (map[*sqlmodel.Function][]string) which records the names each
// function's body calls.
//
// Returns map[string][]*sqlmodel.Function keyed by called name.
func buildCallersMap(
	signatures []*sqlmodel.Function,
	calledFunctions map[*sqlmodel.Function][]string,
) map[string][]*sqlmodel.Function {
	callers := make(map[string][]*sqlmodel.Function)
	for _, signature := range signatures {
		for _, calledName := range calledFunctions[signature] {
			callers[calledName] = append(callers[calledName], signature)
		}
	}
	return callers
}

// addTableCallers adds the predecessor edges from each table-valued function a body reads
// to the signature whose body reads it, for the names the signature index holds.
//
// Takes callers (map[string][]*sqlmodel.Function) which holds the edges to extend.
// Takes signatures ([]*sqlmodel.Function) which holds every signature.
// Takes calledTableFunctions (map[*sqlmodel.Function][]string) which records the
// table-valued functions each body reads.
// Takes index (signatureIndex) which says which names are known.
func addTableCallers(
	callers map[string][]*sqlmodel.Function,
	signatures []*sqlmodel.Function,
	calledTableFunctions map[*sqlmodel.Function][]string,
	index signatureIndex,
) {
	for _, signature := range signatures {
		for _, calledName := range calledTableFunctions[signature] {
			if _, known := index.lookup(calledName); known {
				callers[calledName] = append(callers[calledName], signature)
			}
		}
	}
}

// buildIndexNamesMap maps each signature to the called names that reach it.
//
// Promoting a signature can therefore re-enqueue exactly the names its callers resolve
// through. The map is derived from the same lookup the callers resolve their callees
// through, so the two stay in step.
//
// Takes callers (map[string][]*sqlmodel.Function) which holds the called names.
// Takes index (signatureIndex) which resolves a called name to its overloads.
//
// Returns map[*sqlmodel.Function][]string keyed by signature.
func buildIndexNamesMap(
	callers map[string][]*sqlmodel.Function,
	index signatureIndex,
) map[*sqlmodel.Function][]string {
	indexNames := make(map[*sqlmodel.Function][]string)
	for _, calledName := range slices.Sorted(maps.Keys(callers)) {
		overloads, _ := index.lookup(calledName)
		for _, overload := range overloads {
			indexNames[overload] = append(indexNames[overload], calledName)
		}
	}
	return indexNames
}

// drainDataAccessWorklist runs the worklist traversal that promotes every signature
// reachable from an infectious (non-read-only) callee to data-modifying.
//
// It seeds the queue with every name that is already infectious so the initial wave of
// promotions covers the whole fixed-point loop, then cascades each promotion to the names
// the signature answers to.
//
// Takes callers (map[string][]*sqlmodel.Function) which holds the predecessor edges from
// a called name to the signatures that invoke it.
// Takes indexNames (map[*sqlmodel.Function][]string) which maps a signature to the names
// it is reached through.
// Takes index (signatureIndex) which resolves a called name to its overloads.
func drainDataAccessWorklist(
	callers map[string][]*sqlmodel.Function,
	indexNames map[*sqlmodel.Function][]string,
	index signatureIndex,
) {
	var queue []string
	queued := make(map[string]struct{})
	enqueue := func(name string) {
		if _, alreadyQueued := queued[name]; alreadyQueued {
			return
		}
		queued[name] = struct{}{}
		queue = append(queue, name)
	}

	for calledName := range callers {
		if resolveCalledAccess(calledName, index) != sqlmodel.DataAccessReadOnly {
			enqueue(calledName)
		}
	}

	for len(queue) > 0 {
		calledName := queue[0]
		queue = queue[1:]
		delete(queued, calledName)
		promoteCallersOf(calledName, callers, indexNames, index, enqueue)
	}
}

// promoteCallersOf marks every not-yet-modifying caller of calledName as data-modifying
// and re-enqueues the names each newly promoted signature answers to, so the worklist
// revisits the signatures that call it in turn.
//
// Takes calledName (string) which is the infectious callee whose callers are being
// promoted.
// Takes callers (map[string][]*sqlmodel.Function) which holds the predecessor edges from
// a called name to the signatures that invoke it.
// Takes indexNames (map[*sqlmodel.Function][]string) which maps a signature to the names
// it is reached through.
// Takes index (signatureIndex) which resolves a called name to its overloads.
// Takes enqueue (func(string)) which queues a name for re-examination, skipping
// duplicates.
func promoteCallersOf(
	calledName string,
	callers map[string][]*sqlmodel.Function,
	indexNames map[*sqlmodel.Function][]string,
	index signatureIndex,
	enqueue func(string),
) {
	for _, signature := range callers[calledName] {
		if signature.DataAccess == sqlmodel.DataAccessModifiesData {
			continue
		}
		signature.DataAccess = sqlmodel.DataAccessModifiesData

		for _, name := range indexNames[signature] {
			if resolveCalledAccess(name, index) != sqlmodel.DataAccessReadOnly {
				enqueue(name)
			}
		}
	}
}

// collectAllSignatures gathers every function signature from all schemas in the
// catalogue.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the schema state.
//
// Returns []*sqlmodel.Function which contains all function signatures across all schemas.
func collectAllSignatures(catalogue *sqlmodel.Catalogue) []*sqlmodel.Function {
	var result []*sqlmodel.Function
	for _, schema := range catalogue.Schemas {
		for _, overloads := range schema.Functions {
			result = append(result, overloads...)
		}
	}
	return result
}

// buildSignatureIndex builds a lookup map from the keys of the catalogue's function names
// to their overloads, filing each overload under its name's key and under that key
// qualified by the key of its schema.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the schema state.
//
// Returns map[string][]*sqlmodel.Function which maps each key to its overloads.
func buildSignatureIndex(catalogue *sqlmodel.Catalogue) map[string][]*sqlmodel.Function {
	index := make(map[string][]*sqlmodel.Function)
	for _, schema := range catalogue.Schemas {
		for name, overloads := range schema.Functions {
			key := catalogue.Key(name)
			index[key] = append(index[key], overloads...)
			for _, overload := range overloads {
				if overload.Schema != "" {
					qualifiedKey := catalogue.Key(overload.Schema) + "." + key
					index[qualifiedKey] = append(index[qualifiedKey], overload)
				}
			}
		}
	}
	return index
}

// resolveCalledAccess looks up the data access level for a called function name in the
// signature index.
//
// Takes calledName (string) which is the function name as the body calls it.
// Takes index (signatureIndex) which resolves the name to its overloads.
//
// Returns sqlmodel.FunctionDataAccess which is the resolved data access level.
func resolveCalledAccess(
	calledName string,
	index signatureIndex,
) sqlmodel.FunctionDataAccess {
	overloads, exists := index.lookup(calledName)
	if !exists {
		return sqlmodel.DataAccessUnknown
	}

	for _, overload := range overloads {
		if overload.DataAccess != sqlmodel.DataAccessReadOnly {
			return overload.DataAccess
		}
	}
	return sqlmodel.DataAccessReadOnly
}
