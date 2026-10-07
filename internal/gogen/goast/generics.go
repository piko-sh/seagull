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

package goast

import (
	"strings"

	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// typeArgumentsOpen opens the type arguments of an instance of a generic type.
	typeArgumentsOpen = "["

	// typeArgumentsClose closes the type arguments of an instance of a generic type.
	typeArgumentsClose = "]"

	// typeArgumentSeparator separates the type arguments of an instance.
	typeArgumentSeparator = ','
)

// WrittenGoType parses a package path and type name, including generic arguments in
// go_type syntax. Pointer and slice prefixes are preserved.
//
// Takes packagePath (string) which is the import path of the type's package, or empty.
// Takes name (string) which is the type's name as written.
//
// Returns sqlmodel.GoType which is the named type, or the instance.
func WrittenGoType(packagePath, name string) sqlmodel.GoType {
	markers, base := CutTypeMarkers(name)
	generic, arguments, instance := CutTypeArguments(base)
	if !instance {
		return sqlmodel.GoType{Composite: nil, Package: packagePath, Name: name}
	}
	parsed := make([]sqlmodel.GoType, 0, len(arguments))
	for _, argument := range arguments {
		parsed = append(parsed, WrittenGoType(SplitWrittenType(argument)))
	}
	written := sqlmodel.NewGoInstance(sqlmodel.GoType{Composite: nil, Package: packagePath, Name: generic}, parsed...)
	written.Name = markers
	return written
}

// SplitWrittenType splits a whole type, written as a go_type value writes one, into the
// import path of its package and its name: the path is what comes before the last dot
// outside the type arguments, so github.com/x/opt.Option[example.com/y.Z] names package
// github.com/x/opt and type Option[example.com/y.Z].
//
// Takes written (string) which is the type as written.
//
// Returns packagePath (string) which is the import path, or empty for an unqualified
// type.
// Returns name (string) which is the name, with any markers and type arguments.
func SplitWrittenType(written string) (packagePath, name string) {
	written = strings.TrimSpace(written)
	markers, base := CutTypeMarkers(written)
	head, arguments := cutArgumentsSuffix(base)
	packagePath, typeName, qualified := strings.CutLast(head, ".")
	if !qualified {
		return "", written
	}
	return packagePath, markers + typeName + arguments
}

// CutTypeMarkers splits the leading pointer and slice markers from a type's name.
//
// Takes name (string) which is the name as written.
//
// Returns markers (string) which holds the markers, in order.
// Returns rest (string) which is the rest of the name.
func CutTypeMarkers(name string) (markers string, rest string) {
	rest = name
	for {
		switch {
		case strings.HasPrefix(rest, "*"):
			rest = rest[1:]
		case strings.HasPrefix(rest, "[]"):
			rest = rest[2:]
		default:
			return name[:len(name)-len(rest)], rest
		}
	}
}

// CutTypeArguments splits a type's name, without markers, into the name of the generic
// type and its type arguments, as written, when it has type arguments in brackets.
//
// Takes name (string) which is the name, such as Pair[string, example.com/y.Z].
//
// Returns generic (string) which is the name before the brackets.
// Returns arguments ([]string) which holds each argument, trimmed.
// Returns instance (bool) which is false when the name has no well-formed brackets.
func CutTypeArguments(name string) (generic string, arguments []string, instance bool) {
	head, suffix := cutArgumentsSuffix(name)
	if suffix == "" || head == "" {
		return name, nil, false
	}
	inside := suffix[len(typeArgumentsOpen) : len(suffix)-len(typeArgumentsClose)]
	depth, start := 0, 0
	for index, character := range inside {
		switch {
		case strings.ContainsRune(typeArgumentsOpen, character):
			depth++
		case strings.ContainsRune(typeArgumentsClose, character):
			depth--
		case character == typeArgumentSeparator && depth == 0:
			arguments = append(arguments, strings.TrimSpace(inside[start:index]))
			start = index + 1
		}
		if depth < 0 {
			return name, nil, false
		}
	}
	if depth != 0 {
		return name, nil, false
	}
	return head, append(arguments, strings.TrimSpace(inside[start:])), true
}

// cutArgumentsSuffix splits a name, without markers, at the bracket that opens its type
// arguments: the first one, when the name ends with the bracket that closes them.
//
// Takes name (string) which is the name.
//
// Returns head (string) which is the name before the brackets, or the whole name.
// Returns suffix (string) which is the bracketed arguments, or empty when there are none.
func cutArgumentsSuffix(name string) (head, suffix string) {
	open := strings.Index(name, typeArgumentsOpen)
	if open < 0 || !strings.HasSuffix(name, typeArgumentsClose) {
		return name, ""
	}
	return name[:open], name[open:]
}
