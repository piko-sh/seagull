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
	"encoding/json"
	"reflect"
	"sync/atomic"
)

// JSONCodec encodes and decodes database JSON in place of encoding/json.
//
// Its methods must accept the same values and documents as json.Marshal and
// json.Unmarshal. Unmarshal must reject text containing more than one document.
type JSONCodec interface {
	// Marshal encodes a value as JSON.
	//
	// Takes value (any) which is the value to encode.
	//
	// Returns []byte which is the JSON document.
	// Returns error when the value cannot be encoded.
	Marshal(value any) ([]byte, error)

	// Unmarshal decodes a JSON document into the value a pointer points to.
	//
	// Takes data ([]byte) which is the JSON document.
	// Takes value (any) which is the pointer to decode into.
	//
	// Returns error when the document cannot be decoded.
	Unmarshal(data []byte, value any) error
}

// sglJSONPretoucher is a JSON codec that prepares itself for a Go type before it first
// encodes or decodes one, such as piko.sh/json.
type sglJSONPretoucher interface {
	// Pretouch prepares the codec for a Go type.
	//
	// Takes target (reflect.Type) which is the type to prepare for.
	//
	// Returns error when the codec cannot prepare for the type.
	Pretouch(target reflect.Type) error
}

// sglJSONCodecHolder holds the codec SetJSONCodec installed.
type sglJSONCodecHolder struct {
	// codec is the installed codec.
	codec JSONCodec
}

// sglJSONCodecInUse holds the codec SetJSONCodec installed, or nil for encoding/json, so
// queries running on other goroutines read it safely.
var sglJSONCodecInUse atomic.Pointer[sglJSONCodecHolder]

// sglJSONDecodedTypes lists the Go types the package's queries decode JSON columns into,
// which SetJSONCodec pretouches.
//
//sgl:data jsonTypes
var sglJSONDecodedTypes = []reflect.Type{reflect.TypeFor[[]string]()}

// SetJSONCodec makes the package encode and decode JSON with codec in place of
// encoding/json, and restores encoding/json when codec is nil. It is safe to call while
// queries run, which use the codec installed when they encode or decode a value.
//
// A codec with a Pretouch(reflect.Type) error method, such as piko.sh/json, is first
// prepared for the Go types the package decodes JSON columns into. SetJSONCodec still
// installs the codec when Pretouch returns an error.
//
// Takes codec (JSONCodec) which is the codec, or nil for encoding/json.
func SetJSONCodec(codec JSONCodec) {
	if codec == nil {
		sglJSONCodecInUse.Store(nil)
		return
	}
	if pretoucher, ok := codec.(sglJSONPretoucher); ok {
		for _, target := range sglJSONDecodedTypes {
			_ = pretoucher.Pretouch(target)
		}
	}
	sglJSONCodecInUse.Store(&sglJSONCodecHolder{codec: codec})
}

// sglJSONMarshal encodes a value as JSON with the installed codec, or encoding/json.
//
// Takes value (any) which is the value to encode.
//
// Returns []byte which is the JSON text.
// Returns error when the value cannot be encoded.
func sglJSONMarshal(value any) ([]byte, error) {
	if holder := sglJSONCodecInUse.Load(); holder != nil {
		return holder.codec.Marshal(value)
	}
	return json.Marshal(value)
}

// sglJSONUnmarshal decodes a JSON document with the installed codec, or encoding/json.
//
// Takes data ([]byte) which is the JSON text.
// Takes value (any) which points to the value to fill.
//
// Returns error when the text is not one JSON document the value can hold.
func sglJSONUnmarshal(data []byte, value any) error {
	if holder := sglJSONCodecInUse.Load(); holder != nil {
		return holder.codec.Unmarshal(data, value)
	}
	return json.Unmarshal(data, value)
}
