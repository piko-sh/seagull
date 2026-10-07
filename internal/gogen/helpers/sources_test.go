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

package helpers

import (
	"fmt"
	"io/fs"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreparedSourcesAreIndependentAcrossConcurrentRenders(t *testing.T) {
	original := []byte(`package source
import "strconv"
//sgl:data limit
const sglLimit=0
//sgl:when enabled
func sglText() string { return strconv.Itoa(sglLimit) }
`)
	sources := NewSources(fstest.MapFS{"helper.go": &fstest.MapFile{Data: original}}, ".")
	var workers sync.WaitGroup
	for index := range 16 {
		workers.Go(func() {
			vocabulary := prefixVocabulary{prefix: fmt.Sprintf("p%d", index), title: fmt.Sprintf("P%d", index)}
			data := &Data{Package: "store", Flags: map[string]bool{"enabled": index%2 == 0}, Values: map[string]any{"limit": index}}
			want, err := File("helper.go", original, vocabulary, data)
			if !assert.NoError(t, err) {
				return
			}
			got, err := sources.File("helper.go", "helper.go", vocabulary, data)
			if !assert.NoError(t, err) {
				return
			}
			assert.Equalf(t, string(want.Content), string(got.Content), "render %d differs from a fresh parser", index)
			got.Content[0] = '!'
			again, err := sources.File("helper.go", "helper.go", vocabulary, data)
			if !assert.NoError(t, err) {
				return
			}
			assert.Equalf(t, string(want.Content), string(again.Content), "render %d shares output storage", index)
		})
	}
	workers.Wait()
	_, err := sources.File("absent.go", "absent.go", prefixVocabulary{}, &Data{})
	require.Error(t, err)
}

func TestPreparedSourcesNameTheSourceThatFailedToLoad(t *testing.T) {
	testCases := []struct {
		files      fstest.MapFS
		name       string
		directory  string
		sourceName string
		wantText   string
		wantIs     error
	}{
		{
			name:       "a missing directory names the requested source",
			files:      fstest.MapFS{},
			directory:  "missing",
			sourceName: "helper.go",
			wantText:   "helper source helper.go: ",
			wantIs:     fs.ErrNotExist,
		},
		{
			name:       "an unparsable source names itself",
			files:      fstest.MapFS{"broken.go": &fstest.MapFile{Data: []byte("package source\n\nfunc (\n")}},
			directory:  ".",
			sourceName: "broken.go",
			wantText:   "helper source broken.go: ",
			wantIs:     nil,
		},
		{
			name:       "an absent source names itself",
			files:      fstest.MapFS{"helper.go": &fstest.MapFile{Data: []byte("package source\n")}},
			directory:  ".",
			sourceName: "absent.go",
			wantText:   "helper source absent.go: ",
			wantIs:     fs.ErrNotExist,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			sources := NewSources(testCase.files, testCase.directory)
			_, err := sources.File("out.go", testCase.sourceName, prefixVocabulary{}, &Data{})
			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.wantText)
			if testCase.wantIs != nil {
				assert.ErrorIs(t, err, testCase.wantIs)
			}
		})
	}
}
