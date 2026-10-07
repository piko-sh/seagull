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
	"go/types"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"seagull.sh/seagull/internal/testkit"
)

type typeUse int

const (
	usedBuilt typeUse = iota
	usedRead
)

var facadePatterns = []string{modulePath, modulePath + "/sdk/..."}

type facadeClosure struct {
	named   map[*types.TypeName]bool
	walked  map[*types.TypeName]map[[2]bool]bool
	missing map[string]string
}

func TestEveryTypeACallerMustNameHasAFacadeName(t *testing.T) {
	loaded, err := packages.Load(&packages.Config{
		Mode:    packages.NeedName | packages.NeedTypes,
		Context: t.Context(),
		Dir:     ".",
		Env:     testkit.ToolchainEnvironment(),
		Tests:   false,
	}, facadePatterns...)
	require.NoError(t, err)
	require.Zero(t, packages.PrintErrors(loaded), "the facades must type-check")

	closure := newFacadeClosure(loaded)
	for _, loadedPackage := range loaded {
		closure.walkFacade(loadedPackage.Types)
	}

	problems := make([]string, 0, len(closure.missing))
	for _, typeName := range slices.Sorted(maps.Keys(closure.missing)) {
		problems = append(problems, typeName+", reached by "+closure.missing[typeName])
	}
	require.Empty(t, problems, "a caller must name these types, and no facade names them")
}

func newFacadeClosure(loaded []*packages.Package) *facadeClosure {
	closure := &facadeClosure{
		named:   map[*types.TypeName]bool{},
		walked:  map[*types.TypeName]map[[2]bool]bool{},
		missing: map[string]string{},
	}
	for _, loadedPackage := range loaded {
		scope := loadedPackage.Types.Scope()
		for _, name := range scope.Names() {
			typeName, isType := scope.Lookup(name).(*types.TypeName)
			if !isType || !typeName.Exported() {
				continue
			}
			closure.named[typeName] = true
			if target, isNamed := types.Unalias(typeName.Type()).(*types.Named); isNamed {
				closure.named[target.Origin().Obj()] = true
			}
		}
	}
	return closure
}

func (closure *facadeClosure) walkFacade(facade *types.Package) {
	scope := facade.Scope()
	for _, name := range scope.Names() {
		object := scope.Lookup(name)
		if !object.Exported() {
			continue
		}
		path := facade.Name() + "." + name
		switch typed := object.(type) {
		case *types.Func:
			closure.walkSignature(typed.Signature(), usedRead, true, path)
		case *types.TypeName:
			if _, isPort := typed.Type().Underlying().(*types.Interface); isPort {
				closure.walk(typed.Type(), usedBuilt, true, path)
			}
			closure.walk(typed.Type(), usedRead, true, path)
		default:
			closure.walk(object.Type(), usedRead, true, path)
		}
	}
}

func (closure *facadeClosure) walk(typ types.Type, use typeUse, mustName bool, path string) {
	switch typed := typ.(type) {
	case *types.Alias:
		closure.walk(types.Unalias(typed), use, mustName, path)
	case *types.Named:
		closure.walkNamed(typed, use, mustName, path)
	case *types.Pointer:
		closure.walk(typed.Elem(), use, mustName, path)
	case *types.Slice:
		closure.walk(typed.Elem(), use, mustName, path+"[]")
	case *types.Array:
		closure.walk(typed.Elem(), use, mustName, path+"[]")
	case *types.Map:
		closure.walk(typed.Key(), use, mustName, path+"[key]")
		closure.walk(typed.Elem(), use, mustName, path+"[value]")
	case *types.Chan:
		closure.walk(typed.Elem(), use, mustName, path)
	case *types.Signature:
		closure.walkSignature(typed, use, mustName, path)
	case *types.Struct:
		for field := range typed.Fields() {
			if field.Exported() {
				closure.walk(field.Type(), use, mustName, path+"."+field.Name())
			}
		}
	case *types.Interface:
		for method := range typed.Methods() {
			if method.Exported() {
				closure.walkSignature(method.Signature(), use, use == usedBuilt || mustName, path+"."+method.Name())
			}
		}
	}
}

func (closure *facadeClosure) walkNamed(named *types.Named, use typeUse, mustName bool, path string) {
	for argument := range named.TypeArgs().Types() {
		closure.walk(argument, use, mustName, path+"[type argument]")
	}
	declaration := named.Origin().Obj()
	if declaration.Pkg() == nil || !strings.HasPrefix(declaration.Pkg().Path(), modulePath) || !declaration.Exported() {
		return
	}
	if mustName && !closure.named[declaration] {
		key := declaration.Pkg().Path() + "." + declaration.Name()
		if _, recorded := closure.missing[key]; !recorded {
			closure.missing[key] = path
		}
	}
	nameable := mustName || closure.named[declaration]
	state := [2]bool{use == usedBuilt, nameable}
	if closure.walked[declaration] == nil {
		closure.walked[declaration] = map[[2]bool]bool{}
	}
	if closure.walked[declaration][state] {
		return
	}
	closure.walked[declaration][state] = true

	name := declaration.Name()
	switch underlying := named.Origin().Underlying().(type) {
	case *types.Struct:
		for field := range underlying.Fields() {
			if !field.Exported() {
				continue
			}
			_, writesFunction := field.Type().Underlying().(*types.Signature)
			closure.walk(field.Type(), use, use == usedBuilt && writesFunction, name+"."+field.Name())
		}
	case *types.Interface:
		closure.walk(underlying, use, nameable, name)
		return
	case *types.Signature:
		closure.walkSignature(underlying, use, use == usedBuilt || nameable, name)
	}
	for method := range named.Origin().Methods() {
		if !method.Exported() {
			continue
		}
		signature := method.Signature()
		for parameter := range signature.Params().Variables() {
			closure.walk(parameter.Type(), usedBuilt, nameable, name+"."+method.Name()+"()")
		}
		for result := range signature.Results().Variables() {
			closure.walk(result.Type(), usedRead, false, name+"."+method.Name()+"()")
		}
	}
}

func (closure *facadeClosure) walkSignature(signature *types.Signature, use typeUse, mustName bool, path string) {
	if use == usedBuilt {
		for parameter := range signature.Params().Variables() {
			closure.walk(parameter.Type(), usedRead, true, path+"(parameter "+parameter.Name()+")")
		}
		for result := range signature.Results().Variables() {
			closure.walk(result.Type(), usedBuilt, true, path+"(result)")
		}
		return
	}
	for parameter := range signature.Params().Variables() {
		closure.walk(parameter.Type(), usedBuilt, mustName, path+"(parameter "+parameter.Name()+")")
	}
	for result := range signature.Results().Variables() {
		closure.walk(result.Type(), usedRead, mustName, path+"(result)")
	}
}
