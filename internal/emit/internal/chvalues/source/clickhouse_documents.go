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

package source

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
)

// sglClickHouseDocumentDepth bounds how deep the reading of a value looks for the JSON
// documents it holds.
const sglClickHouseDocumentDepth = 32

// sglClickHouseDocument represents a JSON document returned by clickhouse-go.
//
// It stands for the type clickhouse-go decodes a JSON value into, chcol.JSON, which the
// rendered file names in its place. The driver hands a *chcol.JSON for a JSON column and
// for a value of a Map or a named Tuple, and a chcol.JSON for an element of an Array or
// of a Tuple whose elements have no names.
//
//sgl:inline documentType
type sglClickHouseDocument struct {
	// text holds the stand-in document's JSON representation.
	text string
}

// MarshalJSON stands for chcol.JSON's own, which writes the document's paths as one
// object, keys sorted.
//
// Returns []byte which is the document's text.
// Returns error which is always nil.
//
//sgl:when standIns
func (document *sglClickHouseDocument) MarshalJSON() ([]byte, error) {
	return []byte(document.text), nil
}

// sglClickHouseJSON stands for the package's JSON type, the Go type the JSON slot maps a
// document to, which the rendered file names in its place. A document an interface value
// holds, such as an element of a Tuple read into a map[string]any, is read into it.
//
//sgl:inline jsonType
type sglClickHouseJSON []byte

// sglReadClickHouseDocuments reads one of the driver's values that is, or may hold, a
// JSON document into target, and reports false for a value that is neither, which the
// caller reads as it reads any other.
//
// A document reads as its text, the bytes json.Marshal writes for a *chcol.JSON, which
// are those clickhouse-go's database/sql bridge hands the JSON type of a JSON column: one
// object, keys sorted, numbers as Go writes them, and <, > and & escaped. The text reads
// into a type that unmarshals JSON, one that scans itself, as bytes, a []byte or string
// kind, an interface, as the package's JSON type, and any other type as json.Unmarshal
// decodes it. A value target holds as it is but whose interfaces may hold documents, such
// as the map[string]any of a Tuple, is copied with every document it holds read into the
// package's JSON type.
//
// Takes target (reflect.Value) which is the settable destination.
// Takes source (any) which is the driver's value.
//
// Returns bool which is false when the value is no document and holds none target keeps.
// Returns error when the document cannot be written or target cannot hold it.
func sglReadClickHouseDocuments(target reflect.Value, source any) (bool, error) {
	if text, isDocument, err := sglClickHouseDocumentText(source); isDocument {
		if err != nil {
			return true, err
		}
		return true, sglReadClickHouseText(target, text)
	}
	value := reflect.ValueOf(source)
	if source == nil || !value.Type().AssignableTo(target.Type()) || !sglClickHouseMayHold(value.Type(), 0) {
		return false, nil
	}
	loose, err := sglClickHouseLoose(value, 0)
	if err != nil {
		return true, err
	}
	target.Set(loose)
	return true, nil
}

// sglClickHouseDocumentText returns the text of a document, a non-nil pointer to one or
// one by value, as json.Marshal writes it.
//
// Takes source (any) which is the value.
//
// Returns []byte which is the document's text.
// Returns bool which is false when the value is no document.
// Returns error when the document cannot be written.
func sglClickHouseDocumentText(source any) ([]byte, bool, error) {
	switch typed := source.(type) {
	case *sglClickHouseDocument:
		if typed == nil {
			return nil, false, nil
		}
		text, err := json.Marshal(typed)
		return text, true, err
	case sglClickHouseDocument:
		text, err := json.Marshal(&typed)
		return text, true, err
	default:
		return nil, false, nil
	}
}

// sglReadClickHouseText reads a document's text into target, as
// sglReadClickHouseDocuments describes.
//
// Takes target (reflect.Value) which is the settable destination.
// Takes text ([]byte) which is the document's text.
//
// Returns error when target cannot hold the document.
func sglReadClickHouseText(target reflect.Value, text []byte) error {
	switch target.Kind() {
	case reflect.Pointer:
		element := reflect.New(target.Type().Elem())
		if err := sglReadClickHouseText(element.Elem(), text); err != nil {
			return err
		}
		target.Set(element)
		return nil
	case reflect.Interface:
		return sglReadClickHouseLooseText(target, text)
	default:
	}
	if unmarshaler, unmarshals := reflect.TypeAssert[json.Unmarshaler](target.Addr()); unmarshals {
		return unmarshaler.UnmarshalJSON(text)
	}
	if scanner, scans := reflect.TypeAssert[sql.Scanner](target.Addr()); scans {
		return scanner.Scan(bytes.Clone(text))
	}
	switch {
	case target.Kind() == reflect.Slice && target.Type().Elem().Kind() == reflect.Uint8:
		target.SetBytes(bytes.Clone(text))
		return nil
	case target.Kind() == reflect.String:
		target.SetString(string(text))
		return nil
	default:
		return json.Unmarshal(text, target.Addr().Interface())
	}
}

// sglReadClickHouseLooseText reads a document's text into an interface as the package's
// JSON type, or, when the JSON type is itself an interface, as json.Unmarshal decodes it.
//
// Takes target (reflect.Value) which is the settable interface.
// Takes text ([]byte) which is the document's text.
//
// Returns error when the document cannot be read or the interface cannot hold it.
func sglReadClickHouseLooseText(target reflect.Value, text []byte) error {
	jsonType := reflect.TypeFor[sglClickHouseJSON]()
	if jsonType.Kind() == reflect.Interface {
		return json.Unmarshal(text, target.Addr().Interface())
	}
	if !jsonType.AssignableTo(target.Type()) {
		return fmt.Errorf("cannot read a JSON document into %s", target.Type())
	}
	document := reflect.New(jsonType).Elem()
	if err := sglReadClickHouseText(document, text); err != nil {
		return err
	}
	target.Set(document)
	return nil
}

// sglClickHouseMayHold reports whether a value of a type may hold a document: the type is
// the document's, an interface, or a pointer to, a slice, an array or a map of such.
//
// Takes candidate (reflect.Type) which is the type.
// Takes depth (int) which is how deep the walk is, bounded so a recursive type ends it.
//
// Returns bool which is true when the value may hold a document.
func sglClickHouseMayHold(candidate reflect.Type, depth int) bool {
	switch {
	case depth > sglClickHouseDocumentDepth:
		return false
	case candidate == reflect.TypeFor[sglClickHouseDocument]():
		return true
	default:
	}
	switch candidate.Kind() {
	case reflect.Interface:
		return true
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
		return sglClickHouseMayHold(candidate.Elem(), depth+1)
	default:
		return false
	}
}

// sglClickHouseLooseType returns the type a value of a type is copied as: the package's
// JSON type for a document or a pointer to one, a slice, an array or a map of the copied
// type of its elements, when that differs, and the type itself otherwise.
//
// Takes candidate (reflect.Type) which is the type.
// Takes depth (int) which is how deep the walk is, bounded so a recursive type ends it.
//
// Returns reflect.Type which is the copied type.
func sglClickHouseLooseType(candidate reflect.Type, depth int) reflect.Type {
	document := reflect.TypeFor[sglClickHouseDocument]()
	switch {
	case depth > sglClickHouseDocumentDepth:
		return candidate
	case candidate == document || candidate == reflect.PointerTo(document):
		return reflect.TypeFor[sglClickHouseJSON]()
	default:
	}
	switch candidate.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
	default:
		return candidate
	}
	element := sglClickHouseLooseType(candidate.Elem(), depth+1)
	switch {
	case element == candidate.Elem():
		return candidate
	case candidate.Kind() == reflect.Slice:
		return reflect.SliceOf(element)
	case candidate.Kind() == reflect.Array:
		return reflect.ArrayOf(candidate.Len(), element)
	default:
		return reflect.MapOf(candidate.Key(), element)
	}
}

// sglClickHouseLoose copies a value with every document it holds read into the package's
// JSON type, as sglClickHouseLooseType types it; a value that may hold none is returned
// as it is. The driver's own value is never changed.
//
// Takes value (reflect.Value) which is the value.
// Takes depth (int) which is how deep the walk is, bounded so a recursive value ends it.
//
// Returns reflect.Value which is the copy.
// Returns error when a document cannot be read.
func sglClickHouseLoose(value reflect.Value, depth int) (reflect.Value, error) {
	if !sglClickHouseMayHold(value.Type(), depth) {
		return value, nil
	}
	switch value.Kind() {
	case reflect.Interface:
		return sglClickHouseLooseInterface(value, depth)
	case reflect.Slice, reflect.Array:
		return sglClickHouseLooseList(value, depth)
	case reflect.Map:
		return sglClickHouseLooseMap(value, depth)
	default:
		return sglClickHouseLooseDocument(value, depth)
	}
}

// sglClickHouseLooseInterface copies an interface value with the document it holds, or
// the documents its value holds, read into the package's JSON type.
//
// Takes value (reflect.Value) which is the interface value.
// Takes depth (int) which is how deep the walk is.
//
// Returns reflect.Value which is the copy, of the interface's type.
// Returns error when a document cannot be read.
func sglClickHouseLooseInterface(value reflect.Value, depth int) (reflect.Value, error) {
	if value.IsNil() {
		return value, nil
	}
	held, err := sglClickHouseLoose(value.Elem(), depth+1)
	if err != nil {
		return reflect.Value{}, err
	}
	copied := reflect.New(value.Type()).Elem()
	copied.Set(held)
	return copied, nil
}

// sglClickHouseLooseList copies a slice or an array element by element.
//
// Takes value (reflect.Value) which is the slice or array.
// Takes depth (int) which is how deep the walk is.
//
// Returns reflect.Value which is the copy.
// Returns error when a document cannot be read.
func sglClickHouseLooseList(value reflect.Value, depth int) (reflect.Value, error) {
	looseType := sglClickHouseLooseType(value.Type(), depth)
	if value.Kind() == reflect.Slice && value.IsNil() {
		return reflect.Zero(looseType), nil
	}
	copied := reflect.New(looseType).Elem()
	if value.Kind() == reflect.Slice {
		copied = reflect.MakeSlice(looseType, value.Len(), value.Len())
	}
	for index := range value.Len() {
		element, err := sglClickHouseLoose(value.Index(index), depth+1)
		if err != nil {
			return reflect.Value{}, err
		}
		copied.Index(index).Set(element)
	}
	return copied, nil
}

// sglClickHouseLooseMap copies a map entry by entry.
//
// Takes value (reflect.Value) which is the map.
// Takes depth (int) which is how deep the walk is.
//
// Returns reflect.Value which is the copy.
// Returns error when a document cannot be read.
func sglClickHouseLooseMap(value reflect.Value, depth int) (reflect.Value, error) {
	looseType := sglClickHouseLooseType(value.Type(), depth)
	if value.IsNil() {
		return reflect.Zero(looseType), nil
	}
	copied := reflect.MakeMapWithSize(looseType, value.Len())
	entries := value.MapRange()
	for entries.Next() {
		element, err := sglClickHouseLoose(entries.Value(), depth+1)
		if err != nil {
			return reflect.Value{}, err
		}
		copied.SetMapIndex(entries.Key(), element)
	}
	return copied, nil
}

// sglClickHouseLooseDocument reads a document, or a pointer to one, into the package's
// JSON type, a nil pointer as the JSON type's zero value; any other value is returned as
// it is.
//
// Takes value (reflect.Value) which is the value.
// Takes depth (int) which is how deep the walk is.
//
// Returns reflect.Value which is the JSON type's value, or value.
// Returns error when the document cannot be read.
func sglClickHouseLooseDocument(value reflect.Value, depth int) (reflect.Value, error) {
	looseType := sglClickHouseLooseType(value.Type(), depth)
	if looseType == value.Type() {
		return value, nil
	}
	if value.Kind() == reflect.Pointer && value.IsNil() {
		return reflect.Zero(looseType), nil
	}
	text, _, err := sglClickHouseDocumentText(value.Interface())
	if err != nil {
		return reflect.Value{}, err
	}
	document := reflect.New(looseType).Elem()
	if err := sglReadClickHouseText(document, text); err != nil {
		return reflect.Value{}, err
	}
	return document, nil
}
