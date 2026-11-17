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
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type countingCodec struct {
	pretouched []reflect.Type
	marshals   int
	unmarshals int
}

func (codec *countingCodec) Marshal(value any) ([]byte, error) {
	codec.marshals++
	return json.Marshal(value)
}

func (codec *countingCodec) Unmarshal(data []byte, value any) error {
	codec.unmarshals++
	return json.Unmarshal(data, value)
}

func (codec *countingCodec) Pretouch(target reflect.Type) error {
	codec.pretouched = append(codec.pretouched, target)
	return nil
}

type plainCodec struct{ counted countingCodec }

func (codec *plainCodec) Marshal(value any) ([]byte, error) { return codec.counted.Marshal(value) }

func (codec *plainCodec) Unmarshal(data []byte, value any) error {
	return codec.counted.Unmarshal(data, value)
}

func TestSetJSONCodecRoutesTheHelpersThroughTheCodec(t *testing.T) {
	codec := &countingCodec{}
	SetJSONCodec(codec)
	t.Cleanup(func() { SetJSONCodec(nil) })

	require.Equal(t, sglJSONDecodedTypes, codec.pretouched, "a codec that pretouches is prepared for every decoded type")

	var tags []string
	require.NoError(t, sglScanInto(&tags).Scan([]byte(`["a","b"]`)))
	require.Equal(t, []string{"a", "b"}, tags)
	encoded, err := Null[int64]{V: 7, Valid: true}.MarshalJSON()
	require.NoError(t, err)
	require.Equal(t, "7", string(encoded))
	var decoded Null[int64]
	require.NoError(t, decoded.UnmarshalJSON([]byte("8")))
	require.Equal(t, Null[int64]{V: 8, Valid: true}, decoded)
	var document JSON
	require.NoError(t, document.Scan(map[string]any{"a": 1}))
	require.Equal(t, 2, codec.marshals)
	require.Equal(t, 2, codec.unmarshals)

	SetJSONCodec(nil)
	require.NoError(t, sglScanInto(&tags).Scan([]byte(`["c"]`)))
	require.Equal(t, 2, codec.unmarshals, "a nil codec restores encoding/json")
}

func TestSetJSONCodecTakesACodecWithoutPretouch(t *testing.T) {
	codec := &plainCodec{}
	SetJSONCodec(codec)
	t.Cleanup(func() { SetJSONCodec(nil) })

	var values []int64
	require.NoError(t, sglScanInto(&values).Scan([]byte(`[1,2]`)))
	require.Equal(t, 1, codec.counted.unmarshals)
	require.Empty(t, codec.counted.pretouched)
}

func TestSetJSONCodecIsSafeWhileQueriesDecode(t *testing.T) {
	t.Cleanup(func() { SetJSONCodec(nil) })
	var group sync.WaitGroup
	for range 4 {
		group.Go(func() {
			for range 200 {
				var values []int64
				if err := sglScanInto(&values).Scan([]byte(`[1,2]`)); err != nil {
					panic(err)
				}
			}
		})
	}
	for range 50 {
		SetJSONCodec(&plainCodec{})
		SetJSONCodec(nil)
	}
	group.Wait()
}
