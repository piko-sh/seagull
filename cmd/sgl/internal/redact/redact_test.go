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

package redact

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

var errWriteFailed = errors.New("write failed")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errWriteFailed
}

func TestDSN(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
		leaked   []string
	}{
		{name: "empty", input: "", expected: ""},
		{name: "url password", input: "postgres://user:secret@localhost:5432/app?sslmode=disable", expected: "postgres://user:xxxxx@localhost:5432/app?sslmode=disable", leaked: []string{"secret"}},
		{name: "url without password", input: "postgres://user@localhost/app", expected: "postgres://user@localhost/app"},
		{name: "url password containing at", input: "postgres://u:p@ss@h/db", expected: "postgres://u:xxxxx@h/db", leaked: []string{"p@ss", "ss@"}},
		{name: "url password with invalid escape", input: "postgres://user:pa%zzss@localhost/db", expected: "postgres://user:xxxxx@localhost/db", leaked: []string{"pa%zzss", "zzss"}},
		{name: "url password containing hash", input: "postgres://u:pa#ss@h/db", expected: "postgres://u:xxxxx@h/db", leaked: []string{"pa#ss", "#ss"}},
		{name: "url password that reads as a port and fragment", input: "postgres://u:12#34@h/db", expected: "postgres://u:xxxxx@h/db", leaked: []string{"12#34", "#34"}},
		{name: "url password that reads as a port and query", input: "postgres://u:1?w@h/db", expected: "postgres://u:xxxxx@h/db", leaked: []string{"1?w"}},
		{name: "url password query parameter without userinfo password", input: "postgres://alice@db.example.com/app?password=hunter2", expected: "postgres://alice@db.example.com/app?password=xxxxx", leaked: []string{"hunter2"}},
		{name: "url password query parameter among others", input: "postgres://deploy@db:5432/app?password=S3cret&sslmode=require", expected: "postgres://deploy@db:5432/app?password=xxxxx&sslmode=require", leaked: []string{"S3cret"}},
		{name: "url sslpassword query parameter", input: "postgres://u:secret@h/db?sslpassword=keypass", expected: "postgres://u:xxxxx@h/db?sslpassword=xxxxx", leaked: []string{"secret", "keypass"}},
		{name: "url upper-case password query parameter", input: "clickhouse://h:9000/default?PASSWORD=hunter2&dial_timeout=1s", expected: "clickhouse://h:9000/default?PASSWORD=xxxxx&dial_timeout=1s", leaked: []string{"hunter2"}},
		{name: "url keeps fragment", input: "postgres://user:secret@h/db#section", expected: "postgres://user:xxxxx@h/db#section", leaked: []string{"secret"}},
		{name: "url query parameters before a fragment", input: "postgres://user:secret@h/db?password=hunter2&sslmode=disable#section", expected: "postgres://user:xxxxx@h/db?password=xxxxx&sslmode=disable#section", leaked: []string{"secret", "hunter2"}},
		{name: "url query key with invalid escape", input: "postgres://h/db?password%zz=hunter2", expected: "postgres://h/db?password%zz=xxxxx", leaked: []string{"hunter2"}},
		{name: "url query parameter with invalid escape", input: "postgres://h:5432/db?password=%zz", expected: "postgres://h:5432/db?password=xxxxx", leaked: []string{"%zz"}},
		{name: "url unparseable without userinfo", input: "postgres://h:abc/db?password=hunter2", expected: "postgres://xxxxx", leaked: []string{"hunter2"}},
		{name: "url with at sign after the host", input: "postgres://h/db?application_name=a@b", expected: "postgres://xxxxx@b"},
		{name: "mysql", input: "user:secret@tcp(localhost:3306)/app", expected: "user:xxxxx@tcp(localhost:3306)/app", leaked: []string{"secret"}},
		{name: "mysql password containing at", input: "user:p@ssw0rd@tcp(localhost:3306)/app", expected: "user:xxxxx@tcp(localhost:3306)/app", leaked: []string{"p@ssw0rd", "ssw0rd"}},
		{name: "mysql password containing slash", input: "user:pa/ss@tcp(h:3306)/app?loc=Europe/London", expected: "user:xxxxx@tcp(h:3306)/app?loc=Europe/London", leaked: []string{"pa/ss"}},
		{name: "mysql at sign in parameters", input: "user:pw@tcp(h)/db?x=a@b", expected: "user:xxxxx@tcp(h)/db?x=a@b", leaked: []string{"pw@"}},
		{name: "mysql default host", input: "user:secret@/app", expected: "user:xxxxx@/app", leaked: []string{"secret"}},
		{name: "mysql user without password", input: "user@tcp(h)/app", expected: "user@tcp(h)/app"},
		{name: "key value", input: "host=localhost user=app password=secret dbname=app", expected: "host=localhost user=app password=xxxxx dbname=app", leaked: []string{"secret"}},
		{name: "key value quoted password with space", input: "host=localhost user=app password='my secret' dbname=x", expected: "host=localhost user=app password=xxxxx dbname=x", leaked: []string{"my secret", "secret'"}},
		{name: "key value quoted password with escaped quote", input: `host=h password='it\'s secret' user=u`, expected: "host=h password=xxxxx user=u", leaked: []string{"it", "secret"}},
		{name: "key value escaped space", input: `password=a\ b host=h`, expected: "password=xxxxx host=h", leaked: []string{`a\ b`}},
		{name: "key value spaces around equals", input: "host=h user=u password = hunter2", expected: "host=h user=u password = xxxxx", leaked: []string{"hunter2"}},
		{name: "key value upper-case key", input: "PASSWORD=Secret host=h", expected: "PASSWORD=xxxxx host=h", leaked: []string{"Secret"}},
		{name: "key value at sign in password", input: "host=h password=p@ss user=u", expected: "host=h password=xxxxx user=u", leaked: []string{"p@ss"}},
		{name: "key value sslpassword", input: "host=h sslpassword=keypass", expected: "host=h sslpassword=xxxxx", leaked: []string{"keypass"}},
		{name: "key value short password name", input: "host=h pwd=hunter2", expected: "host=h pwd=xxxxx", leaked: []string{"hunter2"}},
		{name: "key value trailing backslash", input: `host=h password=abc\`, expected: "host=h password=xxxxx", leaked: []string{"abc"}},
		{name: "key value unterminated quote", input: "host=h password='abc def", expected: "host=h password=xxxxx", leaked: []string{"abc", "def"}},
		{name: "semicolon separated", input: "Server=h;User Id=u;Password=hunter2;Database=d", expected: "Server=h;User Id=u;Password=xxxxx;Database=d", leaked: []string{"hunter2"}},
		{name: "sqlite file", input: "file:app.db", expected: "file:app.db"},
		{name: "sqlite file with auth password", input: "file:app.db?_auth_user=admin&_auth_pass=secret", expected: "file:app.db?_auth_user=admin&_auth_pass=xxxxx", leaked: []string{"secret"}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			redacted := DSN(testCase.input)
			require.Equal(t, testCase.expected, redacted)
			for _, secret := range testCase.leaked {
				require.NotContains(t, redacted, secret)
			}
		})
	}
}

func TestText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		text     string
		dsn      string
		expected string
	}{
		{
			name:     "connection string and bare password",
			text:     "cannot connect to postgres://u:hunter2@h/db: password hunter2 rejected",
			dsn:      "postgres://u:hunter2@h/db",
			expected: "cannot connect to postgres://u:xxxxx@h/db: password xxxxx rejected",
		},
		{
			name:     "decoded password",
			text:     `failed: user "u" password "p@ss" refused`,
			dsn:      "postgres://u:p%40ss@h/db",
			expected: `failed: user "u" password "xxxxx" refused`,
		},
		{
			name:     "encoded password",
			text:     "parse u:p%40ss@h failed",
			dsn:      "postgres://u:p%40ss@h/db",
			expected: "parse u:xxxxx@h failed",
		},
		{
			name:     "unquoted key value password",
			text:     "pq: password 'my secret' is wrong",
			dsn:      "host=h password='my secret'",
			expected: "pq: password 'xxxxx' is wrong",
		},
		{
			name:     "empty connection string",
			text:     "nothing to hide",
			dsn:      "",
			expected: "nothing to hide",
		},
		{
			name:     "no password",
			text:     "cannot open file:app.db",
			dsn:      "file:app.db",
			expected: "cannot open file:app.db",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expected, Text(testCase.text, testCase.dsn))
		})
	}
}

func TestWriterRedactsRegisteredConnectionStrings(t *testing.T) {
	t.Parallel()

	var buffer bytes.Buffer
	writer := NewWriter(&buffer)

	message := []byte("sgl: connecting to postgres://u:hunter2@h/db failed\n")
	written, err := writer.Write(message)
	require.NoError(t, err)
	require.Equal(t, len(message), written)
	require.Equal(t, string(message), buffer.String())

	buffer.Reset()
	writer.Add("")
	writer.Add("postgres://u:hunter2@h/db")
	writer.Add("postgres://u:hunter2@h/db")
	written, err = writer.Write(message)
	require.NoError(t, err)
	require.Equal(t, len(message), written)
	require.Equal(t, "sgl: connecting to postgres://u:xxxxx@h/db failed\n", buffer.String())
}

func TestWriterReportsWriteFailures(t *testing.T) {
	t.Parallel()

	writer := NewWriter(failingWriter{})
	writer.Add("postgres://u:hunter2@h/db")

	written, err := writer.Write([]byte("hunter2"))

	require.ErrorIs(t, err, errWriteFailed)
	require.Zero(t, written)
}
