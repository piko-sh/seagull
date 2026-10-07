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
	"fmt"
	"strings"
	"testing"
)

func BenchmarkJSONColumn(b *testing.B) {
	for _, size := range []int{10, 100, 1000} {
		raw := []byte("[" + strings.Repeat("123,", size-1) + "123]")
		b.Run(fmt.Sprintf("Integers%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var values []int64
				if err := sglScanInto(&values).Scan(raw); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestJSONColumnAllocationBudget(t *testing.T) {
	raw := []byte("[" + strings.Repeat("123,", 999) + "123]")
	allocations := testing.AllocsPerRun(20, func() {
		var values []int64
		if err := sglScanInto(&values).Scan(raw); err != nil {
			panic(err)
		}
	})
	if allocations > 20 {
		t.Fatalf("%.0f allocations decoding 1000 integers, budget 20", allocations)
	}
}

var jsonDecodeCalls int

type countedJSON int

func (*countedJSON) UnmarshalJSON(raw []byte) error {
	jsonDecodeCalls++
	if string(raw) == "1" {
		return nil
	}
	return fmt.Errorf("unsupported document %s", raw)
}

func TestJSONFastPathPreservesCustomDecoderCalls(t *testing.T) {
	for _, c := range []struct {
		raw   string
		calls int
		fails bool
	}{{"1", 1, false}, {"2", 2, true}, {"null", 2, true}, {"1 2", 0, true}, {"nul", 0, true}} {
		jsonDecodeCalls = 0
		var value countedJSON
		err := sglScanInto(&value).Scan([]byte(c.raw))
		if (err != nil) != c.fails || jsonDecodeCalls != c.calls {
			t.Fatalf("%s: calls=%d err=%v", c.raw, jsonDecodeCalls, err)
		}
	}
}
