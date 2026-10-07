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

package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/postgres/pgext"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/scaling"
)

const hookTypeName = "widget"

var (
	errHookFault         = errors.New("hook fault")
	errAnalysisAbandoned = errors.New("analysis abandoned")
	errWalkStopped       = errors.New("walk stopped")
)

type foreignStatement struct{}

type panickingExtension struct {
	foobarExtension
}

type classifyPanickingExtension struct {
	foobarExtension
}

type cancellingExtension struct {
	foobarExtension
	cancel     context.CancelCauseFunc
	classified *int
}

type overwritingExtension struct {
	foobarExtension
	seen *[]string
}

type foobarExtension struct{}

const (
	statementKindFoobar           = pgext.StatementKindExtensionBase + 0
	statementKindOverridingSelect = pgext.StatementKindExtensionBase + 50
)

type decliningExtension struct{}

type misusingBuiltinKindExtension struct{}

type selectOverridingExtension struct{}

type foobarTableExtension struct {
	foobarExtension
}

func (*foreignStatement) IsParsedStatement() {}

func (panickingExtension) Parse(pgext.ParserContext, statementKind) (*ast.CatalogueMutation, error) {
	panic("extension bug")
}

func (classifyPanickingExtension) Classify([]pgext.Token) statementKind {
	panic(errHookFault)
}

func (extension cancellingExtension) Classify([]pgext.Token) statementKind {
	*extension.classified++
	extension.cancel(errAnalysisAbandoned)
	return 0
}

func (extension overwritingExtension) Classify(tokens []pgext.Token) statementKind {
	*extension.seen = append(*extension.seen, tokens[0].Value)
	tokens[0].Value = "overwritten"
	tokens[0].Kind = pgext.TokenString
	return 0
}

func (foobarExtension) Classify(tokens []pgext.Token) statementKind {
	if len(tokens) < 2 {
		return 0
	}
	if !tokens[0].IsKeyword(pgext.TokenIdentifier, "CREATE") || !tokens[1].IsKeyword(pgext.TokenIdentifier, "FOOBAR") {
		return 0
	}
	return statementKindFoobar
}

func (foobarExtension) Parse(p pgext.ParserContext, kind statementKind) (*ast.CatalogueMutation, error) {
	if kind != statementKindFoobar {
		return nil, nil
	}
	if err := p.ExpectKeyword("CREATE"); err != nil {
		return nil, err
	}
	if err := p.ExpectKeyword("FOOBAR"); err != nil {
		return nil, err
	}
	nameToken := p.Advance()
	return &ast.CatalogueMutation{
		Kind:       ast.MutationCreateTable,
		SchemaName: "public",
		TableName:  nameToken.Value,
		EngineSpecific: map[string]string{
			"FOOBAR": "true",
		},
	}, nil
}

func (decliningExtension) Classify(tokens []pgext.Token) statementKind { return 0 }

func (decliningExtension) Parse(p pgext.ParserContext, kind statementKind) (*ast.CatalogueMutation, error) {
	return nil, nil
}

func (misusingBuiltinKindExtension) Classify(tokens []pgext.Token) statementKind {
	if len(tokens) < 2 {
		return 0
	}
	if !strings.EqualFold(tokens[0].Value, "CREATE") {
		return 0
	}
	if !strings.EqualFold(tokens[1].Value, "NONESUCH") {
		return 0
	}

	return statementKindDropTable
}

func (misusingBuiltinKindExtension) Parse(p pgext.ParserContext, kind statementKind) (*ast.CatalogueMutation, error) {
	return nil, nil
}

func (selectOverridingExtension) Classify(tokens []pgext.Token) statementKind {
	if len(tokens) < 4 {
		return 0
	}
	if !strings.EqualFold(tokens[0].Value, "SELECT") {
		return 0
	}
	if !strings.EqualFold(tokens[1].Value, "magic_func") {
		return 0
	}
	if tokens[2].Kind != tokenLeftParen {
		return 0
	}
	return statementKindOverridingSelect
}

func (selectOverridingExtension) Parse(p pgext.ParserContext, kind statementKind) (*ast.CatalogueMutation, error) {
	if kind != statementKindOverridingSelect {
		return nil, nil
	}
	p.ConsumeRemainder()
	return &ast.CatalogueMutation{
		Kind:           ast.MutationCreateTable,
		TableName:      "magic_target",
		EngineSpecific: map[string]string{"MAGIC": "true"},
	}, nil
}

func (foobarTableExtension) Parse(p pgext.ParserContext, kind statementKind) (*ast.CatalogueMutation, error) {
	if kind != statementKindFoobar {
		return nil, nil
	}
	if err := p.ExpectKeyword("CREATE"); err != nil {
		return nil, err
	}
	if err := p.ExpectKeyword("FOOBAR"); err != nil {
		return nil, err
	}
	if err := p.ExpectKeyword("TABLE"); err != nil {
		return nil, err
	}
	return nil, nil
}

func TestNewPostgresEngine(t *testing.T) {
	t.Parallel()

	t.Run("default construction", func(t *testing.T) {
		t.Parallel()

		engine := New()

		assert.Equal(t, "postgres", engine.Info().Name, "dialect should default to postgres")
		assert.Equal(t, []string{"public"}, engine.Info().SchemaSearchPath, "default schema should be public")
		assert.NotZero(t, engine.Info().Returning, "PostgreSQL supports RETURNING")
		assert.Equal(t, dialect.PlaceholderSigil{Prefix: "$", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered}, engine.Info().Placeholders.Execution, "parameter style should be dollar")
		assert.NotNil(t, engine.BuiltinFunctions(), "function catalogue should not be nil")
		assert.NotNil(t, engine.BuiltinTypes(), "type catalogue should not be nil")
	})

	t.Run("with dialect name option", func(t *testing.T) {
		t.Parallel()

		engine := New(WithName("cockroachdb"))

		assert.Equal(t, "cockroachdb", engine.Info().Name, "dialect should reflect custom name")

		assert.Equal(t, []string{"public"}, engine.Info().SchemaSearchPath)
		assert.NotZero(t, engine.Info().Returning)
	})

	t.Run("with extra types option", func(t *testing.T) {
		t.Parallel()

		extra := map[string]sqlmodel.SQLType{
			"crdb_internal_type": {Category: sqlmodel.TypeCategoryText, EngineName: "crdb_internal_type"},
		}
		engine := New(WithExtraTypes(extra))

		catalogue := engine.BuiltinTypes()
		_, exists := catalogue.Types["crdb_internal_type"]
		assert.True(t, exists, "extra type should appear in the type catalogue")
	})

	t.Run("with extra functions option", func(t *testing.T) {
		t.Parallel()

		engine := New(WithExtraFunctions(func(builder *FunctionCatalogueBuilder) {
			builder.Catalogue.Functions["custom_fn"] = []*sqlmodel.Function{
				{
					ReturnType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"},
				},
			}
		}))

		catalogue := engine.BuiltinFunctions()
		_, exists := catalogue.Functions["custom_fn"]
		assert.True(t, exists, "extra function should appear in the function catalogue")
	})
}

func TestNormaliseTypeName(t *testing.T) {
	t.Parallel()

	engine := New()

	tests := []struct {
		name            string
		input           string
		modifiers       []int
		wantCategory    sqlmodel.SQLTypeCategory
		wantEngineName  string
		wantIsArray     bool
		wantElementName string
	}{

		{
			name:           "uppercase INTEGER normalises to int4",
			input:          "INTEGER",
			wantCategory:   sqlmodel.TypeCategoryInteger,
			wantEngineName: "int4",
		},
		{
			name:           "lowercase integer normalises to int4",
			input:          "integer",
			wantCategory:   sqlmodel.TypeCategoryInteger,
			wantEngineName: "int4",
		},
		{
			name:           "mixed case Integer normalises to int4",
			input:          "Integer",
			wantCategory:   sqlmodel.TypeCategoryInteger,
			wantEngineName: "int4",
		},

		{
			name:           "int alias normalises to int4",
			input:          "int",
			wantCategory:   sqlmodel.TypeCategoryInteger,
			wantEngineName: "int4",
		},
		{
			name:           "int4 stays int4",
			input:          "int4",
			wantCategory:   sqlmodel.TypeCategoryInteger,
			wantEngineName: "int4",
		},
		{
			name:           "bigint normalises to int8",
			input:          "bigint",
			wantCategory:   sqlmodel.TypeCategoryInteger,
			wantEngineName: "int8",
		},
		{
			name:           "int8 stays int8",
			input:          "int8",
			wantCategory:   sqlmodel.TypeCategoryInteger,
			wantEngineName: "int8",
		},
		{
			name:           "smallint normalises to int2",
			input:          "smallint",
			wantCategory:   sqlmodel.TypeCategoryInteger,
			wantEngineName: "int2",
		},
		{
			name:           "int2 stays int2",
			input:          "int2",
			wantCategory:   sqlmodel.TypeCategoryInteger,
			wantEngineName: "int2",
		},

		{
			name:           "serial normalises to int4",
			input:          "serial",
			wantCategory:   sqlmodel.TypeCategoryInteger,
			wantEngineName: "int4",
		},
		{
			name:           "bigserial normalises to int8",
			input:          "bigserial",
			wantCategory:   sqlmodel.TypeCategoryInteger,
			wantEngineName: "int8",
		},
		{
			name:           "smallserial normalises to int2",
			input:          "smallserial",
			wantCategory:   sqlmodel.TypeCategoryInteger,
			wantEngineName: "int2",
		},

		{
			name:           "double precision normalises to float8",
			input:          "double precision",
			wantCategory:   sqlmodel.TypeCategoryFloat,
			wantEngineName: "float8",
		},
		{
			name:           "real normalises to float4",
			input:          "real",
			wantCategory:   sqlmodel.TypeCategoryFloat,
			wantEngineName: "float4",
		},
		{
			name:           "float normalises to float8",
			input:          "float",
			wantCategory:   sqlmodel.TypeCategoryFloat,
			wantEngineName: "float8",
		},
		{
			name:           "float4 stays float4",
			input:          "float4",
			wantCategory:   sqlmodel.TypeCategoryFloat,
			wantEngineName: "float4",
		},
		{
			name:           "float8 stays float8",
			input:          "float8",
			wantCategory:   sqlmodel.TypeCategoryFloat,
			wantEngineName: "float8",
		},

		{
			name:           "character varying normalises to varchar",
			input:          "character varying",
			wantCategory:   sqlmodel.TypeCategoryText,
			wantEngineName: "varchar",
		},
		{
			name:           "varchar normalises to varchar",
			input:          "varchar",
			wantCategory:   sqlmodel.TypeCategoryText,
			wantEngineName: "varchar",
		},
		{
			name:           "text normalises to text",
			input:          "text",
			wantCategory:   sqlmodel.TypeCategoryText,
			wantEngineName: "text",
		},
		{
			name:           "char normalises to char",
			input:          "char",
			wantCategory:   sqlmodel.TypeCategoryText,
			wantEngineName: "char",
		},
		{
			name:           "character normalises to char",
			input:          "character",
			wantCategory:   sqlmodel.TypeCategoryText,
			wantEngineName: "char",
		},

		{
			name:           "timestamp with time zone normalises to timestamptz",
			input:          "timestamp with time zone",
			wantCategory:   sqlmodel.TypeCategoryTemporal,
			wantEngineName: "timestamptz",
		},
		{
			name:           "timestamptz stays timestamptz",
			input:          "timestamptz",
			wantCategory:   sqlmodel.TypeCategoryTemporal,
			wantEngineName: "timestamptz",
		},
		{
			name:           "timestamp without time zone normalises to timestamp",
			input:          "timestamp without time zone",
			wantCategory:   sqlmodel.TypeCategoryTemporal,
			wantEngineName: "timestamp",
		},
		{
			name:           "timestamp normalises to timestamp",
			input:          "timestamp",
			wantCategory:   sqlmodel.TypeCategoryTemporal,
			wantEngineName: "timestamp",
		},
		{
			name:           "date normalises to date",
			input:          "date",
			wantCategory:   sqlmodel.TypeCategoryTemporal,
			wantEngineName: "date",
		},
		{
			name:           "interval normalises to interval",
			input:          "interval",
			wantCategory:   sqlmodel.TypeCategoryTemporal,
			wantEngineName: "interval",
		},

		{
			name:           "boolean normalises to bool",
			input:          "boolean",
			wantCategory:   sqlmodel.TypeCategoryBoolean,
			wantEngineName: "bool",
		},
		{
			name:           "bool stays bool",
			input:          "bool",
			wantCategory:   sqlmodel.TypeCategoryBoolean,
			wantEngineName: "bool",
		},

		{
			name:           "json normalises to json",
			input:          "json",
			wantCategory:   sqlmodel.TypeCategoryJSON,
			wantEngineName: "json",
		},
		{
			name:           "jsonb normalises to jsonb",
			input:          "jsonb",
			wantCategory:   sqlmodel.TypeCategoryJSON,
			wantEngineName: "jsonb",
		},

		{
			name:           "uuid normalises to uuid",
			input:          "uuid",
			wantCategory:   sqlmodel.TypeCategoryUUID,
			wantEngineName: "uuid",
		},

		{
			name:           "bytea normalises to bytea",
			input:          "bytea",
			wantCategory:   sqlmodel.TypeCategoryBinary,
			wantEngineName: "bytea",
		},

		{
			name:           "numeric normalises to numeric",
			input:          "numeric",
			wantCategory:   sqlmodel.TypeCategoryDecimal,
			wantEngineName: "numeric",
		},
		{
			name:           "decimal normalises to numeric",
			input:          "decimal",
			wantCategory:   sqlmodel.TypeCategoryDecimal,
			wantEngineName: "numeric",
		},

		{
			name:            "integer array normalises to int4 array",
			input:           "integer[]",
			wantCategory:    sqlmodel.TypeCategoryArray,
			wantEngineName:  "integer[]",
			wantIsArray:     true,
			wantElementName: "int4",
		},
		{
			name:            "text array normalises to text array",
			input:           "text[]",
			wantCategory:    sqlmodel.TypeCategoryArray,
			wantEngineName:  "text[]",
			wantIsArray:     true,
			wantElementName: "text",
		},

		{
			name:           "inet normalises to inet",
			input:          "inet",
			wantCategory:   sqlmodel.TypeCategoryNetwork,
			wantEngineName: "inet",
		},

		{
			name:           "int4range normalises to int4range",
			input:          "int4range",
			wantCategory:   sqlmodel.TypeCategoryRange,
			wantEngineName: "int4range",
		},

		{
			name:           "unknown type preserves engine name",
			input:          "my_custom_type",
			wantCategory:   sqlmodel.TypeCategoryUnknown,
			wantEngineName: "my_custom_type",
		},
		{
			name:           "unknown type is lowered",
			input:          "MY_CUSTOM_TYPE",
			wantCategory:   sqlmodel.TypeCategoryUnknown,
			wantEngineName: "my_custom_type",
		},

		{
			name:           "empty string defaults to text",
			input:          "",
			wantCategory:   sqlmodel.TypeCategoryText,
			wantEngineName: "text",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := engine.NormaliseTypeName(testCase.input, testCase.modifiers...)

			assert.Equal(t, testCase.wantCategory, got.Category, "category mismatch")
			assert.Equal(t, testCase.wantEngineName, got.EngineName, "engine name mismatch")

			if testCase.wantIsArray {
				require.NotNil(t, got.ElementType, "expected non-nil ElementType for array type")
				assert.Equal(t, testCase.wantElementName, got.ElementType.EngineName, "element engine name mismatch")
			} else {
				assert.Nil(t, got.ElementType, "expected nil ElementType for non-array type")
			}
		})
	}
}

func TestNormaliseTypeName_Modifiers(t *testing.T) {
	t.Parallel()

	engine := New()

	t.Run("numeric with precision and scale", func(t *testing.T) {
		t.Parallel()

		got := engine.NormaliseTypeName("numeric", 10, 2)

		assert.Equal(t, sqlmodel.TypeCategoryDecimal, got.Category)
		assert.Equal(t, "numeric", got.EngineName)
		require.NotNil(t, got.Precision, "precision should be set")
		assert.Equal(t, 10, *got.Precision)
		require.NotNil(t, got.Scale, "scale should be set")
		assert.Equal(t, 2, *got.Scale)
	})

	t.Run("varchar with length", func(t *testing.T) {
		t.Parallel()

		got := engine.NormaliseTypeName("varchar", 255)

		assert.Equal(t, sqlmodel.TypeCategoryText, got.Category)
		assert.Equal(t, "varchar", got.EngineName)
		require.NotNil(t, got.Length, "length should be set")
		assert.Equal(t, 255, *got.Length)
	})

	t.Run("timestamp with precision", func(t *testing.T) {
		t.Parallel()

		got := engine.NormaliseTypeName("timestamp", 6)

		assert.Equal(t, sqlmodel.TypeCategoryTemporal, got.Category)
		assert.Equal(t, "timestamp", got.EngineName)
		require.NotNil(t, got.Precision, "precision should be set")
		assert.Equal(t, 6, *got.Precision)
	})

	t.Run("integer ignores modifiers", func(t *testing.T) {
		t.Parallel()

		got := engine.NormaliseTypeName("integer", 42)

		assert.Equal(t, sqlmodel.TypeCategoryInteger, got.Category)
		assert.Nil(t, got.Precision, "integer should not have precision")
		assert.Nil(t, got.Scale, "integer should not have scale")
		assert.Nil(t, got.Length, "integer should not have length")
	})
}

func TestNormaliseTypeName_Hook(t *testing.T) {
	t.Parallel()

	hookedType := sqlmodel.SQLType{
		Category:   sqlmodel.TypeCategoryText,
		EngineName: "hooked_text",
	}

	engine := New(WithTypeNormaliserHook(func(name string, _ []int) *sqlmodel.SQLType {
		if name == "my_special_type" {
			return &hookedType
		}
		return nil
	}))

	t.Run("hook intercepts matching type", func(t *testing.T) {
		t.Parallel()

		got := engine.NormaliseTypeName("MY_SPECIAL_TYPE")
		assert.Equal(t, hookedType, got)
	})

	t.Run("hook falls through for non-matching type", func(t *testing.T) {
		t.Parallel()

		got := engine.NormaliseTypeName("integer")
		assert.Equal(t, sqlmodel.TypeCategoryInteger, got.Category)
		assert.Equal(t, "int4", got.EngineName)
	})
}

func TestPromoteType(t *testing.T) {
	t.Parallel()

	engine := New()

	int2 := sqlmodel.NewIntegerType("int2", 16, false)
	int4 := sqlmodel.NewIntegerType("int4", 32, false)
	integer8 := sqlmodel.NewIntegerType("int8", 64, false)
	float4 := sqlmodel.NewFloatType("float4", 32)
	float8 := sqlmodel.NewFloatType("float8", 64)
	text := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"}

	tests := []struct {
		name  string
		left  sqlmodel.SQLType
		right sqlmodel.SQLType
		want  sqlmodel.SQLType
	}{

		{
			name:  "int2 + int4 promotes to int4",
			left:  int2,
			right: int4,
			want:  int4,
		},
		{
			name:  "int4 + int2 promotes to int4",
			left:  int4,
			right: int2,
			want:  int4,
		},
		{
			name:  "int4 + int8 promotes to int8",
			left:  int4,
			right: integer8,
			want:  integer8,
		},
		{
			name:  "int8 + int4 promotes to int8",
			left:  integer8,
			right: int4,
			want:  integer8,
		},
		{
			name:  "int2 + int8 promotes to int8",
			left:  int2,
			right: integer8,
			want:  integer8,
		},

		{
			name:  "float4 + float8 promotes to float8",
			left:  float4,
			right: float8,
			want:  float8,
		},
		{
			name:  "float8 + float4 promotes to float8",
			left:  float8,
			right: float4,
			want:  float8,
		},

		{
			name:  "int4 + int4 returns int4",
			left:  int4,
			right: int4,
			want:  int4,
		},
		{
			name:  "float8 + float8 returns float8",
			left:  float8,
			right: float8,
			want:  float8,
		},

		{
			name:  "int4 + float8 returns the float, as a UNION over them does",
			left:  int4,
			right: float8,
			want:  float8,
		},
		{
			name:  "float4 + int4 returns the float, keeping its own width",
			left:  float4,
			right: int4,
			want:  float4,
		},
		{
			name:  "text + int4 returns left (different categories)",
			left:  text,
			right: int4,
			want:  text,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := engine.PromoteType(testCase.left, testCase.right)
			assert.Equal(t, testCase.want.Category, got.Category, "category mismatch")
			assert.Equal(t, testCase.want.EngineName, got.EngineName, "engine name mismatch")
		})
	}
}

func TestPromoteType_Hook(t *testing.T) {
	t.Parallel()

	promoted := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryDecimal, EngineName: "numeric"}

	engine := New(WithPromoteTypeHook(func(left, right sqlmodel.SQLType) *sqlmodel.SQLType {

		if left.Category == sqlmodel.TypeCategoryInteger && right.Category == sqlmodel.TypeCategoryFloat {
			return &promoted
		}
		return nil
	}))

	t.Run("hook intercepts cross-category promotion", func(t *testing.T) {
		t.Parallel()

		int4 := sqlmodel.NewIntegerType("int4", 32, false)
		float8 := sqlmodel.NewFloatType("float8", 64)

		got := engine.PromoteType(int4, float8)
		assert.Equal(t, promoted, got)
	})

	t.Run("hook falls through for same-category promotion", func(t *testing.T) {
		t.Parallel()

		int2 := sqlmodel.NewIntegerType("int2", 16, false)
		integer8 := sqlmodel.NewIntegerType("int8", 64, false)

		got := engine.PromoteType(int2, integer8)
		assert.Equal(t, sqlmodel.TypeCategoryInteger, got.Category)
		assert.Equal(t, "int8", got.EngineName)
	})
}

func TestCanImplicitCast(t *testing.T) {
	t.Parallel()

	engine := New()

	tests := []struct {
		name string
		from sqlmodel.SQLTypeCategory
		to   sqlmodel.SQLTypeCategory
		want bool
	}{

		{
			name: "integer to float is allowed",
			from: sqlmodel.TypeCategoryInteger,
			to:   sqlmodel.TypeCategoryFloat,
			want: true,
		},
		{
			name: "integer to decimal is allowed",
			from: sqlmodel.TypeCategoryInteger,
			to:   sqlmodel.TypeCategoryDecimal,
			want: true,
		},
		{
			name: "float to decimal needs an assignment, not an implicit cast",
			from: sqlmodel.TypeCategoryFloat,
			to:   sqlmodel.TypeCategoryDecimal,
			want: false,
		},
		{
			name: "decimal to float is allowed",
			from: sqlmodel.TypeCategoryDecimal,
			to:   sqlmodel.TypeCategoryFloat,
			want: true,
		},
		{
			name: "text to text is allowed",
			from: sqlmodel.TypeCategoryText,
			to:   sqlmodel.TypeCategoryText,
			want: true,
		},

		{
			name: "float to integer is not allowed",
			from: sqlmodel.TypeCategoryFloat,
			to:   sqlmodel.TypeCategoryInteger,
			want: false,
		},
		{
			name: "boolean to integer is not allowed",
			from: sqlmodel.TypeCategoryBoolean,
			to:   sqlmodel.TypeCategoryInteger,
			want: false,
		},
		{
			name: "text to integer is not allowed",
			from: sqlmodel.TypeCategoryText,
			to:   sqlmodel.TypeCategoryInteger,
			want: false,
		},
		{
			name: "integer to text is not allowed",
			from: sqlmodel.TypeCategoryInteger,
			to:   sqlmodel.TypeCategoryText,
			want: false,
		},
		{
			name: "integer to boolean is not allowed",
			from: sqlmodel.TypeCategoryInteger,
			to:   sqlmodel.TypeCategoryBoolean,
			want: false,
		},
		{
			name: "decimal to integer is not allowed",
			from: sqlmodel.TypeCategoryDecimal,
			to:   sqlmodel.TypeCategoryInteger,
			want: false,
		},
		{
			name: "json to text is not allowed",
			from: sqlmodel.TypeCategoryJSON,
			to:   sqlmodel.TypeCategoryText,
			want: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := engine.CanImplicitCast(sqlmodel.NewSQLType(testCase.from, ""), sqlmodel.NewSQLType(testCase.to, ""))
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestCanImplicitCast_Hook(t *testing.T) {
	t.Parallel()

	engine := New(WithImplicitCastHook(func(from, to sqlmodel.SQLType) *bool {

		if from.Category == sqlmodel.TypeCategoryBoolean && to.Category == sqlmodel.TypeCategoryInteger {
			return new(true)
		}
		return nil
	}))

	t.Run("hook allows normally disallowed cast", func(t *testing.T) {
		t.Parallel()

		assert.True(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryBoolean, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "")))
	})

	t.Run("hook falls through for default rules", func(t *testing.T) {
		t.Parallel()

		assert.True(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryFloat, "")))
		assert.False(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryFloat, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "")))
	})
}

func TestSupportedExpressions(t *testing.T) {
	t.Parallel()

	engine := New()
	features := engine.Info().Expressions

	expectedFlags := []struct {
		name string
		flag ast.ExpressionFeature
	}{
		{name: "binary arithmetic", flag: ast.SQLFeatureBinaryArithmetic},
		{name: "binary comparison", flag: ast.SQLFeatureBinaryComparison},
		{name: "string concat", flag: ast.SQLFeatureStringConcat},
		{name: "case when", flag: ast.SQLFeatureCaseWhen},
		{name: "scalar subquery", flag: ast.SQLFeatureScalarSubquery},
		{name: "exists", flag: ast.SQLFeatureExists},
		{name: "is null", flag: ast.SQLFeatureIsNull},
		{name: "in list", flag: ast.SQLFeatureInList},
		{name: "between", flag: ast.SQLFeatureBetween},
		{name: "logical op", flag: ast.SQLFeatureLogicalOp},
		{name: "unary op", flag: ast.SQLFeatureUnaryOp},
		{name: "window function", flag: ast.SQLFeatureWindowFunction},
		{name: "array subscript", flag: ast.SQLFeatureArraySubscript},
		{name: "json op", flag: ast.SQLFeatureJSONOp},
		{name: "pattern match", flag: ast.SQLFeaturePatternMatch},
		{name: "bitwise op", flag: ast.SQLFeatureBitwiseOp},
		{name: "composite field access", flag: ast.SQLFeatureStructFieldAccess},
	}

	for _, testCase := range expectedFlags {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.True(t, features.Has(testCase.flag), "expected %s to be supported", testCase.name)
		})
	}

	t.Run("lambda not supported", func(t *testing.T) {
		t.Parallel()
		assert.False(t, features.Has(ast.SQLFeatureLambda), "lambda should not be supported")
	})
}

func TestParameterStyle(t *testing.T) {
	t.Parallel()

	engine := New()

	assert.Equal(t, dialect.PlaceholderSigil{Prefix: "$", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered}, engine.Info().Placeholders.Execution)
}

func TestDefaultSchema(t *testing.T) {
	t.Parallel()

	engine := New()

	assert.Equal(t, []string{"public"}, engine.Info().SchemaSearchPath)
}

func TestSupportsReturning(t *testing.T) {
	t.Parallel()

	engine := New()

	assert.NotZero(t, engine.Info().Returning)
}

func TestDialect(t *testing.T) {
	t.Parallel()

	t.Run("default dialect is postgres", func(t *testing.T) {
		t.Parallel()

		engine := New()
		assert.Equal(t, "postgres", engine.Info().Name)
	})

	t.Run("custom dialect name", func(t *testing.T) {
		t.Parallel()

		engine := New(WithName("yugabytedb"))
		assert.Equal(t, "yugabytedb", engine.Info().Name)
	})
}

func TestBuiltinFunctions(t *testing.T) {
	t.Parallel()

	engine := New()
	catalogue := engine.BuiltinFunctions()

	require.NotNil(t, catalogue, "function catalogue should not be nil")
	require.NotNil(t, catalogue.Functions, "function map should not be nil")

	expectedFunctions := []string{
		"count",
		"array_agg",
		"string_agg",
		"bool_and",
		"bool_or",
		"concat",
		"concat_ws",
		"json_agg",
		"jsonb_agg",
		"json_build_object",
		"jsonb_build_object",
		"stddev",
		"variance",
	}

	for _, name := range expectedFunctions {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			signatures, exists := catalogue.Functions[name]
			assert.True(t, exists, "expected built-in function %q to exist", name)
			if exists {
				assert.NotEmpty(t, signatures, "expected at least one signature for %q", name)
			}
		})
	}
}

func TestBuiltinTypes(t *testing.T) {
	t.Parallel()

	engine := New()
	catalogue := engine.BuiltinTypes()

	require.NotNil(t, catalogue, "type catalogue should not be nil")
	require.NotNil(t, catalogue.Types, "type map should not be nil")

	expectedTypes := []struct {
		name     string
		category sqlmodel.SQLTypeCategory
	}{
		{name: "int4", category: sqlmodel.TypeCategoryInteger},
		{name: "int2", category: sqlmodel.TypeCategoryInteger},
		{name: "int8", category: sqlmodel.TypeCategoryInteger},
		{name: "float4", category: sqlmodel.TypeCategoryFloat},
		{name: "float8", category: sqlmodel.TypeCategoryFloat},
		{name: "numeric", category: sqlmodel.TypeCategoryDecimal},
		{name: "bool", category: sqlmodel.TypeCategoryBoolean},
		{name: "boolean", category: sqlmodel.TypeCategoryBoolean},
		{name: "text", category: sqlmodel.TypeCategoryText},
		{name: "varchar", category: sqlmodel.TypeCategoryText},
		{name: "bytea", category: sqlmodel.TypeCategoryBinary},
		{name: "timestamp", category: sqlmodel.TypeCategoryTemporal},
		{name: "timestamptz", category: sqlmodel.TypeCategoryTemporal},
		{name: "date", category: sqlmodel.TypeCategoryTemporal},
		{name: "json", category: sqlmodel.TypeCategoryJSON},
		{name: "jsonb", category: sqlmodel.TypeCategoryJSON},
		{name: "uuid", category: sqlmodel.TypeCategoryUUID},
		{name: "inet", category: sqlmodel.TypeCategoryNetwork},
		{name: "int4range", category: sqlmodel.TypeCategoryRange},
		{name: "point", category: sqlmodel.TypeCategoryGeometric},
	}

	for _, testCase := range expectedTypes {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			sqlType, exists := catalogue.Types[testCase.name]
			require.True(t, exists, "expected type %q to exist in catalogue", testCase.name)
			assert.Equal(t, testCase.category, sqlType.Category, "category mismatch for type %q", testCase.name)
		})
	}
}

func TestTableValuedFunctionColumns(t *testing.T) {
	t.Parallel()

	engine := New()

	t.Run("generate_series returns expected columns", func(t *testing.T) {
		t.Parallel()

		columns := engine.TableValuedFunctionColumns("generate_series")
		require.NotNil(t, columns, "generate_series should return columns")
		require.Len(t, columns, 1, "generate_series should have one column")
		assert.Equal(t, "generate_series", columns[0].Name)
		assert.Equal(t, sqlmodel.TypeCategoryInteger, columns[0].SQLType.Category)
		assert.Equal(t, "int4", columns[0].SQLType.EngineName)
		assert.False(t, columns[0].Nullable)
	})

	t.Run("json_each returns key and value columns", func(t *testing.T) {
		t.Parallel()

		columns := engine.TableValuedFunctionColumns("json_each")
		require.NotNil(t, columns, "json_each should return columns")
		require.Len(t, columns, 2, "json_each should have two columns")

		assert.Equal(t, "key", columns[0].Name)
		assert.Equal(t, sqlmodel.TypeCategoryText, columns[0].SQLType.Category)
		assert.False(t, columns[0].Nullable)

		assert.Equal(t, "value", columns[1].Name)
		assert.Equal(t, sqlmodel.TypeCategoryJSON, columns[1].SQLType.Category)
		assert.True(t, columns[1].Nullable)
	})

	t.Run("unnest returns unknown-type column", func(t *testing.T) {
		t.Parallel()

		columns := engine.TableValuedFunctionColumns("unnest")
		require.NotNil(t, columns, "unnest should return columns")
		require.Len(t, columns, 1)
		assert.Equal(t, sqlmodel.TypeCategoryUnknown, columns[0].SQLType.Category)
	})

	t.Run("unknown function returns nil", func(t *testing.T) {
		t.Parallel()

		columns := engine.TableValuedFunctionColumns("nonexistent_function")
		assert.Nil(t, columns, "unknown function should return nil")
	})

	t.Run("returned slice is a copy", func(t *testing.T) {
		t.Parallel()

		columns1 := engine.TableValuedFunctionColumns("generate_series")
		columns2 := engine.TableValuedFunctionColumns("generate_series")

		require.NotNil(t, columns1)
		require.NotNil(t, columns2)

		columns1[0].Name = "modified"
		assert.NotEqual(t, columns1[0].Name, columns2[0].Name,
			"returned slices should be independent copies")
	})
}

func TestCommentStyle(t *testing.T) {
	t.Parallel()

	engine := New()
	style := engine.Info().Comments

	assert.Equal(t, "--", style.LinePrefix, "comment style should use standard SQL line prefix")
}

func TestSupportedDirectivePrefixes(t *testing.T) {
	t.Parallel()

	engine := New()
	prefixes := engine.Info().Placeholders.Sigils

	require.Len(t, prefixes, 2, "PostgreSQL should support two directive prefixes")

	assert.Equal(t, "$", prefixes[0].Prefix)
	assert.Equal(t, dialect.PlaceholderNumbered, prefixes[0].Kind, "dollar prefix is numbered, not named")

	assert.Equal(t, ":", prefixes[1].Prefix)
	assert.Equal(t, dialect.PlaceholderNamed, prefixes[1].Kind, "colon prefix is named")
}

func TestPromoteTypeComparesWidths(t *testing.T) {
	t.Parallel()

	engine := New()
	tests := []struct {
		name     string
		left     string
		right    string
		expected string
	}{
		{name: "a wider right integer wins", left: "smallint", right: "bigint", expected: "int8"},
		{name: "a narrower right integer loses", left: "bigint", right: "integer", expected: "int8"},
		{name: "equal widths keep the left", left: "int4", right: "serial", expected: "int4"},
		{name: "a double outranks a real", left: "real", right: "double precision", expected: "float8"},
		{name: "an integer meeting a real gives the real", left: "integer", right: "real", expected: "float4"},
		{name: "a decimal meeting an integer gives the decimal", left: "integer", right: "numeric", expected: "numeric"},
		{name: "a decimal meeting a double gives the double", left: "numeric", right: "double precision", expected: "float8"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			promoted := engine.PromoteType(engine.NormaliseTypeName(testCase.left), engine.NormaliseTypeName(testCase.right))
			assert.Equal(t, testCase.expected, promoted.EngineName)
		})
	}
}

func TestAnalyseQuery_ReportsEmptyStatementAsSyntaxError(t *testing.T) {
	t.Parallel()

	engine := New()
	statement := ast.ParsedStatement{
		Raw: &parsedStatement{
			tokens: []token{{Kind: tokenEOF}},
			kind:   statementKindSelect,
		},
	}

	analysis, err := engine.AnalyseQuery(t.Context(), nil, statement)

	require.Error(t, err)
	require.Nil(t, analysis)
	assert.Contains(t, err.Error(), "expected keyword SELECT")
	assert.NotContains(t, err.Error(), "panic")
}

func TestTableValuedFunctionColumnsFromCatalogueFollowTheSearchPath(t *testing.T) {
	t.Parallel()

	makeSchema := func(name, compositeColumn string) *sqlmodel.Schema {
		compositeName := name + "_row"
		return &sqlmodel.Schema{
			Name: name,
			CompositeTypes: map[string]*sqlmodel.CompositeType{
				compositeName: {
					Name: compositeName,
					Fields: []sqlmodel.Column{
						{Name: compositeColumn, SQLType: sqlmodel.SQLType{EngineName: "text"}},
					},
				},
			},
			Functions: map[string][]*sqlmodel.Function{
				"shared_fn": {
					nil,
					{
						Name:       "shared_fn",
						Schema:     name,
						ReturnsSet: true,
						ReturnType: sqlmodel.SQLType{
							EngineName: compositeName,
							Category:   sqlmodel.TypeCategoryComposite,
							Schema:     name,
						},
					},
				},
			},
		}
	}
	catalogueOnPath := func(searchPath ...string) *sqlmodel.Catalogue {
		return &sqlmodel.Catalogue{
			Schemas: map[string]*sqlmodel.Schema{
				"omega":  makeSchema("omega", "omega_col"),
				"alpha":  makeSchema("alpha", "alpha_col"),
				"public": nil,
			},
			DefaultSchema: "public",
			SearchPath:    searchPath,
		}
	}

	testCases := []struct {
		catalogue  *sqlmodel.Catalogue
		name       string
		function   string
		wantColumn string
	}{
		{name: "the first schema on the path wins", catalogue: catalogueOnPath("omega", "alpha"), function: "shared_fn", wantColumn: "omega_col"},
		{name: "the path order decides, not the names", catalogue: catalogueOnPath("alpha", "omega"), function: "shared_fn", wantColumn: "alpha_col"},
		{name: "a schema missing from the catalogue is passed over", catalogue: catalogueOnPath("missing", "omega"), function: "shared_fn", wantColumn: "omega_col"},
		{name: "a schema off the path is not searched", catalogue: catalogueOnPath("public"), function: "shared_fn", wantColumn: ""},
		{name: "a qualified name searches its schema alone", catalogue: catalogueOnPath("omega"), function: "alpha.shared_fn", wantColumn: "alpha_col"},
		{name: "a qualified name in a schema the catalogue lacks", catalogue: catalogueOnPath("omega"), function: "missing.shared_fn", wantColumn: ""},
		{name: "no catalogue", catalogue: nil, function: "shared_fn", wantColumn: ""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			columns := catalogueFunctionColumns(t, New(), testCase.catalogue, testCase.function)

			if testCase.wantColumn == "" {
				require.Empty(t, columns)
				return
			}
			require.Len(t, columns, 1)
			require.Equal(t, testCase.wantColumn, columns[0].Name)
		})
	}
}

func TestApplyDDL_ReportsEmptyStatementAsSyntaxError(t *testing.T) {
	t.Parallel()

	engine := New()
	statement := ast.ParsedStatement{
		Raw: &parsedStatement{
			tokens: []token{{Kind: tokenEOF}},
			kind:   statementKindCreateTable,
		},
	}

	mutation, err := engine.ApplyDDL(t.Context(), statement)

	require.Error(t, err)
	require.Nil(t, mutation)
	assert.Contains(t, err.Error(), "expected keyword CREATE")
	assert.NotContains(t, err.Error(), "panic")
}

func TestEntryPointsReturnAPanicAsAPanicErrorAndLogItsStackOnce(t *testing.T) {
	t.Parallel()

	dates := []sqlmodel.SQLType{builtinTypeMap["date"], builtinTypeMap["date"], builtinTypeMap["interval"]}
	testCases := []struct {
		call        func(ctx context.Context) error
		name        string
		operation   string
		wantMessage string
	}{
		{
			name:        "parse statements with an extension whose Classify panics",
			operation:   "postgres: parse statements",
			wantMessage: "postgres: parse statements: panic: hook fault",
			call: func(ctx context.Context) error {
				_, err := New(WithStatementExtensions(classifyPanickingExtension{})).ParseStatements(ctx, "SELECT 1")
				return err
			},
		},
		{
			name:        "apply DDL with a type hook that panics",
			operation:   "postgres: apply DDL",
			wantMessage: "postgres: apply DDL: panic: hook fault",
			call: func(ctx context.Context) error {
				_, err := applyWithContext(ctx, New(panicOnType(hookTypeName)), "CREATE TABLE t (c widget)")
				return err
			},
		},
		{
			name:        "apply DDL with an extension whose Parse panics",
			operation:   "postgres: apply DDL",
			wantMessage: "postgres: apply DDL: panic: extension bug",
			call: func(ctx context.Context) error {
				_, err := applyWithContext(ctx, New(WithStatementExtensions(panickingExtension{})), "CREATE FOOBAR widgets")
				return err
			},
		},
		{
			name:        "analyse query with a type hook that panics",
			operation:   "postgres: analyse query",
			wantMessage: "postgres: analyse query: panic: hook fault",
			call: func(ctx context.Context) error {
				_, err := analyseWithContext(ctx, New(panicOnType(hookTypeName)), "SELECT 1 WHERE $1::widget = 1")
				return err
			},
		},
		{
			name:        "resolve function call with a type hook that panics",
			operation:   "postgres: resolve function call",
			wantMessage: "postgres: resolve function call: panic: hook fault",
			call: func(ctx context.Context) error {
				_, err := New(panicOnType("timestamptz")).ResolveFunctionCall(ctx, nil, "generate_series", "", dates)
				return err
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, logs := newLoggingContext(t)
			err := testCase.call(ctx)

			require.EqualError(t, err, testCase.wantMessage)
			requireRecoveredPanic(t, err, logs, testCase.operation)
		})
	}
}

func TestAnalysisStopsWithTheContextErrorOnceTheContextIsCancelled(t *testing.T) {
	t.Parallel()

	manyColumns := strings.Repeat(", 1", 2_000)
	var manyColumnDefinitions strings.Builder
	for index := range 2_000 {
		manyColumnDefinitions.WriteString(", c" + strconv.Itoa(index) + " int")
	}
	testCases := []struct {
		call    func(ctx context.Context, engine *Dialect) error
		name    string
		wantErr error
	}{
		{
			name:    "a context cancelled before the analysis",
			wantErr: context.Canceled,
			call: func(ctx context.Context, engine *Dialect) error {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				statements, err := engine.ParseStatements(ctx, "SELECT 1")
				require.NoError(t, err)
				_, err = engine.AnalyseQuery(cancelled, nil, statements[0])
				return err
			},
		},
		{
			name:    "a context cancelled while a query is walked",
			wantErr: errAnalysisAbandoned,
			call: func(ctx context.Context, engine *Dialect) error {
				_, err := analyseWithContext(ctx, engine, "SELECT $1::widget"+manyColumns)
				return err
			},
		},
		{
			name:    "a context cancelled while a DDL statement is walked",
			wantErr: errAnalysisAbandoned,
			call: func(ctx context.Context, engine *Dialect) error {
				_, err := applyWithContext(ctx, engine, "CREATE TABLE t (c widget"+manyColumnDefinitions.String()+")")
				return err
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			loggingContext, logs := newLoggingContext(t)
			ctx, cancel := context.WithCancelCause(loggingContext)
			defer cancel(nil)
			engine := New(cancelOnType(hookTypeName, cancel))

			err := testCase.call(ctx, engine)

			require.ErrorIs(t, err, testCase.wantErr)
			assert.NotErrorIs(t, err, fault.ErrPanic)
			assert.Empty(t, loggedRecords(t, logs))
		})
	}
}

func TestEntryPointsRefuseAStatementParsedByAnotherDialect(t *testing.T) {
	t.Parallel()

	engine := New()
	testCases := []struct {
		raw  ast.ParsedStatementRaw
		name string
	}{
		{name: "no statement", raw: nil},
		{name: "another dialect's statement", raw: &foreignStatement{}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			statement := ast.ParsedStatement{Raw: testCase.raw, Location: 0, Length: 0}

			mutation, applyErr := engine.ApplyDDL(t.Context(), statement)
			analysis, analyseErr := engine.AnalyseQuery(t.Context(), nil, statement)

			assert.Nil(t, mutation)
			assert.Nil(t, analysis)
			require.ErrorIs(t, applyErr, dialect.ErrForeignStatement)
			require.ErrorIs(t, analyseErr, dialect.ErrForeignStatement)
		})
	}
}

func TestStatementParsersWalkWithTheInterruptTheyAreGiven(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		read func(p *parser)
		name string
	}{
		{name: "the statement's parser", read: func(p *parser) { p.Peek() }},
		{name: "a child parser", read: func(p *parser) { p.newChildParser(p.Tokens).Peek() }},
		{name: "a common table expression's parser", read: func(p *parser) { p.analyseCTEBody(p.Tokens) }},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			statementParser := New().newStatementParser(stopEveryWalk, parsedStatementOf(t, "SELECT 1"))

			err := walkUnderRecover(t.Context(), func() { testCase.read(statementParser) })

			require.ErrorIs(t, err, errWalkStopped)
		})
	}
}

func stopEveryWalk() error {
	return errWalkStopped
}

func walkUnderRecover(ctx context.Context, read func()) (err error) {
	defer func() { dialectkit.Recover[struct{}](ctx, "postgres: test walk", recover(), nil, &err) }()

	read()
	return nil
}

func parsedStatementOf(t *testing.T, sql string) *parsedStatement {
	t.Helper()

	statements, err := New().ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.Len(t, statements, 1)
	parsed, ok := statements[0].Raw.(*parsedStatement)
	require.True(t, ok)
	return parsed
}

func panicOnType(typeName string) Option {
	return WithTypeNormaliserHook(func(name string, _ []int) *sqlmodel.SQLType {
		if name == typeName {
			panic(errHookFault)
		}
		return nil
	})
}

func cancelOnType(typeName string, cancel context.CancelCauseFunc) Option {
	return WithTypeNormaliserHook(func(name string, _ []int) *sqlmodel.SQLType {
		if name == typeName {
			cancel(errAnalysisAbandoned)
		}
		return nil
	})
}

func newLoggingContext(t *testing.T) (context.Context, *bytes.Buffer) {
	t.Helper()

	logs := &bytes.Buffer{}
	return logging.ContextWithLogger(t.Context(), slog.New(slog.NewJSONHandler(logs, nil))), logs
}

func loggedRecords(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()

	var records []map[string]any
	decoder := json.NewDecoder(logs)
	for decoder.More() {
		record := map[string]any{}
		require.NoError(t, decoder.Decode(&record))
		records = append(records, record)
	}
	return records
}

func requireRecoveredPanic(t *testing.T, err error, logs *bytes.Buffer, operation string) {
	t.Helper()

	panicError, ok := errors.AsType[*dialectkit.PanicError](err)
	require.True(t, ok, "want a *dialectkit.PanicError, got %v", err)
	assert.Equal(t, operation, panicError.Operation)
	assert.ErrorIs(t, err, fault.ErrPanic)
	assert.NotContains(t, err.Error(), "goroutine")
	records := loggedRecords(t, logs)
	require.Len(t, records, 1)
	assert.Equal(t, slog.LevelWarn.String(), records[0][slog.LevelKey])
	assert.Equal(t, operation, records[0]["operation"])
	assert.Contains(t, records[0]["stack"], "goroutine")
}

func applyWithContext(ctx context.Context, engine *Dialect, sql string) (*ast.CatalogueMutation, error) {
	statements, err := engine.ParseStatements(ctx, sql)
	if err != nil {
		return nil, err
	}
	if len(statements) == 0 {
		return nil, errors.New("no statement parsed")
	}
	return engine.ApplyDDL(ctx, statements[0])
}

func analyseWithContext(ctx context.Context, engine *Dialect, sql string) (*ast.RawQueryAnalysis, error) {
	statements, err := engine.ParseStatements(ctx, sql)
	if err != nil {
		return nil, err
	}
	if len(statements) == 0 {
		return nil, errors.New("no statement parsed")
	}
	return engine.AnalyseQuery(ctx, newPostgresCatalogue(), statements[0])
}

func TestWithMaxTokensPerStatement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		limit int
		want  int
	}{
		{name: "positive limit is applied", limit: 50, want: 50},
		{name: "zero keeps the default", limit: 0, want: dialectkit.DefaultMaxTokensPerStatement},
		{name: "negative keeps the default", limit: -1, want: dialectkit.DefaultMaxTokensPerStatement},
		{name: "a limit above the ceiling is held at the ceiling", limit: dialectkit.MaxTokensPerStatementCeiling + 1,
			want: dialectkit.MaxTokensPerStatementCeiling},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(WithMaxTokensPerStatement(testCase.limit))

			assert.Equal(t, testCase.want, engine.settings.Limits.TokensPerStatement())
		})
	}
}

func TestTokenBudgetRejectsOversizedStatements(t *testing.T) {
	t.Parallel()

	longSelect := "SELECT " + strings.TrimSuffix(strings.Repeat("1, ", 40), ", ")
	longTable := "CREATE TABLE t (" + strings.TrimSuffix(strings.Repeat("c int, ", 20), ", ") + ")"

	testCases := []struct {
		name    string
		sql     string
		isDDL   bool
		wantErr bool
	}{
		{name: "analysed query over budget", sql: longSelect, wantErr: true},
		{name: "analysed query within budget", sql: "SELECT 1", wantErr: false},
		{name: "handled DDL over budget", sql: longTable, isDDL: true, wantErr: true},
		{name: "handled DDL within budget", sql: "CREATE TABLE t (id int)", isDDL: true, wantErr: false},
		{name: "unhandled statement in a migration is not charged", sql: "INSERT INTO t VALUES " +
			strings.TrimSuffix(strings.Repeat("(1), ", 40), ", "), isDDL: true, wantErr: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(WithMaxTokensPerStatement(30))
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)

			if testCase.isDDL {
				_, err = engine.ApplyDDL(t.Context(), statements[0])
			} else {
				_, err = engine.AnalyseQuery(t.Context(), nil, statements[0])
			}

			if !testCase.wantErr {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.ErrorIs(t, err, dialectkit.ErrTokenBudget)
			assert.Contains(t, err.Error(), "more than 30 tokens")
		})
	}
}

func TestDefaultTokenBudgetAcceptsLargeInLists(t *testing.T) {
	t.Parallel()

	var builder strings.Builder
	builder.WriteString("SELECT id FROM users WHERE id IN (")
	for index := range 20_000 {
		if index > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString("$")
		builder.WriteString(strconv.Itoa(index + 1))
	}
	builder.WriteString(")")

	analysis := analyseQuery(t, newPostgresCatalogue(), builder.String())

	assert.Len(t, analysis.ParameterReferences, 20_000)
}

func TestAnalyseQueryHandlesStatementsWithoutAnalyser(t *testing.T) {
	t.Parallel()

	engine := New()

	t.Run("unexpected raw statement type is an error", func(t *testing.T) {
		t.Parallel()

		_, err := engine.AnalyseQuery(t.Context(), nil, ast.ParsedStatement{Raw: nil})

		require.ErrorIs(t, err, dialect.ErrForeignStatement)
	})

	t.Run("DDL kind yields an empty analysis", func(t *testing.T) {
		t.Parallel()

		statements, err := engine.ParseStatements(t.Context(), "CREATE TABLE t (id int)")
		require.NoError(t, err)

		analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])

		require.NoError(t, err)
		assert.Equal(t, &ast.RawQueryAnalysis{}, analysis)
	})
}

func TestExtraTypesReachTheTypeNormaliser(t *testing.T) {
	t.Parallel()

	engine := New(WithExtraTypes(map[string]sqlmodel.SQLType{
		"MyType":  sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "mytype"),
		"integer": sqlmodel.NewIntegerType("int8", 64, false),
	}))

	testCases := []struct {
		name         string
		typeName     string
		wantCategory sqlmodel.SQLTypeCategory
		wantEngine   string
	}{
		{name: "mixed-case key, lower-case lookup", typeName: "mytype", wantCategory: sqlmodel.TypeCategoryText, wantEngine: "mytype"},
		{name: "upper-case lookup", typeName: "MYTYPE", wantCategory: sqlmodel.TypeCategoryText, wantEngine: "mytype"},
		{name: "extra type replaces a built-in", typeName: "integer", wantCategory: sqlmodel.TypeCategoryInteger, wantEngine: "int8"},
		{name: "built-in still resolves", typeName: "text", wantCategory: sqlmodel.TypeCategoryText, wantEngine: "text"},
		{name: "array of an extra type", typeName: "mytype[]", wantCategory: sqlmodel.TypeCategoryArray, wantEngine: "mytype[]"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolved := engine.NormaliseTypeName(testCase.typeName)
			assert.Equal(t, testCase.wantCategory, resolved.Category)
			assert.Equal(t, testCase.wantEngine, resolved.EngineName)
		})
	}

	assert.Contains(t, engine.BuiltinTypes().Types, "mytype")
}

func TestExtraTypesReachColumnsAndCasts(t *testing.T) {
	t.Parallel()

	engine := New(WithExtraTypes(map[string]sqlmodel.SQLType{"citext2": sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "citext2")}))

	statements, err := engine.ParseStatements(t.Context(), "CREATE TABLE t (name citext2 NOT NULL)")
	require.NoError(t, err)
	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.Len(t, mutation.Columns, 1)
	assert.Equal(t, sqlmodel.TypeCategoryText, mutation.Columns[0].SQLType.Category)

	statements, err = engine.ParseStatements(t.Context(), "SELECT $1::citext2 AS name")
	require.NoError(t, err)
	analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])
	require.NoError(t, err)
	require.Len(t, analysis.ParameterReferences, 1)
	require.NotNil(t, analysis.ParameterReferences[0].CastType)
	assert.Equal(t, sqlmodel.TypeCategoryText, analysis.ParameterReferences[0].CastType.Category)
}

func TestOptionsAreAdditive(t *testing.T) {
	t.Parallel()

	declining := func(string, []int) *sqlmodel.SQLType { return nil }
	first := func(name string, _ []int) *sqlmodel.SQLType {
		if name == "shared" || name == "first_only" {
			return new(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "first"))
		}
		return nil
	}
	second := func(name string, _ []int) *sqlmodel.SQLType {
		if name == "shared" {
			return new(sqlmodel.NewSQLType(sqlmodel.TypeCategoryBoolean, "second"))
		}
		return nil
	}
	engine := New(
		WithExtraTypes(map[string]sqlmodel.SQLType{"alpha": sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "alpha")}),
		WithExtraTypes(map[string]sqlmodel.SQLType{"beta": sqlmodel.NewSQLType(sqlmodel.TypeCategoryUUID, "beta")}),
		WithExtraFunctions(func(builder *FunctionCatalogueBuilder) {
			builder.NeverNull("first_function", nil, builder.Text())
		}),
		WithExtraFunctions(func(builder *FunctionCatalogueBuilder) {
			builder.NeverNull("second_function", nil, builder.Type("beta"))
		}),
		WithTypeNormaliserHook(first),
		WithTypeNormaliserHook(second),
		WithTypeNormaliserHook(declining),
		WithImplicitCastHook(func(sqlmodel.SQLType, sqlmodel.SQLType) *bool { return new(true) }),
		WithImplicitCastHook(func(sqlmodel.SQLType, sqlmodel.SQLType) *bool { return nil }),
		WithPromoteTypeHook(func(sqlmodel.SQLType, sqlmodel.SQLType) *sqlmodel.SQLType {
			return new(sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "numeric"))
		}),
	)

	assert.Equal(t, sqlmodel.TypeCategoryText, engine.NormaliseTypeName("alpha").Category)
	assert.Equal(t, sqlmodel.TypeCategoryUUID, engine.NormaliseTypeName("beta").Category)
	assert.Equal(t, "second", engine.NormaliseTypeName("shared").EngineName, "the most recent hook is consulted first")
	assert.Equal(t, "first", engine.NormaliseTypeName("first_only").EngineName, "a declining hook falls through")

	functions := engine.BuiltinFunctions().Functions
	require.Len(t, functions["first_function"], 1)
	require.Len(t, functions["second_function"], 1)
	assert.Equal(t, sqlmodel.TypeCategoryUUID, functions["second_function"][0].ReturnType.Category)

	assert.True(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryUUID, "")))
	assert.Equal(t, sqlmodel.TypeCategoryDecimal,
		engine.PromoteType(sqlmodel.NewIntegerType("int4", 32, false), sqlmodel.NewIntegerType("int8", 64, false)).Category)
}

func TestLimitAndSyntaxErrorsWrapTheGeneratorSentinels(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		engine    *Dialect
		sql       string
		sentinel  error
		wantLimit bool
	}{
		{name: "token budget", engine: New(WithMaxTokensPerStatement(3)), sql: "SELECT id FROM users WHERE id = 1", sentinel: dialectkit.ErrTokenBudget, wantLimit: true},
		{name: "analysis depth", engine: New(WithMaxParseDepth(2)), sql: "SELECT id FROM (SELECT id FROM (SELECT id FROM (SELECT 1 AS id) a) b) c", sentinel: dialectkit.ErrParseDepth, wantLimit: true},
		{name: "expression depth", engine: New(WithMaxParseDepth(3)), sql: "SELECT ((((((1))))))", sentinel: dialectkit.ErrParseDepth, wantLimit: true},
		{name: "syntax error", engine: New(), sql: "SELECT id FROM users WHERE", sentinel: fault.ErrParse},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			statements, err := testCase.engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			_, err = testCase.engine.AnalyseQuery(t.Context(), nil, statements[0])
			require.ErrorIs(t, err, testCase.sentinel)
			if testCase.wantLimit {
				require.ErrorIs(t, err, fault.ErrLimit)
			}
		})
	}
}

func TestAnalyseQueryRegistersProcedureArguments(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		sql           string
		wantFunction  string
		wantOrdinals  []int
		wantCastTypes []string
	}{
		{name: "cast argument", sql: "CALL tidy($1::timestamptz)", wantFunction: "tidy", wantOrdinals: []int{0}, wantCastTypes: []string{"timestamptz"}},
		{name: "positional arguments", sql: "CALL refresh_continuous_aggregate('v', $1, $2)", wantFunction: "refresh_continuous_aggregate", wantOrdinals: []int{1, 2}, wantCastTypes: []string{"", ""}},
		{name: "schema-qualified procedure", sql: "CALL maintenance.tidy($1)", wantFunction: "maintenance.tidy", wantOrdinals: []int{0}, wantCastTypes: []string{""}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newPostgresCatalogue(), testCase.sql)

			require.Len(t, analysis.ParameterReferences, len(testCase.wantOrdinals))
			for index, reference := range analysis.ParameterReferences {
				assert.Equal(t, testCase.wantOrdinals[index], reference.ArgumentOrdinal)
				if testCase.wantCastTypes[index] == "" {
					assert.Equal(t, sqlmodel.ParameterContextFunctionArgument, reference.Context)
					assert.Equal(t, testCase.wantFunction, reference.EnclosingFunctionName)
					continue
				}
				require.NotNil(t, reference.CastType)
				assert.Equal(t, testCase.wantCastTypes[index], reference.CastType.EngineName)
			}
		})
	}
}

func TestWithInsertKeywordsOpensAnInsert(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		options  []Option
		sql      string
		message  string
		inserted bool
	}{
		{name: "PostgreSQL has no UPSERT", options: nil, sql: "UPSERT INTO items (id) VALUES ($1)", message: "not a statement Seagull can analyse"},
		{name: "a registered word opens an INSERT", options: []Option{WithInsertKeywords("upsert")},
			sql: "UPSERT INTO items (id) VALUES ($1) RETURNING id", inserted: true},
		{name: "words from several options add up", options: []Option{WithInsertKeywords("REPLACE"), WithInsertKeywords("UPSERT")},
			sql: "WITH x AS (SELECT 1) UPSERT INTO items (id) SELECT $1 FROM x", inserted: true},
		{name: "a registered word in a WITH list", options: []Option{WithInsertKeywords("UPSERT")},
			sql: "WITH u AS (UPSERT INTO items (id) VALUES ($1) RETURNING id) SELECT id FROM u", inserted: false},
		{name: "a registered word takes no ON CONFLICT", options: []Option{WithInsertKeywords("UPSERT")},
			sql: "UPSERT INTO items (id) VALUES (1) ON CONFLICT DO NOTHING", message: "UPSERT takes no ON CONFLICT clause"},
		{name: "INSERT keeps its ON CONFLICT", options: []Option{WithInsertKeywords("UPSERT")},
			sql: "INSERT INTO items (id) VALUES ($1) ON CONFLICT DO NOTHING", inserted: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(testCase.options...)
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])
			if testCase.message != "" {
				require.ErrorContains(t, err, testCase.message)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.inserted, analysis.DataChange == ast.DataChangeInsert)
			require.NotEmpty(t, analysis.ParameterReferences)
		})
	}
}

func TestTableValuedFunctionColumnsComeFromSetReturningFunctions(t *testing.T) {
	t.Parallel()

	pair := sqlmodel.NewSQLType(sqlmodel.TypeCategoryComposite, "record")
	engine := New(WithExtraFunctions(func(builder *FunctionCatalogueBuilder) {
		pair.StructFields = []sqlmodel.StructField{
			{Name: "first", SQLType: builder.Integer(), Positional: false},
			{Name: "second", SQLType: builder.Text(), Positional: false},
		}
		setOf := func(name string, returnType sqlmodel.SQLType, behaviour sqlmodel.FunctionNullableBehaviour, argument sqlmodel.SQLType) {
			signature := &sqlmodel.Function{}
			signature.ReturnType = returnType
			signature.ReturnsSet = true
			signature.NullableBehaviour = behaviour
			signature.Arguments = builder.Args(Arg{Name: "agg", Type: argument})
			builder.Add(name, signature)
		}
		setOf("ranked", builder.Bigint(), sqlmodel.FunctionNullableNeverNull, builder.Bigint())
		setOf("ranked", builder.Text(), sqlmodel.FunctionNullableNeverNull, builder.Text())
		setOf("ranked", builder.Any(), sqlmodel.FunctionNullableNeverNull, builder.Any())
		setOf("counted", builder.Bigint(), sqlmodel.FunctionNullableNeverNull, builder.Bigint())
		setOf("counted", builder.Bigint(), sqlmodel.FunctionNullableReturnsNullOnNull, builder.Text())
		setOf("pairs", pair, sqlmodel.FunctionNullableNeverNull, builder.Bigint())
		setOf("wildcard", builder.Any(), sqlmodel.FunctionNullableNeverNull, builder.Bigint())
		setOf("opaque", builder.Type("opaque_record"), sqlmodel.FunctionNullableNeverNull, builder.Bigint())
		setOf("mixed", builder.Bigint(), sqlmodel.FunctionNullableNeverNull, builder.Bigint())
		setOf("mixed", pair, sqlmodel.FunctionNullableNeverNull, builder.Text())
		builder.NeverNull("scalar", builder.Args(Arg{Name: "agg", Type: builder.Bigint()}), builder.Bigint())
	}))
	integer := engine.NormaliseTypeName("int4")
	bigint := engine.NormaliseTypeName("int8")
	text := engine.NormaliseTypeName("text")
	testCases := []struct {
		name string
		want []ast.ScopedColumn
	}{
		{name: "ranked", want: []ast.ScopedColumn{{Name: "ranked", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), Nullable: false}}},
		{name: "counted", want: []ast.ScopedColumn{{Name: "counted", SQLType: bigint, Nullable: true}}},
		{name: "pairs", want: []ast.ScopedColumn{{Name: "first", SQLType: integer, Nullable: true}, {Name: "second", SQLType: text, Nullable: true}}},
		{name: "wildcard", want: nil},
		{name: "opaque", want: nil},
		{name: "mixed", want: nil},
		{name: "scalar", want: nil},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, engine.TableValuedFunctionColumns(testCase.name))
		})
	}
}

func TestTableValuedFunctionColumnsFromCatalogueTypeASetOfABaseType(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.NewIntegerType("int4", widthInteger, false)
	setOf := func(name string, returnType sqlmodel.SQLType) *sqlmodel.Function {
		signature := &sqlmodel.Function{}
		signature.Name = name
		signature.Schema = "public"
		signature.ReturnType = returnType
		signature.ReturnsSet = true
		return signature
	}
	catalogue := &sqlmodel.Catalogue{
		Schemas: map[string]*sqlmodel.Schema{
			"public": {
				Name: "public",
				Functions: map[string][]*sqlmodel.Function{
					"numbers":  {setOf("numbers", integer)},
					"records":  {setOf("records", sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "record"))},
					"one_only": {{Name: "one_only", Schema: "public", ReturnType: integer}},
				},
			},
		},
		DefaultSchema: "public",
	}

	engine := New()
	require.Equal(t, []ast.ScopedColumn{{Name: "numbers", SQLType: integer, Nullable: true}},
		catalogueFunctionColumns(t, engine, catalogue, "numbers"))
	require.Nil(t, catalogueFunctionColumns(t, engine, catalogue, "records"))
	require.Equal(t, []ast.ScopedColumn{{Name: "one_only", SQLType: integer, Nullable: true}},
		catalogueFunctionColumns(t, engine, catalogue, "one_only"))
}

func TestCoerceArgumentReadsQuotedLiteralsAsTheParameter(t *testing.T) {
	t.Parallel()

	engine := New()
	testCases := []struct {
		name      string
		argument  dialect.CallArgument
		parameter string
		coerced   bool
	}{
		{name: "a quoted literal to an interval", argument: dialect.CallArgument{Type: builtinTypeMap["text"], Literal: true}, parameter: "interval", coerced: true},
		{name: "a quoted literal to a timestamp with time zone", argument: dialect.CallArgument{Type: builtinTypeMap["text"], Literal: true}, parameter: "timestamptz", coerced: true},
		{name: "a quoted literal to jsonb", argument: dialect.CallArgument{Type: builtinTypeMap["text"], Literal: true}, parameter: "jsonb", coerced: true},
		{name: "a text column to an interval", argument: dialect.CallArgument{Type: builtinTypeMap["text"], Literal: false}, parameter: "interval", coerced: false},
		{name: "a numeric literal to double precision casts implicitly instead", argument: dialect.CallArgument{Type: builtinTypeMap["numeric"], Literal: true}, parameter: "float8", coerced: false},
		{name: "a numeric column to real casts implicitly instead", argument: dialect.CallArgument{Type: builtinTypeMap["numeric"], Literal: false}, parameter: "float4", coerced: false},
		{name: "a numeric literal to an integer", argument: dialect.CallArgument{Type: builtinTypeMap["numeric"], Literal: true}, parameter: "int4", coerced: false},
		{name: "a double precision column to an interval", argument: dialect.CallArgument{Type: builtinTypeMap["float8"], Literal: false}, parameter: "interval", coerced: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			coercion, coerced := engine.CoerceArgument(testCase.argument, builtinTypeMap[testCase.parameter])

			require.Equal(t, testCase.coerced, coerced)
			require.False(t, coercion.MayBeNull)
		})
	}
}

func TestResolveFunctionCallTypesGenerateSeriesByItsBounds(t *testing.T) {
	t.Parallel()

	engine := New()
	testCases := []struct {
		name   string
		bounds []sqlmodel.SQLType
		want   string
	}{
		{name: "integers", bounds: []sqlmodel.SQLType{builtinTypeMap["int4"], builtinTypeMap["int4"]}, want: "int4"},
		{name: "bigints", bounds: []sqlmodel.SQLType{builtinTypeMap["int4"], builtinTypeMap["int8"]}, want: "int8"},
		{name: "timestamps", bounds: []sqlmodel.SQLType{builtinTypeMap["timestamp"], builtinTypeMap["timestamp"], builtinTypeMap["interval"]}, want: "timestamp"},
		{name: "dates", bounds: []sqlmodel.SQLType{builtinTypeMap["date"], builtinTypeMap["date"], builtinTypeMap["interval"]}, want: "timestamptz"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := engine.ResolveFunctionCall(t.Context(), nil, "generate_series", "", testCase.bounds)
			require.NoError(t, err)
			require.NotNil(t, resolution)
			require.Equal(t, testCase.want, resolution.ReturnType.EngineName)
			require.True(t, resolution.ReturnsSet)
		})
	}
}

func TestWithoutBuiltinFunctionsLeavesThemUnknown(t *testing.T) {
	t.Parallel()

	engine := New(WithoutBuiltinFunctions("Mode", "regexp_count"))
	functions := engine.BuiltinFunctions().Functions

	require.NotContains(t, functions, "mode")
	require.NotContains(t, functions, "regexp_count")
	require.Contains(t, functions, "regexp_replace")

	resolution, err := engine.ResolveFunctionCall(t.Context(), nil, "mode", "", []sqlmodel.SQLType{builtinTypeMap["text"]})
	require.NoError(t, err)
	require.Nil(t, resolution)
}

func TestFunctionsInFromGiveTheColumnsOfTheirResult(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.NewIntegerType("int4", widthInteger, false)
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	signature := func(name string, returnType sqlmodel.SQLType, returnsSet bool, resultName string) *sqlmodel.Function {
		function := &sqlmodel.Function{}
		function.Name = name
		function.Schema = "public"
		function.ReturnType = returnType
		function.ReturnsSet = returnsSet
		function.ResultName = resultName
		return function
	}
	users := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "users")
	catalogue := &sqlmodel.Catalogue{
		SearchPath: []string{"public"},
		Schemas: map[string]*sqlmodel.Schema{
			"public": {
				Name: "public",
				Tables: map[string]*sqlmodel.Table{"users": {Name: "users", Schema: "public", Columns: []sqlmodel.Column{
					{Name: "id", SQLType: integer, Nullable: false},
					{Name: "name", SQLType: text, Nullable: false},
				}}},
				Functions: map[string][]*sqlmodel.Function{
					"users_named": {signature("users_named", users, true, "")},
					"one_user":    {signature("one_user", users, false, "")},
					"add_author":  {signature("add_author", integer, false, "id")},
					"tidy":        {signature("tidy", sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "void"), false, "")},
				},
			},
		},
	}
	everyColumnNullable := []ast.ScopedColumn{{Name: "id", SQLType: integer, Nullable: true}, {Name: "name", SQLType: text, Nullable: true}}

	testCases := []struct {
		name     string
		function string
		want     []ast.ScopedColumn
	}{
		{name: "a set of a table's rows", function: "users_named", want: everyColumnNullable},
		{name: "one row of a table's type", function: "one_user", want: everyColumnNullable},
		{name: "a value named by its only output parameter", function: "add_author",
			want: []ast.ScopedColumn{{Name: "id", SQLType: integer, Nullable: true}}},
		{name: "nothing", function: "tidy", want: nil},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, catalogueFunctionColumns(t, New(), catalogue, testCase.function))
		})
	}
}

func catalogueFunctionColumns(t *testing.T, engine *Dialect, catalogue *sqlmodel.Catalogue, name string) []ast.ScopedColumn {
	t.Helper()
	columns, err := engine.TableValuedFunctionColumnsFromCatalogue(t.Context(), catalogue, name)
	require.NoError(t, err)
	return columns
}

func TestEntryPointsReturnTheContextsCauseWhenItIsDoneBeforeTheyStart(t *testing.T) {
	t.Parallel()

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), "CREATE TABLE t (id int); SELECT 1")
	require.NoError(t, err)
	require.Len(t, statements, 2)
	testCases := []struct {
		call func(ctx context.Context) error
		name string
	}{
		{name: "parse statements", call: func(ctx context.Context) error {
			_, err := engine.ParseStatements(ctx, "SELECT 1")
			return err
		}},
		{name: "apply DDL", call: func(ctx context.Context) error {
			_, err := engine.ApplyDDL(ctx, statements[0])
			return err
		}},
		{name: "analyse query", call: func(ctx context.Context) error {
			_, err := engine.AnalyseQuery(ctx, nil, statements[1])
			return err
		}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancelCause(t.Context())
			cancel(errAnalysisAbandoned)

			require.ErrorIs(t, testCase.call(ctx), errAnalysisAbandoned)
		})
	}
}

func TestParseStatementsStopsBetweenStatementsOnceTheContextIsDone(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		sql            string
		wantStatements int
		wantClassified int
		wantErr        bool
	}{
		{name: "a script of several statements stops after the first", sql: "SELECT 1; SELECT 2; SELECT 3", wantClassified: 1, wantErr: true},
		{name: "a script of one statement is done before the next check", sql: "SELECT 1", wantStatements: 1, wantClassified: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			classified := 0
			engine := New(WithStatementExtensions(cancellingExtension{foobarExtension: foobarExtension{}, cancel: cancel, classified: &classified}))

			statements, err := engine.ParseStatements(ctx, testCase.sql)

			assert.Equal(t, testCase.wantClassified, classified)
			assert.Len(t, statements, testCase.wantStatements)
			if testCase.wantErr {
				require.ErrorIs(t, err, errAnalysisAbandoned)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestAHugeStatementCostsNoMoreThanItsTokenBudget(t *testing.T) {
	const scriptBytes = 4 << 20
	const allocationBudget = 200 << 20
	row := "(1, 'value'), "
	sql := "INSERT INTO t (id, name) VALUES " + strings.Repeat(row, scriptBytes/len(row)) + "(1, 'value')"
	engine := New()

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	statements, err := engine.ParseStatements(t.Context(), sql)
	runtime.ReadMemStats(&after)

	require.NoError(t, err)
	require.Len(t, statements, 1)
	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(allocationBudget))
	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err, "a statement no DDL handler walks stays a no-op")
	assert.Nil(t, mutation)
	_, err = engine.AnalyseQuery(t.Context(), nil, statements[0])
	require.ErrorIs(t, err, dialectkit.ErrTokenBudget)
	scaling.Within(t, 100*time.Millisecond, func(ctx context.Context) {
		_, _ = engine.ParseStatements(ctx, sql)
	})
}

func TestWithStatementExtensionsLeavesOutANilExtension(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		extensions []pgext.StatementExtensionPort
		wantKind   statementKind
	}{
		{name: "a nil extension alone", extensions: []pgext.StatementExtensionPort{nil}, wantKind: statementKindUnknown},
		{name: "a nil extension before one that claims", extensions: []pgext.StatementExtensionPort{nil, foobarExtension{}}, wantKind: statementKindFoobar},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(WithStatementExtensions(testCase.extensions...))

			statements, err := engine.ParseStatements(t.Context(), "CREATE FOOBAR widgets")

			require.NoError(t, err)
			require.Len(t, statements, 1)
			parsed, isParsed := statements[0].Raw.(*parsedStatement)
			require.True(t, isParsed)
			assert.Equal(t, testCase.wantKind, parsed.kind)
		})
	}
}

func TestEachStatementExtensionClassifiesItsOwnCopyOfTheTokens(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
		want []string
	}{
		{name: "a query", sql: "SELECT 1", want: []string{"select", "select"}},
		{name: "a DDL statement", sql: "CREATE TABLE t (id int)", want: []string{"create", "create"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var seen []string
			engine := New(WithStatementExtensions(overwritingExtension{seen: &seen}, overwritingExtension{seen: &seen}))

			statements, err := engine.ParseStatements(t.Context(), testCase.sql)

			require.NoError(t, err)
			require.Len(t, statements, 1)
			assert.Equal(t, testCase.want, seen)
			parsed, isParsed := statements[0].Raw.(*parsedStatement)
			require.True(t, isParsed)
			assert.Equal(t, testCase.want[0], parsed.tokens[0].Value)
			assert.Equal(t, tokenIdentifier, parsed.tokens[0].Kind)
		})
	}
}

func TestEveryHookKindAsksTheMostRecentHookFirst(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.NewIntegerType("int4", widthInteger, false)
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	named := func(name string) *sqlmodel.SQLType {
		return new(sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, name))
	}
	engine := New(
		WithPromoteTypeHook(func(sqlmodel.SQLType, sqlmodel.SQLType) *sqlmodel.SQLType { return named("earlier") }),
		WithPromoteTypeHook(func(left, _ sqlmodel.SQLType) *sqlmodel.SQLType {
			if left.Category == sqlmodel.TypeCategoryText {
				return nil
			}
			return named("later")
		}),
		WithImplicitCastHook(func(sqlmodel.SQLType, sqlmodel.SQLType) *bool { return new(false) }),
		WithImplicitCastHook(func(from, _ sqlmodel.SQLType) *bool {
			if from.Category == sqlmodel.TypeCategoryText {
				return nil
			}
			return new(true)
		}),
		WithRowValueTypeHook(func(dialect.RowRelation) *sqlmodel.SQLType { return named("earlier") }),
		WithRowValueTypeHook(func(relation dialect.RowRelation) *sqlmodel.SQLType {
			if relation.Function {
				return nil
			}
			return named("later")
		}),
	)
	testCases := []struct {
		answer func() string
		name   string
		want   string
	}{
		{name: "promote type answered by the later hook", want: "later",
			answer: func() string { return engine.PromoteType(integer, integer).EngineName }},
		{name: "promote type falling through to the earlier hook", want: "earlier",
			answer: func() string { return engine.PromoteType(text, integer).EngineName }},
		{name: "implicit cast answered by the later hook", want: "true",
			answer: func() string { return strconv.FormatBool(engine.CanImplicitCast(integer, text)) }},
		{name: "implicit cast falling through to the earlier hook", want: "false",
			answer: func() string { return strconv.FormatBool(engine.CanImplicitCast(text, text)) }},
		{name: "row value type answered by the later hook", want: "later",
			answer: func() string { return engine.RowValueType(dialect.RowRelation{Name: "t"}).EngineName }},
		{name: "row value type falling through to the earlier hook", want: "earlier",
			answer: func() string { return engine.RowValueType(dialect.RowRelation{Function: true}).EngineName }},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, testCase.answer())
		})
	}
}

func TestInfoHandsOutACopyThatChangesNothingElse(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		change func(info *dialect.Info)
		read   func(info dialect.Info) any
		name   string
	}{
		{
			name:   "the system columns",
			change: func(info *dialect.Info) { info.SystemColumns[0] = "changed" },
			read:   func(info dialect.Info) any { return info.SystemColumns },
		},
		{
			name:   "the operator features",
			change: func(info *dialect.Info) { clear(info.OperatorFeatures) },
			read:   func(info dialect.Info) any { return info.OperatorFeatures },
		},
		{
			name:   "the filter operators",
			change: func(info *dialect.Info) { info.Filters.Operators[0] = "changed" },
			read:   func(info dialect.Info) any { return info.Filters.Operators },
		},
		{
			name:   "the casts",
			change: func(info *dialect.Info) { info.Filters.Casts[0].Before = "changed" },
			read:   func(info dialect.Info) any { return info.Filters.Casts },
		},
		{
			name:   "the cast types",
			change: func(info *dialect.Info) { info.Filters.CastTypes[0] = "changed" },
			read:   func(info dialect.Info) any { return info.Filters.CastTypes },
		},
		{
			name:   "the placeholder sigils",
			change: func(info *dialect.Info) { info.Placeholders.Sigils[0].Prefix = "changed" },
			read:   func(info dialect.Info) any { return info.Placeholders.Sigils },
		},
		{
			name:   "the drivers",
			change: func(info *dialect.Info) { info.Drivers[0] = "changed" },
			read:   func(info dialect.Info) any { return info.Drivers },
		},
		{
			name:   "the search path",
			change: func(info *dialect.Info) { info.SchemaSearchPath[0] = "changed" },
			read:   func(info dialect.Info) any { return info.SchemaSearchPath },
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			want := testCase.read(New().Info())
			changed := engine.Info()

			testCase.change(&changed)

			assert.NotEqual(t, want, testCase.read(changed))
			assert.Equal(t, want, testCase.read(engine.Info()))
			assert.Equal(t, want, testCase.read(New().Info()))
		})
	}
}

func TestPostgresEngine_StatementExtension_RecognisesUnknownStatement(t *testing.T) {
	t.Parallel()

	engine := New(WithStatementExtensions(foobarExtension{}))

	statements, err := engine.ParseStatements(t.Context(), "CREATE FOOBAR widgets")
	require.NoError(t, err)
	require.Len(t, statements, 1)

	raw, ok := statements[0].Raw.(*parsedStatement)
	require.True(t, ok)
	assert.Equal(t, statementKindFoobar, raw.kind, "extension kind should survive ParseStatements")
}

func TestPostgresEngine_StatementExtension_DispatchProducesMutation(t *testing.T) {
	t.Parallel()

	engine := New(WithStatementExtensions(foobarExtension{}))

	statements, err := engine.ParseStatements(t.Context(), "CREATE FOOBAR widgets")
	require.NoError(t, err)
	require.Len(t, statements, 1)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
	assert.Equal(t, "widgets", mutation.TableName)
	assert.Equal(t, "true", mutation.EngineSpecific["FOOBAR"])
}

func TestPostgresEngine_StatementExtension_BuiltinPathUnchanged(t *testing.T) {
	t.Parallel()

	engine := New(WithStatementExtensions(foobarExtension{}))

	statements, err := engine.ParseStatements(t.Context(), "CREATE TABLE products (id BIGINT)")
	require.NoError(t, err)
	require.Len(t, statements, 1)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
	assert.Equal(t, "products", mutation.TableName)
	assert.Empty(t, mutation.EngineSpecific["FOOBAR"], "built-in DDL should not pick up the extension's marker")
}

func TestPostgresEngine_StatementExtension_DeclinedKindFallsThrough(t *testing.T) {
	t.Parallel()

	declining := decliningExtension{}
	engine := New(WithStatementExtensions(declining))

	statements, err := engine.ParseStatements(t.Context(), "CREATE NONESUCH zzz")
	require.NoError(t, err)
	require.Len(t, statements, 1)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	assert.Nil(t, mutation)
}

func TestPostgresEngine_StatementExtension_RejectsBuiltinKindMisuse(t *testing.T) {
	t.Parallel()

	engine := New(WithStatementExtensions(misusingBuiltinKindExtension{}))

	statements, err := engine.ParseStatements(t.Context(), "CREATE NONESUCH zzz")
	require.NoError(t, err)
	require.Len(t, statements, 1)

	raw, rawOk := statements[0].Raw.(*parsedStatement)
	require.True(t, rawOk, "extension misuse must still produce a *parsedStatement")
	assert.Equal(t, statementKindUnknown, raw.kind,
		"extension returning a built-in kind must be rejected and fall through to unknown")

	mutation, applyErr := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, applyErr)
	assert.Nil(t, mutation, "unknown statement must produce no mutation")
}

func TestPostgresEngine_PostParseHook_EnrichesExistingMutation(t *testing.T) {
	t.Parallel()

	tag := func(_ pgext.ParserContext, kind statementKind, mutation *ast.CatalogueMutation) error {
		if mutation == nil {
			return nil
		}
		if kind == statementKindCreateTable {
			if mutation.EngineSpecific == nil {
				mutation.EngineSpecific = map[string]string{}
			}
			mutation.EngineSpecific["TAGGED_BY_HOOK"] = "true"
		}
		return nil
	}
	engine := New(WithPostParseHook(tag))

	statements, err := engine.ParseStatements(t.Context(), "CREATE TABLE widgets (id BIGINT)")
	require.NoError(t, err)
	require.Len(t, statements, 1)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "true", mutation.EngineSpecific["TAGGED_BY_HOOK"], "post-parse hook should run after built-in handler")
}

func TestPostgresEngine_PostParseHook_RunInRegistrationOrder(t *testing.T) {
	t.Parallel()

	order := []string{}
	first := func(_ pgext.ParserContext, _ statementKind, _ *ast.CatalogueMutation) error {
		order = append(order, "first")
		return nil
	}
	second := func(_ pgext.ParserContext, _ statementKind, _ *ast.CatalogueMutation) error {
		order = append(order, "second")
		return nil
	}
	engine := New(
		WithPostParseHook(first),
		WithPostParseHook(second),
	)

	statements, err := engine.ParseStatements(t.Context(), "CREATE TABLE x (id BIGINT)")
	require.NoError(t, err)
	_, err = engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	assert.Equal(t, []string{"first", "second"}, order, "hooks should run in registration order")
}

func TestPostgresEngine_PostParseHook_RanOnExtensionStatement(t *testing.T) {
	t.Parallel()

	hookSawExtensionKind := false
	hook := func(_ pgext.ParserContext, kind statementKind, _ *ast.CatalogueMutation) error {
		if kind == statementKindFoobar {
			hookSawExtensionKind = true
		}
		return nil
	}
	engine := New(
		WithStatementExtensions(foobarExtension{}),
		WithPostParseHook(hook),
	)

	statements, err := engine.ParseStatements(t.Context(), "CREATE FOOBAR x")
	require.NoError(t, err)
	_, err = engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	assert.True(t, hookSawExtensionKind, "post-parse hook must run for extension statements too")
}

func TestPostgresEngine_StatementExtension_OverridesBuiltinClassification(t *testing.T) {
	t.Parallel()

	engine := New(WithStatementExtensions(selectOverridingExtension{}))

	statements, err := engine.ParseStatements(t.Context(), "SELECT magic_func(1, 2)")
	require.NoError(t, err)
	require.Len(t, statements, 1)
	raw, rawOk := statements[0].Raw.(*parsedStatement)
	require.True(t, rawOk, "extension statement must carry a *parsedStatement payload")
	assert.Equal(t, statementKindOverridingSelect, raw.kind,
		"extension classification must override built-in SELECT classification")

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "magic_target", mutation.TableName)
	assert.Equal(t, "true", mutation.EngineSpecific["MAGIC"])
}

func TestPostgresEngine_PostParseHook_ErrorAbortsChain(t *testing.T) {
	t.Parallel()

	secondRan := false
	failing := func(_ pgext.ParserContext, _ statementKind, _ *ast.CatalogueMutation) error {
		return assert.AnError
	}
	tracking := func(_ pgext.ParserContext, _ statementKind, _ *ast.CatalogueMutation) error {
		secondRan = true
		return nil
	}
	engine := New(
		WithPostParseHook(failing),
		WithPostParseHook(tracking),
	)

	statements, err := engine.ParseStatements(t.Context(), "CREATE TABLE x (id BIGINT)")
	require.NoError(t, err)
	_, applyErr := engine.ApplyDDL(t.Context(), statements[0])
	require.Error(t, applyErr, "failing hook must abort the chain")
	assert.False(t, secondRan, "subsequent hooks must not run after an error")
}

func TestPostgresEngine_NoExtensions_BehavesLikeBaseline(t *testing.T) {
	t.Parallel()

	engine := New()

	statements, err := engine.ParseStatements(t.Context(), "CREATE FOOBAR widgets")
	require.NoError(t, err)
	require.Len(t, statements, 1)
	raw, rawOk := statements[0].Raw.(*parsedStatement)
	require.True(t, rawOk, "baseline statement must carry a *parsedStatement payload")
	assert.Equal(t, statementKindUnknown, raw.kind)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	assert.Nil(t, mutation)
}

func TestPostgresEngine_StatementExtension_KeywordMismatchFailsApply(t *testing.T) {
	t.Parallel()

	engine := New(WithStatementExtensions(foobarTableExtension{}))
	statements, err := engine.ParseStatements(t.Context(), "CREATE FOOBAR widgets")
	require.NoError(t, err)

	_, err = engine.ApplyDDL(t.Context(), statements[0])

	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected keyword TABLE")
	assert.NotContains(t, err.Error(), "panic")
}

func TestTableValuedFunctionColumnsPassOverMissingCatalogueEntries(t *testing.T) {
	t.Parallel()

	pair := &sqlmodel.CompositeType{Name: "pair", Fields: []sqlmodel.Column{sqlmodel.NewColumn("a", builtinTypeMap["int4"], true)}}
	returning := func(schema string) []*sqlmodel.Function {
		return []*sqlmodel.Function{{
			Name:       "f",
			ReturnType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryComposite, EngineName: "pair", Schema: schema},
		}}
	}
	testCases := []struct {
		catalogue   *sqlmodel.Catalogue
		name        string
		wantColumns []string
	}{
		{
			name: "a nil composite type gives no columns",
			catalogue: &sqlmodel.Catalogue{DefaultSchema: "public", Schemas: map[string]*sqlmodel.Schema{"public": {
				Name: "public", CompositeTypes: map[string]*sqlmodel.CompositeType{"pair": nil}, Functions: map[string][]*sqlmodel.Function{"f": returning("")},
			}}},
			wantColumns: nil,
		},
		{
			name: "a type in a schema the catalogue does not hold gives no columns",
			catalogue: &sqlmodel.Catalogue{DefaultSchema: "public", Schemas: map[string]*sqlmodel.Schema{"public": {
				Name: "public", Functions: map[string][]*sqlmodel.Function{"f": returning("elsewhere")},
			}}},
			wantColumns: nil,
		},
		{
			name: "a nil schema entry for the type's schema gives no columns",
			catalogue: &sqlmodel.Catalogue{DefaultSchema: "public", Schemas: map[string]*sqlmodel.Schema{
				"public":    {Name: "public", Functions: map[string][]*sqlmodel.Function{"f": returning("elsewhere")}},
				"elsewhere": nil,
			}},
			wantColumns: nil,
		},
		{
			name: "a type's schema is found under the catalogue's key for it",
			catalogue: &sqlmodel.Catalogue{DefaultSchema: "public", Identifiers: sqlmodel.IdentifierCase{Insensitive: true}, Schemas: map[string]*sqlmodel.Schema{
				"public": {Name: "public", Functions: map[string][]*sqlmodel.Function{"f": returning("Types")}},
				"types":  {Name: "types", CompositeTypes: map[string]*sqlmodel.CompositeType{"pair": pair}},
			}},
			wantColumns: []string{"a"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			columns, err := New().TableValuedFunctionColumnsFromCatalogue(t.Context(), testCase.catalogue, "f")

			require.NoError(t, err)
			var names []string
			for _, column := range columns {
				names = append(names, column.Name)
			}
			assert.Equal(t, testCase.wantColumns, names)
		})
	}
}

func TestADialectWhoseBuiltinsDidNotDecodeReportsItWhenUsed(t *testing.T) {
	t.Parallel()

	decodeErr := errors.New("line 3: unknown type spelling")
	broken := func() (*dialectkit.FunctionSpec, error) { return nil, decodeErr }
	reporting := func(builder *FunctionCatalogueBuilder) { builder.ReportError(decodeErr) }
	testCases := []struct {
		engine *Dialect
		name   string
	}{
		{name: "built-in signatures that did not decode", engine: newDialect(&settings{Name: "postgres"}, broken)},
		{name: "a flavour's registration that reported a problem", engine: New(WithName("flavour"), WithExtraFunctions(reporting))},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			statements, err := testCase.engine.ParseStatements(t.Context(), "CREATE TABLE t (id int); SELECT 1")
			require.NoError(t, err)
			require.Len(t, statements, 2)

			_, applyErr := testCase.engine.ApplyDDL(t.Context(), statements[0])
			_, analyseErr := testCase.engine.AnalyseQuery(t.Context(), nil, statements[1])

			require.ErrorIs(t, applyErr, decodeErr)
			require.ErrorIs(t, analyseErr, decodeErr)
			assert.Contains(t, applyErr.Error(), "building the dialect")
		})
	}
}

func TestADialectBuiltWholeReportsNoBuildError(t *testing.T) {
	t.Parallel()

	_, err := builtinFunctionSpec()
	require.NoError(t, err)
	require.NoError(t, New().initErr)
}

func TestAFailingPostParseHookIsNamedInTheError(t *testing.T) {
	t.Parallel()

	passing := func(pgext.ParserContext, statementKind, *ast.CatalogueMutation) error { return nil }
	failing := func(pgext.ParserContext, statementKind, *ast.CatalogueMutation) error { return assert.AnError }
	testCases := []struct {
		name    string
		hooks   []pgext.PostParseHook
		wantErr string
	}{
		{name: "the first hook", hooks: []pgext.PostParseHook{failing, passing}, wantErr: "post-parse hook 0: "},
		{name: "a later hook", hooks: []pgext.PostParseHook{passing, passing, failing}, wantErr: "post-parse hook 2: "},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var options []Option
			for _, hook := range testCase.hooks {
				options = append(options, WithPostParseHook(hook))
			}
			_, err := applyWithContext(t.Context(), New(options...), "CREATE TABLE t (id int)")

			require.ErrorIs(t, err, assert.AnError)
			assert.Equal(t, testCase.wantErr+assert.AnError.Error(), err.Error())
		})
	}
}

func TestWithheldFunctionsAreNeverResolved(t *testing.T) {
	t.Parallel()

	engine := New(WithoutBuiltinFunctions("Array_Agg", "COALESCE"))
	testCases := []struct {
		name         string
		functionName string
		wantResolved bool
	}{
		{name: "a withheld name in another case", functionName: "array_agg", wantResolved: false},
		{name: "a withheld name as written", functionName: "Coalesce", wantResolved: false},
		{name: "a name that is not withheld", functionName: "greatest", wantResolved: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := engine.ResolveFunctionCall(t.Context(), nil, testCase.functionName, "", []sqlmodel.SQLType{builtinTypeMap["int4"]})

			require.NoError(t, err)
			assert.Equal(t, testCase.wantResolved, resolution != nil)
		})
	}
}

func TestWithExtraTypesTakesNamesDifferingInCaseInSortedOrder(t *testing.T) {
	t.Parallel()

	types := map[string]sqlmodel.SQLType{
		"MONEY": sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "upper"),
		"Money": sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "title"),
		"money": sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "lower"),
	}
	testCases := []struct {
		name string
		runs int
	}{
		{name: "every build picks the name that sorts last", runs: 50},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			for range testCase.runs {
				assert.Equal(t, "lower", New(WithExtraTypes(types)).NormaliseTypeName("money").EngineName)
			}
		})
	}
}

func TestAStatementOverTheTokenBudgetIsCheckedWholeForAMissingSemicolon(t *testing.T) {
	t.Parallel()

	values := strings.TrimSuffix(strings.Repeat("(1), ", 40), ", ")
	testCases := []struct {
		name    string
		sql     string
		wantErr string
	}{
		{
			name:    "a run-on past the tokens the statement kept",
			sql:     "INSERT INTO t VALUES " + values + "\nCREATE TABLE u (id int)",
			wantErr: `syntax error near "CREATE": a semicolon is missing before CREATE`,
		},
		{
			name:    "a run-on among the tokens the statement kept",
			sql:     "INSERT INTO t VALUES (1)\nCREATE TABLE u (id int, " + strings.Repeat("c int, ", 20) + "d int)",
			wantErr: `syntax error near "CREATE": a semicolon is missing before CREATE`,
		},
		{
			name:    "a run-on within the budget",
			sql:     "INSERT INTO t VALUES (1)\nCREATE TABLE u (id int)",
			wantErr: `syntax error near "CREATE": a semicolon is missing before CREATE`,
		},
		{
			name: "a statement over the budget that ends where it should",
			sql:  "INSERT INTO t VALUES " + values,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(WithMaxTokensPerStatement(30))
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)

			_, err = engine.ApplyDDL(t.Context(), statements[0])
			if testCase.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, fault.ErrParse)
			require.ErrorContains(t, err, testCase.wantErr)
		})
	}
}

func TestUntypedArgumentTypePrefersDoublePrecisionAmongNumbers(t *testing.T) {
	t.Parallel()

	engine := New()
	double := engine.NormaliseTypeName("double precision")
	integer := engine.NormaliseTypeName("integer")
	numeric := engine.NormaliseTypeName("numeric")
	text := engine.NormaliseTypeName("text")

	chosen, chooses := engine.UntypedArgumentType([]sqlmodel.SQLType{integer, double, numeric})
	require.True(t, chooses)
	assert.Equal(t, "float8", chosen.EngineName)

	_, chooses = engine.UntypedArgumentType([]sqlmodel.SQLType{integer, numeric})
	assert.False(t, chooses, "without float8 among the overloads PostgreSQL cannot choose")

	_, chooses = engine.UntypedArgumentType([]sqlmodel.SQLType{double, text})
	assert.False(t, chooses, "a non-numeric overload leaves the choice open")
}

func TestBinaryResultTypesArithmeticBetweenAFloatAndAnotherNumberAsDoublePrecision(t *testing.T) {
	t.Parallel()

	engine := New()
	realType := engine.NormaliseTypeName("real")
	integer := engine.NormaliseTypeName("integer")
	numeric := engine.NormaliseTypeName("numeric")

	testCases := []struct {
		name     string
		operator string
		left     sqlmodel.SQLType
		right    sqlmodel.SQLType
		typed    bool
	}{
		{name: "an integer plus a real", operator: "+", left: integer, right: realType, typed: true},
		{name: "a numeric times a real", operator: "*", left: numeric, right: realType, typed: true},
		{name: "a real divided by an integer", operator: "/", left: realType, right: integer, typed: true},
		{name: "two reals keep the structural default", operator: "+", left: realType, right: realType, typed: false},
		{name: "an integer plus a numeric keeps the structural default", operator: "+", left: integer, right: numeric, typed: false},
		{name: "a remainder is no float arithmetic", operator: "%", left: integer, right: realType, typed: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, _, typed := engine.BinaryResult(testCase.operator, testCase.left, testCase.right)
			require.Equal(t, testCase.typed, typed)
			if typed {
				assert.Equal(t, "float8", result.EngineName)
			}
		})
	}
}
