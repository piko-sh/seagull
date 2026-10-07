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
	"net/netip"
	"testing"
)

func BenchmarkClickHouseShapedValues(b *testing.B) {
	shape, err := sglClickHouseParseShape("[n]")
	if err != nil {
		b.Fatal(err)
	}
	for _, n := range []int{3, 100, 1000} {
		values := make([]int64, n)
		for i := range values {
			values[i] = int64(i + 1000)
		}
		b.Run(fmt.Sprintf("Integers%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				clickHouseTextSink = sglClickHouseShapedValue(values, shape)
			}
		})
	}
}

var clickHouseTextSink string

func BenchmarkClickHouseMap(b *testing.B) {
	shape, err := sglClickHouseParseShape("{q:n}")
	if err != nil {
		b.Fatal(err)
	}
	values := make(map[string]int64, 1000)
	for i := range 1000 {
		values[fmt.Sprintf("key%04d", i)] = int64(i)
	}
	b.ReportAllocs()
	for b.Loop() {
		clickHouseTextSink = sglClickHouseShapedValue(values, shape)
	}
}

func TestNativeShapeAllocationBudget(t *testing.T) {
	shape, err := sglClickHouseParseShape("('id'=a,'name'=a,'ip'=4)")
	if err != nil {
		t.Fatal(err)
	}
	shape = prepareTestShape(shape)
	values := []any{int64(1), "Alice", netip.MustParseAddr("127.0.0.1")}
	appendRow := func(...any) error { return nil }
	count := testing.AllocsPerRun(100, func() {
		if err := sglAppendNativeShapedRow(appendRow, shape, "('id'=a,'name'=a,'ip'=4)", values...); err != nil {
			panic(err)
		}
	})
	if count > 1 {
		t.Fatalf("%.0f helper allocations per row, budget 1", count)
	}
}

func BenchmarkClickHouseShapedAppend(b *testing.B) {
	shape, err := sglClickHouseParseShape("('id'=a,'name'=a,'ip'=4)")
	if err != nil {
		b.Fatal(err)
	}
	shape = prepareTestShape(shape)
	values := []any{int64(1), "Alice", netip.MustParseAddr("127.0.0.1")}
	appendRow := func(...any) error { return nil }
	b.ReportAllocs()
	for b.Loop() {
		if err := sglAppendNativeShapedRow(appendRow, shape, "('id'=a,'name'=a,'ip'=4)", values...); err != nil {
			b.Fatal(err)
		}
	}
}

func prepareTestShape(shape sglClickHouseShape) sglClickHouseShape {
	shape.prepared = true
	shape.checked = sglAppendNativeHolds(shape, sglAppendNativeChecked)
	shape.driverMap = sglAppendNativeHolds(shape, sglAppendNativeDriverMap)
	for index, element := range shape.elements {
		shape.elements[index] = prepareTestShape(element)
	}
	return shape
}
