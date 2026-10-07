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
	"database/sql/driver"
	"testing"

	"github.com/stretchr/testify/require"
)

type driverBinaryIdentifier [4]byte

func (identifier driverBinaryIdentifier) MarshalBinary() ([]byte, error) {
	return identifier[:], nil
}

func (identifier driverBinaryIdentifier) Value() (driver.Value, error) {
	return "text of the identifier", nil
}

type driverBinaryArray [2]byte

func TestDriverBinaryBindsTheBytesAValueMarshalsItselfTo(t *testing.T) {
	t.Parallel()

	identifier := driverBinaryIdentifier{1, 2, 3, 4}
	var missing *driverBinaryIdentifier

	testCases := []struct {
		value any
		want  driver.Value
		name  string
	}{
		{name: "nil", value: nil, want: nil},
		{name: "a binary marshaler ahead of its valuer", value: identifier, want: []byte{1, 2, 3, 4}},
		{name: "a pointer to a binary marshaler", value: &identifier, want: []byte{1, 2, 3, 4}},
		{name: "a nil pointer", value: missing, want: nil},
		{name: "bytes", value: []byte("raw"), want: []byte("raw")},
		{name: "any other value, for pgx to bind", value: driverBinaryArray{5, 6}, want: driverBinaryArray{5, 6}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			value, err := sglDriverBinary{value: testCase.value}.Value()
			require.NoError(t, err)
			require.Equal(t, testCase.want, value)
		})
	}
}
