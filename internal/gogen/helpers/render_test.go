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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/scaling"
)

type prefixVocabulary struct {
	prefix       string
	title        string
	undocumented bool
}

func (v prefixVocabulary) Helper(suffix string) string { return v.prefix + suffix }

func (v prefixVocabulary) Sentinel(suffix string) string { return "err" + v.title + suffix }

func (v prefixVocabulary) ErrorValue(suffix string) string { return "Err" + v.title + suffix }

func (v prefixVocabulary) Message(text string) string { return v.prefix + ": " + text }

func (v prefixVocabulary) DocComments() bool { return !v.undocumented }

var acme = prefixVocabulary{prefix: "acme", title: "Acme"}

const licence = `// Copyright 2026 PolitePixels Limited
//
// Licensed under the Apache License, Version 2.0 (the "License").

// Package source holds a helper.
`

func TestRenderSpecialisesSources(t *testing.T) {
	testCases := []struct {
		data   Data
		name   string
		source string
		want   string
	}{
		{
			name:   "drops the header and package comment and names the package",
			source: licence + "package source\n\nconst limit = 1\n",
			data:   Data{Package: "generated"},
			want:   "package generated\n\nconst limit = 1\n",
		},
		{
			name: "renames canonical identifiers, comments and messages after the vocabulary",
			source: "package source\n\nimport \"errors\"\n\n" +
				"// errSglTooMany is returned past sglLimit; see ErrSglUnknown.\n" +
				"var errSglTooMany = errors.New(\"sgl: too many\")\n\n" +
				"// ErrSglUnknown is returned for an unknown sgl.thing.\n" +
				"var ErrSglUnknown = errors.New(\"sgl: unknown\")\n\n" +
				"const sglLimit = 3\n\n" +
				"func sglCheck(count int) error {\n\tif count > sglLimit {\n\t\treturn errSglTooMany\n\t}\n\treturn nil\n}\n",
			data: Data{Package: "generated"},
			want: "package generated\n\nimport \"errors\"\n\n" +
				"// errAcmeTooMany is returned past acmeLimit; see ErrAcmeUnknown.\n" +
				"var errAcmeTooMany = errors.New(\"acme: too many\")\n\n" +
				"// ErrAcmeUnknown is returned for an unknown sgl.thing.\n" +
				"var ErrAcmeUnknown = errors.New(\"acme: unknown\")\n\n" +
				"const acmeLimit = 3\n\n" +
				"func acmeCheck(count int) error {\n\tif count > acmeLimit {\n\t\treturn errAcmeTooMany\n\t}\n\treturn nil\n}\n",
		},
		{
			name: "keeps a declaration only when its flags hold",
			source: "package source\n\nconst first = 1\n//sgl:when numbered\nconst second = 2\n" +
				"//sgl:when !numbered\nconst third = 3\n//sgl:when numbered named\nconst fourth = 4\n",
			data: Data{Package: "generated", Flags: map[string]bool{"numbered": true, "named": false}},
			want: "package generated\n\nconst first = 1\nconst second = 2\n",
		},
		{
			name: "replaces the literal a data declaration holds",
			source: "package source\n\nimport \"regexp\"\n\n//sgl:data limit\nconst sglLimit = 1\n\n" +
				"//sgl:data pattern\nvar sglPattern = regexp.MustCompile(`x`)\n\n//sgl:data name\nvar sglName = \"x\"\n\n" +
				"//sgl:data allowed\nvar sglAllowed = map[string]bool{}\n",
			data: Data{Package: "generated", Values: map[string]any{
				"limit":   65535,
				"pattern": `^(?i:a\b)$`,
				"name":    "a\tb",
				"allowed": Code{Text: "map[string]bool{\n\"=\": true,\n\"LIKE\": true,\n}", Imports: nil},
			}},
			want: "package generated\n\nimport \"regexp\"\n\nconst acmeLimit = 65535\n\n" +
				"var acmePattern = regexp.MustCompile(`^(?i:a\\b)$`)\nvar acmeName = \"a\\tb\"\n" +
				"var acmeAllowed = map[string]bool{\n\t\"=\":    true,\n\t\"LIKE\": true,\n}\n",
		},
		{
			name: "quotes a raw data value that a raw string cannot hold",
			source: "package source\n\nimport \"regexp\"\n\n//sgl:data pattern\nvar sglPattern = regexp.MustCompile(`x`)\n\n" +
				"//sgl:data carriage\nvar sglCarriage = regexp.MustCompile(`x`)\n\n//sgl:data invalid\nvar sglInvalid = regexp.MustCompile(`x`)\n",
			data: Data{Package: "generated", Values: map[string]any{
				"pattern":  "^`a`$",
				"carriage": "a\r\nb",
				"invalid":  "a\xffb\x00\uFEFF",
			}},
			want: "package generated\n\nimport \"regexp\"\n\nvar acmePattern = regexp.MustCompile(\"^`a`$\")\n" +
				"var acmeCarriage = regexp.MustCompile(\"a\\r\\nb\")\nvar acmeInvalid = regexp.MustCompile(\"a\\xffb\\x00\\ufeff\")\n",
		},
		{
			name: "writes an inlined value where it is used",
			source: "package source\n\n//sgl:inline marker\nconst sglMarker = '?'\n\n//sgl:inline prefix\nconst sglPrefix = \"?\"\n\n" +
				"func sglIsMarker(character byte) bool {\n\treturn character == sglMarker\n}\n\n" +
				"func sglPlaceholder() string {\n\treturn sglPrefix\n}\n",
			data: Data{Package: "generated", Values: map[string]any{"marker": '$', "prefix": "$"}},
			want: "package generated\n\nfunc acmeIsMarker(character byte) bool {\n\treturn character == '$'\n}\n" +
				"func acmePlaceholder() string {\n\treturn \"$\"\n}\n",
		},
		{
			name: "folds an if statement on an inlined condition to the branch it takes",
			source: "package source\n\n//sgl:inline numbered\nconst sglNumbered = true\n\n" +
				"func sglNext(count int) int {\n\tif sglNumbered {\n\t\tcount++\n\t} else {\n\t\t_ = count\n\t}\n\treturn count\n}\n\n" +
				"func sglMark(count int) string {\n\tif !sglNumbered {\n\t\treturn \"?\"\n\t}\n\t_ = count\n\treturn \"$\"\n}\n",
			data: Data{Package: "generated", Values: map[string]any{"numbered": false}},
			want: "package generated\n\nfunc acmeNext(count int) int {\n\t_ = count\n\treturn count\n}\n" +
				"func acmeMark(count int) string {\n\treturn \"?\"\n}\n",
		},
		{
			name: "drops an if statement whose condition does not hold and has no else",
			source: "package source\n\n//sgl:inline numbered\nconst sglNumbered = true\n\n" +
				"func sglNext(count int) int {\n\tif sglNumbered {\n\t\tcount++\n\t}\n\treturn count\n}\n",
			data: Data{Package: "generated", Values: map[string]any{"numbered": false}},
			want: "package generated\n\nfunc acmeNext(count int) int {\n\treturn count\n}\n",
		},
		{
			name: "unrolls a range over an inlined list",
			source: "package source\n\nimport \"strings\"\n\n//sgl:inline operators\nvar sglOperators = []string{\"->\"}\n\n" +
				"func sglRoot(column string) string {\n\tfor _, operator := range sglOperators {\n" +
				"\t\tif index := strings.Index(column, operator); index > 0 {\n\t\t\treturn column[:index]\n\t\t}\n\t}\n\treturn column\n}\n",
			data: Data{Package: "generated", Values: map[string]any{"operators": []string{"#>", "->"}}},
			want: "package generated\n\nimport \"strings\"\n\nfunc acmeRoot(column string) string {\n" +
				"\tif index := strings.Index(column, \"#>\"); index > 0 {\n\t\treturn column[:index]\n\t}\n" +
				"\tif index := strings.Index(column, \"->\"); index > 0 {\n\t\treturn column[:index]\n\t}\n\treturn column\n}\n",
		},
		{
			name: "drops a range over an empty inlined list and the imports only it used",
			source: "package source\n\nimport (\n\t\"fmt\"\n\t\"strings\"\n)\n\n//sgl:inline operators\nvar sglOperators = []string{\"->\"}\n\n" +
				"func sglRoot(column string) string {\n\tfor _, operator := range sglOperators {\n" +
				"\t\tif index := strings.Index(column, operator); index > 0 {\n\t\t\treturn column[:index]\n\t\t}\n\t}\n\treturn fmt.Sprint(column)\n}\n",
			data: Data{Package: "generated", Values: map[string]any{"operators": []string{}}},
			want: "package generated\n\nimport \"fmt\"\n\nfunc acmeRoot(column string) string {\n\treturn fmt.Sprint(column)\n}\n",
		},
		{
			name: "writes an inlined type and adds the import it needs",
			source: "package source\n\n//sgl:inline group\ntype sglGroup struct{ Value []any }\n\n" +
				"func sglWrap(values []any) any {\n\treturn sglGroup{Value: values}\n}\n",
			data: Data{Package: "generated", Values: map[string]any{
				"group": Code{Text: "clickhouse.GroupSet", Imports: []string{"github.com/ClickHouse/clickhouse-go/v2"}},
			}},
			want: "package generated\n\nimport \"github.com/ClickHouse/clickhouse-go/v2\"\n\n" +
				"func acmeWrap(values []any) any {\n\treturn clickhouse.GroupSet{Value: values}\n}\n",
		},
		{
			name: "evaluates the directives on the specs of a grouped declaration",
			source: "package source\n\nconst (\n\t// sglFirst is kept.\n\tsglFirst = 1\n\n" +
				"\t// sglSecond is dropped.\n\t//\n\t//sgl:when numbered\n\tsglSecond = 2\n\n" +
				"\t// sglPrefix is inlined.\n\t//\n\t//sgl:inline prefix\n\tsglPrefix = \"?\"\n\n" +
				"\t// sglLimit holds data.\n\t//\n\t//sgl:data limit\n\tsglLimit = 1\n)\n\n" +
				"func sglPlaceholder() string {\n\treturn sglPrefix\n}\n",
			data: Data{Package: "generated", Flags: map[string]bool{"numbered": false}, Values: map[string]any{"prefix": "$", "limit": 7}},
			want: "package generated\n\nconst (\n\t// acmeFirst is kept.\n\tacmeFirst = 1\n\n\t// acmeLimit holds data.\n\tacmeLimit = 7\n)\n\n" +
				"func acmePlaceholder() string {\n\treturn \"$\"\n}\n",
		},
		{
			name: "removes the specs that open and close a grouped declaration with the blank lines that part them",
			source: "package source\n\nvar (\n\t//sgl:when numbered\n\tsglA = 1\n\n\t//sgl:when numbered\n\tsglB = 2\n\n" +
				"\tsglC = 3\n\n\tsglD = 4\n\n\t//sgl:when numbered\n\tsglE = 5\n)\n",
			data: Data{Package: "generated", Flags: map[string]bool{"numbered": false}},
			want: "package generated\n\nvar (\n\tacmeC = 3\n\n\tacmeD = 4\n)\n",
		},
		{
			name: "removes a grouped declaration none of whose specs is left",
			source: "package source\n\nconst sglKept = 1\n\nconst (\n\t//sgl:when numbered\n\tsglA = 1\n\n" +
				"\t//sgl:inline marker\n\tsglMarker = '?'\n)\n",
			data: Data{Package: "generated", Flags: map[string]bool{"numbered": false}, Values: map[string]any{"marker": '$'}},
			want: "package generated\n\nconst acmeKept = 1\n",
		},
		{
			name:   "renames as the data says, past the vocabulary",
			source: "package source\n\n// sglTupleNative binds a tuple.\nfunc sglTupleNative() {}\n\n// QueryName names a query.\nfunc QueryName() {}\n",
			data:   Data{Package: "generated", Renames: map[string]string{"sglTupleNative": "acmeTuple", "QueryName": "Resolve"}},
			want:   "package generated\n\n// acmeTuple binds a tuple.\nfunc acmeTuple() {}\n\n// Resolve names a query.\nfunc Resolve() {}\n",
		},
		{
			name: "leaves out the empty line gofmt writes before a doc comment's directives",
			source: "package source\n\n// sglLimit bounds a list.\n//\n//sgl:data limit\n//nolint:revive // generated as is\nconst sglLimit = 1\n\n" +
				"// sglNote keeps its paragraphs.\n//\n// It has two.\nconst sglNote = 2\n",
			data: Data{Package: "generated", Values: map[string]any{"limit": 7}},
			want: "package generated\n\n// acmeLimit bounds a list.\nconst acmeLimit = 7\n\n" +
				"// acmeNote keeps its paragraphs.\n//\n// It has two.\nconst acmeNote = 2\n",
		},
		{
			name:   "leaves out nolint lines and trailing nolint comments",
			source: "package source\n\n//nolint:revive // kept as generated\nfunc sglRun() {\n\tsglStep() //nolint:errcheck // fire and forget\n}\n\nfunc sglStep() {}\n",
			data:   Data{Package: "generated"},
			want:   "package generated\n\nfunc acmeRun() {\n\tacmeStep()\n}\nfunc acmeStep() {}\n",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			rendered, err := render([]byte(testCase.source), acme, &testCase.data)
			require.NoError(t, err)
			require.Equal(t, testCase.want, string(rendered))
		})
	}
}

func TestRenderRejectsInvalidSources(t *testing.T) {
	testCases := []struct {
		data   Data
		name   string
		source string
		want   error
	}{
		{
			name:   "a directive that is not above a top-level declaration",
			source: "package source\n\nfunc sglRun() {\n\t//sgl:when numbered\n\t_ = 1\n}\n",
			data:   Data{Package: "generated", Flags: map[string]bool{"numbered": true}},
			want:   errInvalidSource,
		},
		{
			name:   "an unknown directive",
			source: "package source\n\n//sgl:maybe numbered\nconst sglLimit = 1\n",
			data:   Data{Package: "generated"},
			want:   errInvalidSource,
		},
		{
			name:   "a when directive naming a flag the data does not hold",
			source: "package source\n\n//sgl:when numbered\nconst sglLimit = 1\n",
			data:   Data{Package: "generated"},
			want:   errMissingData,
		},
		{
			name:   "a data directive naming a value the data does not hold",
			source: "package source\n\n//sgl:data limit\nconst sglLimit = 1\n",
			data:   Data{Package: "generated"},
			want:   errMissingData,
		},
		{
			name:   "an inline directive on a declaration of two names",
			source: "package source\n\n//sgl:inline limits\nconst sglLow, sglHigh = 1, 2\n",
			data:   Data{Package: "generated", Values: map[string]any{"limits": 1}},
			want:   errInvalidSource,
		},
		{
			name:   "a directive on a spec of a const block that counts with iota",
			source: "package source\n\nconst (\n\tsglA = iota\n\n\t//sgl:when numbered\n\tsglB\n)\n",
			data:   Data{Package: "generated", Flags: map[string]bool{"numbered": false}},
			want:   errInvalidSource,
		},
		{
			name:   "an inline declaration whose specs have directives of their own",
			source: "package source\n\n//sgl:inline group\nvar (\n\t//sgl:when numbered\n\tsglGroup = 1\n)\n",
			data:   Data{Package: "generated", Flags: map[string]bool{"numbered": true}, Values: map[string]any{"group": 1}},
			want:   errInvalidSource,
		},
		{
			name:   "a value of a type no literal is written for",
			source: "package source\n\n//sgl:data limit\nconst sglLimit = 1\n",
			data:   Data{Package: "generated", Values: map[string]any{"limit": 1.5}},
			want:   errUnsupportedValue,
		},
		{
			name:   "source that does not parse",
			source: "package source\n\nfunc {",
			data:   Data{Package: "generated"},
			want:   errInvalidSource,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			_, err := render([]byte(testCase.source), acme, &testCase.data)
			require.ErrorIs(t, err, testCase.want)
		})
	}
}

func TestRenderSpacesDeclarationsAsThePrinterDoes(t *testing.T) {
	source := "package source\n\nimport \"errors\"\n\nconst sglA = 1\n\nconst sglB = 2\n\n// sglC is documented.\nconst sglC = 3\n\n" +
		"var errSglD = errors.New(\"sgl: d\")\n\nfunc sglE() {}\n\nfunc sglF() {}\n\n//sgl:when kept\nfunc sglG() {}\n\n" +
		"type sglH struct{}\n\ntype sglI struct{}\n"
	want := "package generated\n\nimport \"errors\"\n\nconst acmeA = 1\nconst acmeB = 2\n\n// acmeC is documented.\nconst acmeC = 3\n\n" +
		"var errAcmeD = errors.New(\"acme: d\")\n\nfunc acmeE() {}\nfunc acmeF() {}\nfunc acmeG() {}\n\n" +
		"type acmeH struct{}\ntype acmeI struct{}\n"
	rendered, err := render([]byte(source), acme, &Data{Package: "generated", Flags: map[string]bool{"kept": true}})
	require.NoError(t, err)
	require.Equal(t, want, string(rendered))
}

func TestRenderLeavesOutDocCommentsWhenTheVocabularyAsks(t *testing.T) {
	source := "package source\n\n// sglLimit bounds the helper.\n//\n//sgl:when kept\nconst sglLimit = 1\n\n" +
		"const (\n\t// sglFirst is first.\n\tsglFirst = 1\n\n\t// sglSecond is second.\n\tsglSecond = 2\n)\n\n" +
		"// sglPair holds two values.\ntype sglPair struct {\n\t// left is the first value.\n\tleft int\n\n" +
		"\t// rightmost is the second value.\n\trightmost int // trailing note\n}\n\n" +
		"// sglReader reads.\ntype sglReader interface {\n\t// Read reads.\n\tRead() int\n}\n\n" +
		"// sglCheck reports whether a value is in range.\n//\n// Takes value (int) which is checked.\n" +
		"func sglCheck(value int) bool {\n\t// Compare against the limit.\n\treturn value < sglLimit\n}\n"
	testCases := []struct {
		vocabulary prefixVocabulary
		name       string
		want       string
	}{
		{
			name:       "keeps every comment when doc comments are asked for",
			vocabulary: acme,
			want: "package generated\n\n// acmeLimit bounds the helper.\nconst acmeLimit = 1\n" +
				"const (\n\t// acmeFirst is first.\n\tacmeFirst = 1\n\n\t// acmeSecond is second.\n\tacmeSecond = 2\n)\n\n" +
				"// acmePair holds two values.\ntype acmePair struct {\n\t// left is the first value.\n\tleft int\n\n" +
				"\t// rightmost is the second value.\n\trightmost int // trailing note\n}\n\n" +
				"// acmeReader reads.\ntype acmeReader interface {\n\t// Read reads.\n\tRead() int\n}\n\n" +
				"// acmeCheck reports whether a value is in range.\n//\n// Takes value (int) which is checked.\n" +
				"func acmeCheck(value int) bool {\n\t// Compare against the limit.\n\treturn value < acmeLimit\n}\n",
		},
		{
			name:       "leaves out the documentation of declarations, specs and fields but keeps body comments",
			vocabulary: prefixVocabulary{prefix: "acme", title: "Acme", undocumented: true},
			want: "package generated\n\nconst acmeLimit = 1\nconst (\n\tacmeFirst  = 1\n\tacmeSecond = 2\n)\n\n" +
				"type acmePair struct {\n\tleft      int\n\trightmost int\n}\ntype acmeReader interface {\n\tRead() int\n}\n\n" +
				"func acmeCheck(value int) bool {\n\t// Compare against the limit.\n\treturn value < acmeLimit\n}\n",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			rendered, err := render([]byte(source), testCase.vocabulary, &Data{Package: "generated", Flags: map[string]bool{"kept": true}})
			require.NoError(t, err)
			require.Equal(t, testCase.want, string(rendered))
		})
	}
}

func TestRenderLeavesOutTheDocumentationOfGroupedSpecsWithDirectives(t *testing.T) {
	source := "package source\n\nconst (\n\t// sglFirst is dropped.\n\t//\n\t//sgl:when numbered\n\tsglFirst = 1\n\n" +
		"\t// sglSecond is kept.\n\tsglSecond = 2\n\n\t// sglThird is dropped.\n\t//\n\t//sgl:when numbered\n\tsglThird = 3\n\n" +
		"\t// sglFourth holds data.\n\t//\n\t//sgl:data limit\n\tsglFourth = 4\n)\n"
	want := "package generated\n\nconst (\n\tacmeSecond = 2\n\tacmeFourth = 9\n)\n"
	rendered, err := render([]byte(source), prefixVocabulary{prefix: "acme", title: "Acme", undocumented: true},
		&Data{Package: "generated", Flags: map[string]bool{"numbered": false}, Values: map[string]any{"limit": 9}})
	require.NoError(t, err)
	require.Equal(t, want, string(rendered))
}

func TestRenderRejectsAStrayDirectiveInLeftOutDocumentation(t *testing.T) {
	source := "package source\n\ntype sglPair struct {\n\t// left is the first value.\n\t//\n\t//sgl:when kept\n\tleft int\n}\n"
	_, err := render([]byte(source), prefixVocabulary{prefix: "acme", title: "Acme", undocumented: true}, &Data{Package: "generated"})
	require.ErrorIs(t, err, errInvalidSource)
}

func TestFileNamesTheRenderedFile(t *testing.T) {
	file, err := File("limits.go", []byte("package source\n\nconst sglLimit = 1\n"), acme, &Data{Package: "generated"})
	require.NoError(t, err)
	require.Equal(t, "limits.go", file.Name)
	require.Equal(t, "package generated\n\nconst acmeLimit = 1\n", string(file.Content))

	_, err = File("broken.go", []byte("package"), acme, &Data{Package: "generated"})
	require.ErrorContains(t, err, "rendering broken.go")
}

func TestRendererFindsTheLineAnOffsetIsOn(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\nvar a = 1\nvar b = 2")
	state := &renderer{source: source}
	testCases := []struct {
		name      string
		offset    int
		wantStart int
		wantEnd   int
	}{
		{name: "the first line", offset: 3, wantStart: 0, wantEnd: 10},
		{name: "an empty line", offset: 10, wantStart: 10, wantEnd: 11},
		{name: "a middle line", offset: 15, wantStart: 11, wantEnd: 21},
		{name: "the start of a line", offset: 11, wantStart: 11, wantEnd: 21},
		{name: "the last line, which no newline ends", offset: 25, wantStart: 21, wantEnd: len(source)},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.wantStart, state.lineStart(testCase.offset))
			require.Equal(t, testCase.wantEnd, state.lineEnd(testCase.offset))
		})
	}
}

func TestRendererFindsEveryLineInTimeLinearInTheSource(t *testing.T) {
	t.Parallel()

	lines := func(count int) *renderer {
		return &renderer{source: []byte(strings.Repeat("var value = 1\n", count))}
	}
	walk := func(state *renderer) func() {
		return func() {
			for offset := 0; offset < len(state.source); offset = state.lineEnd(offset) {
				_ = state.lineStart(offset)
			}
		}
	}

	scaling.Linear(t, walk(lines(20_000)), walk(lines(80_000)))
}
