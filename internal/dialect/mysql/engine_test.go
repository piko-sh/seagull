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

package mysql

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
)

type fakeStatement struct{}

var (
	sampleDDLStatements = []string{
		"CREATE TABLE IF NOT EXISTS db.t (a INT NOT NULL AUTO_INCREMENT PRIMARY KEY, b VARCHAR(10) NULL DEFAULT (lower('x')) " +
			"CHECK (length(b) > 0) REFERENCES u(id) ON DELETE CASCADE, c TIMESTAMP ON UPDATE CURRENT_TIMESTAMP(3) COMMENT 'c' " +
			"COLLATE utf8mb4_bin, d INT GENERATED ALWAYS AS (a + 1) STORED, CONSTRAINT k UNIQUE KEY (b), " +
			"FOREIGN KEY (a) REFERENCES u(id), INDEX ix (b)) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4",
		"DROP TABLE IF EXISTS db.t, u",
		"ALTER TABLE t RENAME COLUMN a TO b",
		"ALTER TABLE t ADD COLUMN d INT DEFAULT 0 AFTER a",
		"ALTER TABLE t MODIFY COLUMN d BIGINT NOT NULL",
		"ALTER TABLE t CHANGE COLUMN d e INT",
		"ALTER TABLE t DROP COLUMN d",
		"ALTER TABLE t RENAME TO u",
		"ALTER TABLE t ENGINE = InnoDB, ALGORITHM = (INPLACE)",
		"CREATE OR REPLACE VIEW v (x) AS SELECT a FROM t",
		"DROP VIEW IF EXISTS v",
		"CREATE UNIQUE INDEX i ON t (a, b)",
		"DROP INDEX i ON t",
		"CREATE TRIGGER tr AFTER INSERT ON t FOR EACH ROW SET @x = 1",
		"DROP TRIGGER IF EXISTS tr",
		"CREATE DATABASE IF NOT EXISTS d",
		"DROP DATABASE IF EXISTS d",
		"CREATE FUNCTION f(a INT, b VARCHAR(10)) RETURNS INT DETERMINISTIC RETURN a + 1",
		"DROP FUNCTION IF EXISTS f",
	}
	sampleQueryStatements = []string{
		"WITH c AS (SELECT a FROM t) SELECT DISTINCT a, CAST(? AS DECIMAL(10, 2)), a = ANY (SELECT 1) FROM c " +
			"JOIN u USING (id) LEFT JOIN db.w AS x ON x.id = c.a WHERE a IN (SELECT a FROM t) " +
			"AND EXISTS (SELECT 1 FROM (SELECT 1) d) GROUP BY a HAVING count(*) > 1 UNION SELECT 1 " +
			"ORDER BY 1 LIMIT ? OFFSET ? FOR UPDATE",
		"INSERT INTO t PARTITION (p1) (a, b) VALUES (?, ?) ON DUPLICATE KEY UPDATE b = VALUES(b)",
		"REPLACE INTO t (a) SELECT a FROM u",
		"UPDATE t, db.u AS x SET t.a = ? WHERE t.id = x.id ORDER BY t.a LIMIT 1",
		"DELETE t FROM t JOIN u ON t.id = u.id WHERE u.a = ?",
		"DELETE FROM t WHERE a = ? ORDER BY a LIMIT 1",
		"VALUES ROW(1, ?)",
	}
)

const columnsPastAnInterruptCheck = 5_000

var errWalkCancelled = errors.New("walk cancelled by the test")

func (*fakeStatement) IsParsedStatement() {}

func TestNewMySQLEngine(t *testing.T) {
	t.Parallel()

	engine := New()

	assert.Equal(t, "mysql", engine.Info().Name, "default dialect should be mysql")
	assert.Equal(t, []string{""}, engine.Info().SchemaSearchPath, "MySQL has no default schema in the PostgreSQL sense")
	assert.Equal(t, dialect.PlaceholderSigil{Prefix: "?", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderAnonymous}, engine.Info().Placeholders.Execution, "MySQL uses question-mark parameters")
	assert.Zero(t, engine.Info().Returning, "standard MySQL does not support RETURNING")
	assert.NotNil(t, engine.BuiltinFunctions(), "function catalogue should be initialised")
	assert.NotNil(t, engine.BuiltinTypes(), "type catalogue should be initialised")
}

func TestNormaliseTypeName(t *testing.T) {
	t.Parallel()

	engine := New()

	tests := []struct {
		name           string
		input          string
		wantEngineName string
		wantCategory   sqlmodel.SQLTypeCategory
	}{
		{
			name:           "int normalises to int",
			input:          "int",
			wantEngineName: "int",
			wantCategory:   sqlmodel.TypeCategoryInteger,
		},
		{
			name:           "bigint normalises to bigint",
			input:          "bigint",
			wantEngineName: "bigint",
			wantCategory:   sqlmodel.TypeCategoryInteger,
		},
		{
			name:           "year is a 16-bit year number, the way the driver returns it",
			input:          "year",
			wantEngineName: "year",
			wantCategory:   sqlmodel.TypeCategoryInteger,
		},
		{
			name:           "bit is a byte string, the way the driver returns it",
			input:          "bit",
			wantEngineName: "bit",
			wantCategory:   sqlmodel.TypeCategoryBinary,
		},
		{
			name:           "tinyint normalises to tinyint",
			input:          "tinyint",
			wantEngineName: "tinyint",
			wantCategory:   sqlmodel.TypeCategoryInteger,
		},
		{
			name:           "varchar normalises to varchar",
			input:          "varchar",
			wantEngineName: "varchar",
			wantCategory:   sqlmodel.TypeCategoryText,
		},
		{
			name:           "text normalises to text",
			input:          "text",
			wantEngineName: "text",
			wantCategory:   sqlmodel.TypeCategoryText,
		},
		{
			name:           "datetime normalises to datetime",
			input:          "datetime",
			wantEngineName: "datetime",
			wantCategory:   sqlmodel.TypeCategoryTemporal,
		},
		{
			name:           "timestamp normalises to timestamp",
			input:          "timestamp",
			wantEngineName: "timestamp",
			wantCategory:   sqlmodel.TypeCategoryTemporal,
		},
		{
			name:           "boolean normalises to tinyint",
			input:          "boolean",
			wantEngineName: "tinyint",
			wantCategory:   sqlmodel.TypeCategoryBoolean,
		},
		{
			name:           "bool normalises to tinyint",
			input:          "bool",
			wantEngineName: "tinyint",
			wantCategory:   sqlmodel.TypeCategoryBoolean,
		},
		{
			name:           "json normalises to json",
			input:          "json",
			wantEngineName: "json",
			wantCategory:   sqlmodel.TypeCategoryJSON,
		},
		{
			name:           "decimal normalises to decimal",
			input:          "decimal",
			wantEngineName: "decimal",
			wantCategory:   sqlmodel.TypeCategoryDecimal,
		},
		{
			name:           "numeric normalises to decimal",
			input:          "numeric",
			wantEngineName: "decimal",
			wantCategory:   sqlmodel.TypeCategoryDecimal,
		},
		{
			name:           "float normalises to float",
			input:          "float",
			wantEngineName: "float",
			wantCategory:   sqlmodel.TypeCategoryFloat,
		},
		{
			name:           "double normalises to double",
			input:          "double",
			wantEngineName: "double",
			wantCategory:   sqlmodel.TypeCategoryFloat,
		},
		{
			name:           "case insensitive normalisation",
			input:          "VARCHAR",
			wantEngineName: "varchar",
			wantCategory:   sqlmodel.TypeCategoryText,
		},
		{
			name:           "mixed case normalisation",
			input:          "BigInt",
			wantEngineName: "bigint",
			wantCategory:   sqlmodel.TypeCategoryInteger,
		},
		{

			name:           "int unsigned normalises to int unsigned",
			input:          "int unsigned",
			wantEngineName: "int unsigned",
			wantCategory:   sqlmodel.TypeCategoryInteger,
		},
		{

			name:           "decimal unsigned normalises to decimal",
			input:          "decimal unsigned",
			wantEngineName: "decimal",
			wantCategory:   sqlmodel.TypeCategoryDecimal,
		},
		{
			name:           "numeric unsigned normalises to decimal",
			input:          "numeric unsigned",
			wantEngineName: "decimal",
			wantCategory:   sqlmodel.TypeCategoryDecimal,
		},
		{
			name:           "float unsigned normalises to float",
			input:          "float unsigned",
			wantEngineName: "float",
			wantCategory:   sqlmodel.TypeCategoryFloat,
		},
		{
			name:           "double unsigned normalises to double",
			input:          "double unsigned",
			wantEngineName: "double",
			wantCategory:   sqlmodel.TypeCategoryFloat,
		},
		{
			name:           "double precision unsigned normalises to double",
			input:          "double precision unsigned",
			wantEngineName: "double",
			wantCategory:   sqlmodel.TypeCategoryFloat,
		},
		{
			name:           "real unsigned normalises to double",
			input:          "real unsigned",
			wantEngineName: "double",
			wantCategory:   sqlmodel.TypeCategoryFloat,
		},
		{
			name:           "unknown type falls back to unknown category",
			input:          "nonexistent_type",
			wantEngineName: "nonexistent_type",
			wantCategory:   sqlmodel.TypeCategoryUnknown,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := engine.NormaliseTypeName(testCase.input)

			assert.Equal(t, testCase.wantEngineName, result.EngineName, "engine name mismatch")
			assert.Equal(t, testCase.wantCategory, result.Category, "category mismatch")
		})
	}
}

func TestPromoteType(t *testing.T) {
	t.Parallel()

	engine := New()

	t.Run("integer promotion selects the wider type", func(t *testing.T) {
		t.Parallel()

		tinyint := sqlmodel.NewIntegerType("tinyint", 8, false)
		bigint := sqlmodel.NewIntegerType("bigint", 64, false)

		result := engine.PromoteType(tinyint, bigint)
		assert.Equal(t, "bigint", result.EngineName, "bigint should win over tinyint")

		result = engine.PromoteType(bigint, tinyint)
		assert.Equal(t, "bigint", result.EngineName, "bigint should still win when on the left")
	})

	t.Run("float promotion selects the wider type", func(t *testing.T) {
		t.Parallel()

		floatType := sqlmodel.NewFloatType("float", 32)
		doubleType := sqlmodel.NewFloatType("double", 64)

		result := engine.PromoteType(floatType, doubleType)
		assert.Equal(t, "double", result.EngineName, "double should win over float")
	})

	t.Run("same type returns identity", func(t *testing.T) {
		t.Parallel()

		intType := sqlmodel.NewIntegerType("int", 32, false)

		result := engine.PromoteType(intType, intType)
		assert.Equal(t, "int", result.EngineName, "same type should return itself")
	})
}

func TestCanImplicitCast(t *testing.T) {
	t.Parallel()

	engine := New()

	t.Run("integer to float is allowed", func(t *testing.T) {
		t.Parallel()

		assert.True(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryFloat, "")))
	})

	t.Run("float to integer is not allowed", func(t *testing.T) {
		t.Parallel()

		assert.False(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryFloat, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "")))
	})

	t.Run("integer to decimal is allowed", func(t *testing.T) {
		t.Parallel()

		assert.True(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "")))
	})

	t.Run("text to text is allowed", func(t *testing.T) {
		t.Parallel()

		assert.True(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "")))
	})

	t.Run("text to integer is not allowed", func(t *testing.T) {
		t.Parallel()

		assert.False(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "")))
	})
}

func TestCanImplicitCastFollowsMySQLArgumentConversions(t *testing.T) {
	t.Parallel()

	engine := New()
	testCases := []struct {
		name     string
		from     sqlmodel.SQLTypeCategory
		to       sqlmodel.SQLTypeCategory
		expected bool
	}{
		{name: "a decimal converts to a double", from: sqlmodel.TypeCategoryDecimal, to: sqlmodel.TypeCategoryFloat, expected: true},
		{name: "an integer converts to a string", from: sqlmodel.TypeCategoryInteger, to: sqlmodel.TypeCategoryText, expected: true},
		{name: "a decimal converts to a string", from: sqlmodel.TypeCategoryDecimal, to: sqlmodel.TypeCategoryText, expected: true},
		{name: "a double converts to a string", from: sqlmodel.TypeCategoryFloat, to: sqlmodel.TypeCategoryText, expected: true},
		{name: "a date and time converts to a string", from: sqlmodel.TypeCategoryTemporal, to: sqlmodel.TypeCategoryText, expected: true},
		{name: "a string never converts to a date and time", from: sqlmodel.TypeCategoryText, to: sqlmodel.TypeCategoryTemporal, expected: false},
		{name: "a double never converts to an integer", from: sqlmodel.TypeCategoryFloat, to: sqlmodel.TypeCategoryInteger, expected: false},
		{name: "a JSON document never converts to a string", from: sqlmodel.TypeCategoryJSON, to: sqlmodel.TypeCategoryText, expected: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expected, engine.CanImplicitCast(sqlmodel.NewSQLType(testCase.from, ""), sqlmodel.NewSQLType(testCase.to, "")))
		})
	}
}

func TestSupportedExpressions(t *testing.T) {
	t.Parallel()

	engine := New()
	features := engine.Info().Expressions

	assert.Zero(t, features&ast.SQLFeatureStringConcat,
		"MySQL should not support string concatenation via ||")

	assert.NotZero(t, features&ast.SQLFeatureWindowFunction,
		"MySQL should support window functions")

	assert.NotZero(t, features&ast.SQLFeatureJSONOp,
		"MySQL should support JSON operators")
}

func TestParameterStyle(t *testing.T) {
	t.Parallel()

	engine := New()

	assert.Equal(t, dialect.PlaceholderSigil{Prefix: "?", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderAnonymous}, engine.Info().Placeholders.Execution,
		"MySQL should use question-mark parameter style")
}

func TestSupportsReturning(t *testing.T) {
	t.Parallel()

	t.Run("false by default", func(t *testing.T) {
		t.Parallel()

		engine := New()
		assert.Zero(t, engine.Info().Returning, "standard MySQL does not support RETURNING")
	})

	t.Run("true with WithReturning override", func(t *testing.T) {
		t.Parallel()

		engine := New(WithReturning(ast.DataChangeInsert | ast.DataChangeUpdate | ast.DataChangeDelete))
		assert.NotZero(t, engine.Info().Returning, "engine with returning support should report true")
	})
}

func TestDialect(t *testing.T) {
	t.Parallel()

	t.Run("default dialect is mysql", func(t *testing.T) {
		t.Parallel()

		engine := New()
		assert.Equal(t, "mysql", engine.Info().Name)
	})

	t.Run("custom dialect name via option", func(t *testing.T) {
		t.Parallel()

		engine := New(WithName("mariadb"))
		assert.Equal(t, "mariadb", engine.Info().Name)
	})
}

func TestDefaultSchema(t *testing.T) {
	t.Parallel()

	engine := New()

	assert.Equal(t, []string{""}, engine.Info().SchemaSearchPath,
		"MySQL should return an empty default schema")
}

func TestBuiltinFunctions(t *testing.T) {
	t.Parallel()

	engine := New()
	catalogue := engine.BuiltinFunctions()

	require.NotNil(t, catalogue, "function catalogue must not be nil")
	require.NotNil(t, catalogue.Functions, "function map must not be nil")

	expectedFunctions := []string{"concat", "count", "abs", "now"}
	for _, name := range expectedFunctions {
		signatures, exists := catalogue.Functions[name]
		assert.True(t, exists, "expected built-in function %q to be registered", name)
		assert.NotEmpty(t, signatures, "expected at least one signature for %q", name)
	}
}

func TestBuiltinFunctions_Inet6InBaseCatalogue(t *testing.T) {
	t.Parallel()

	engine := New()
	catalogue := engine.BuiltinFunctions()

	atonSignatures, atonExists := catalogue.Functions["inet6_aton"]
	require.True(t, atonExists, "inet6_aton must be in the base MySQL catalogue")
	require.NotEmpty(t, atonSignatures)
	assert.Equal(t, sqlmodel.TypeCategoryBinary, atonSignatures[0].ReturnType.Category,
		"inet6_aton should return varbinary")
	assert.Equal(t, "varbinary", atonSignatures[0].ReturnType.EngineName)

	ntoaSignatures, ntoaExists := catalogue.Functions["inet6_ntoa"]
	require.True(t, ntoaExists, "inet6_ntoa must be in the base MySQL catalogue")
	require.NotEmpty(t, ntoaSignatures)
	assert.Equal(t, sqlmodel.TypeCategoryText, ntoaSignatures[0].ReturnType.Category,
		"inet6_ntoa should return varchar")
	assert.Equal(t, "varchar", ntoaSignatures[0].ReturnType.EngineName)
}

func TestBuiltinTypes(t *testing.T) {
	t.Parallel()

	engine := New()
	catalogue := engine.BuiltinTypes()

	require.NotNil(t, catalogue, "type catalogue must not be nil")
	require.NotNil(t, catalogue.Types, "type map must not be nil")

	expectedTypes := []string{"int", "varchar", "text", "boolean", "json", "datetime", "bigint", "float", "double", "decimal"}
	for _, name := range expectedTypes {
		_, exists := catalogue.Types[name]
		assert.True(t, exists, "expected built-in type %q to be registered", name)
	}
}

func TestCommentStyle(t *testing.T) {
	t.Parallel()

	engine := New()
	style := engine.Info().Comments

	assert.NotEmpty(t, style.LinePrefix, "line comment prefix should not be empty")
}

func TestSupportedDirectivePrefixes(t *testing.T) {
	t.Parallel()

	engine := New()
	prefixes := engine.Info().Placeholders.Sigils

	require.NotEmpty(t, prefixes, "MySQL should have at least one directive prefix")
}

func TestTableValuedFunctionColumns(t *testing.T) {
	t.Parallel()

	engine := New()

	columns := engine.TableValuedFunctionColumns("nonexistent_tvf")
	assert.Nil(t, columns, "unknown TVF should return nil columns")
}

func TestTableValuedFunctionColumnsReturnsColumnsTheCallerMayChange(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		function string
	}{
		{name: "the diagnostics SHOW WARNINGS and SHOW ERRORS share", function: "show warnings"},
		{name: "the settings SHOW VARIABLES and SHOW STATUS share", function: "show variables"},
		{name: "a result of its own", function: "show full tables"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			want := engine.TableValuedFunctionColumns(testCase.function)
			require.NotEmpty(t, want)
			changed := engine.TableValuedFunctionColumns(testCase.function)

			changed[0].Name = "changed"
			changed[0].SQLType.EngineName = "changed"

			assert.Equal(t, want, engine.TableValuedFunctionColumns(testCase.function))
			assert.Equal(t, want, New().TableValuedFunctionColumns(testCase.function))
		})
	}
}

func TestTableValuedFunctionColumnsFromCatalogue(t *testing.T) {
	t.Parallel()

	engine := New()
	catalogue := &sqlmodel.Catalogue{
		Schemas: map[string]*sqlmodel.Schema{
			"public": {
				Name:      "public",
				Functions: map[string][]*sqlmodel.Function{},
			},
		},
	}

	columns := catalogueFunctionColumns(t, engine, catalogue, "nonexistent_fn")
	assert.Nil(t, columns, "unknown function should return nil columns")
}

func TestResolveFunctionCall(t *testing.T) {
	t.Parallel()

	engine := New()
	catalogue := &sqlmodel.Catalogue{
		Schemas: map[string]*sqlmodel.Schema{},
	}

	_, err := engine.ResolveFunctionCall(t.Context(), catalogue, "nonexistent_fn", "", nil)
	_ = err
}

func TestWithExtraTypes(t *testing.T) {
	t.Parallel()

	engine := New(WithExtraTypes(map[string]sqlmodel.SQLType{
		"custom_type": {Category: sqlmodel.TypeCategoryText, EngineName: "custom_type"},
	}))

	catalogue := engine.BuiltinTypes()
	_, exists := catalogue.Types["custom_type"]
	assert.True(t, exists, "extra type should be registered")
}

func TestWithExtraFunctions(t *testing.T) {
	t.Parallel()

	engine := New(WithExtraFunctions(func(builder *FunctionCatalogueBuilder) {
		builder.NullOnNull("custom_func",
			builder.Args(Arg{Name: "x", Type: builder.Integer()}),
			builder.Integer(),
		)
	}))

	catalogue := engine.BuiltinFunctions()
	_, exists := catalogue.Functions["custom_func"]
	assert.True(t, exists, "extra function should be registered")
}

func TestWithExtraTypesReplacesABuiltInType(t *testing.T) {
	t.Parallel()

	override := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryJSON, EngineName: "longtext"}
	engine := New(WithExtraTypes(map[string]sqlmodel.SQLType{"JSON": override}))

	assert.Equal(t, "longtext", engine.NormaliseTypeName("json").EngineName)
	assert.Equal(t, "longtext", engine.BuiltinTypes().Types["json"].EngineName)
}

func TestWithTypeNormaliserHook(t *testing.T) {
	t.Parallel()

	hook := func(name string, modifiers []int) *sqlmodel.SQLType {
		if name == "custom" {
			return &sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "custom_resolved"}
		}
		return nil
	}
	engine := New(WithTypeNormaliserHook(hook))
	result := engine.NormaliseTypeName("custom")
	assert.Equal(t, "custom_resolved", result.EngineName)
}

func TestWithImplicitCastHook(t *testing.T) {
	t.Parallel()

	hook := func(from, to sqlmodel.SQLType) *bool {
		if from.Category == sqlmodel.TypeCategoryText && to.Category == sqlmodel.TypeCategoryInteger {
			return new(true)
		}
		return nil
	}
	engine := New(WithImplicitCastHook(hook))
	assert.True(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "")),
		"hook should allow text to integer cast")
}

func TestWithPromoteTypeHook(t *testing.T) {
	t.Parallel()

	hook := func(left, right sqlmodel.SQLType) *sqlmodel.SQLType {
		if left.Category == sqlmodel.TypeCategoryText && right.Category == sqlmodel.TypeCategoryText {
			return &sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "longtext"}
		}
		return nil
	}
	engine := New(WithPromoteTypeHook(hook))
	result := engine.PromoteType(
		sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "varchar"},
		sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"},
	)
	assert.Equal(t, "longtext", result.EngineName)
}

func TestMySQLFunctionResolver_ResolveFunctionCall(t *testing.T) {
	t.Parallel()

	resolver := newFunctionResolver()

	t.Run("IF with integer arguments returns integer", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "IF", "", []sqlmodel.SQLType{
			{Category: sqlmodel.TypeCategoryBoolean, EngineName: "tinyint"},
			{Category: sqlmodel.TypeCategoryInteger, EngineName: "int"},
			{Category: sqlmodel.TypeCategoryInteger, EngineName: "int"},
		})
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, sqlmodel.TypeCategoryInteger, resolution.ReturnType.Category)
		assert.Equal(t, sqlmodel.FunctionNullableCalledOnNull, resolution.NullableBehaviour)
	})

	t.Run("IF with mixed integer and text returns text", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "if", "", []sqlmodel.SQLType{
			{Category: sqlmodel.TypeCategoryBoolean, EngineName: "tinyint"},
			{Category: sqlmodel.TypeCategoryInteger, EngineName: "int"},
			{Category: sqlmodel.TypeCategoryText, EngineName: "text"},
		})
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, sqlmodel.TypeCategoryText, resolution.ReturnType.Category)
	})

	t.Run("IF with too few arguments returns nil", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "IF", "", []sqlmodel.SQLType{
			{Category: sqlmodel.TypeCategoryBoolean, EngineName: "tinyint"},
		})
		require.NoError(t, err)
		assert.Nil(t, resolution)
	})

	t.Run("FLOOR of a double widens to the double type", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "floor", "", []sqlmodel.SQLType{
			{Category: sqlmodel.TypeCategoryFloat, EngineName: "double"},
		})
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, sqlmodel.TypeCategoryFloat, resolution.ReturnType.Category)
		assert.Equal(t, "double", resolution.ReturnType.EngineName)
	})

	t.Run("CEIL of a double widens to the double type", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "ceil", "", []sqlmodel.SQLType{
			{Category: sqlmodel.TypeCategoryFloat, EngineName: "double"},
		})
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, sqlmodel.TypeCategoryFloat, resolution.ReturnType.Category)
		assert.Equal(t, "double", resolution.ReturnType.EngineName)
	})

	t.Run("FLOOR of an integer keeps the integer's type", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "floor", "", []sqlmodel.SQLType{
			{Category: sqlmodel.TypeCategoryInteger, EngineName: "bigint", Width: 64},
		})
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, "bigint", resolution.ReturnType.EngineName)
	})

	t.Run("CEIL of a decimal is a BIGINT", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "ceil", "", []sqlmodel.SQLType{
			{Category: sqlmodel.TypeCategoryDecimal, EngineName: "decimal"},
		})
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, "bigint", resolution.ReturnType.EngineName)
	})

	t.Run("MOD of two doubles preserves the fraction (double)", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "mod", "", []sqlmodel.SQLType{
			{Category: sqlmodel.TypeCategoryFloat, EngineName: "double"},
			{Category: sqlmodel.TypeCategoryFloat, EngineName: "double"},
		})
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, sqlmodel.TypeCategoryFloat, resolution.ReturnType.Category)
		assert.Equal(t, "double", resolution.ReturnType.EngineName)
	})

	t.Run("MOD with a float operand widens to double", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "mod", "", []sqlmodel.SQLType{
			{Category: sqlmodel.TypeCategoryInteger, EngineName: "int"},
			{Category: sqlmodel.TypeCategoryFloat, EngineName: "double"},
		})
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, sqlmodel.TypeCategoryFloat, resolution.ReturnType.Category)
		assert.Equal(t, "double", resolution.ReturnType.EngineName)
	})

	t.Run("MOD of two integers is the wider integer", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "mod", "", []sqlmodel.SQLType{
			{Category: sqlmodel.TypeCategoryInteger, EngineName: "int", Width: 32},
			{Category: sqlmodel.TypeCategoryInteger, EngineName: "bigint", Width: 64},
		})
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, "bigint", resolution.ReturnType.EngineName)
	})

	t.Run("MOD with a decimal operand is that decimal", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "mod", "", []sqlmodel.SQLType{
			{Category: sqlmodel.TypeCategoryDecimal, EngineName: "decimal"},
			{Category: sqlmodel.TypeCategoryInteger, EngineName: "int", Width: 32},
		})
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, sqlmodel.TypeCategoryDecimal, resolution.ReturnType.Category)
		assert.Equal(t, sqlmodel.FunctionNullableCalledOnNull, resolution.NullableBehaviour)
	})

	t.Run("ABS keeps a BIGINT argument's type", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "abs", "", []sqlmodel.SQLType{
			{Category: sqlmodel.TypeCategoryInteger, EngineName: "bigint", Width: 64},
		})
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, "bigint", resolution.ReturnType.EngineName)
	})

	t.Run("NVL2 is the promoted type of its results, NULL when any argument may be", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "nvl2", "", []sqlmodel.SQLType{
			{Category: sqlmodel.TypeCategoryInteger, EngineName: "int", Width: 32, Nullable: true},
			{Category: sqlmodel.TypeCategoryInteger, EngineName: "int", Width: 32},
			{Category: sqlmodel.TypeCategoryDecimal, EngineName: "decimal"},
		})
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, sqlmodel.TypeCategoryDecimal, resolution.ReturnType.Category)
		assert.True(t, resolution.ReturnType.Nullable)
	})

	t.Run("DECODE_ORACLE without a default may be NULL", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "decode_oracle", "", []sqlmodel.SQLType{
			{Category: sqlmodel.TypeCategoryInteger, EngineName: "int", Width: 32},
			{Category: sqlmodel.TypeCategoryInteger, EngineName: "int", Width: 32},
			{Category: sqlmodel.TypeCategoryText, EngineName: "varchar"},
		})
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, sqlmodel.TypeCategoryText, resolution.ReturnType.Category)
		assert.Equal(t, sqlmodel.FunctionNullableCalledOnNull, resolution.NullableBehaviour)
	})

	t.Run("DECODE_ORACLE with a non-null default and results is never NULL", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "decode_oracle", "", []sqlmodel.SQLType{
			{Category: sqlmodel.TypeCategoryInteger, EngineName: "int", Width: 32},
			{Category: sqlmodel.TypeCategoryInteger, EngineName: "int", Width: 32},
			{Category: sqlmodel.TypeCategoryText, EngineName: "varchar"},
			{Category: sqlmodel.TypeCategoryText, EngineName: "varchar"},
		})
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, sqlmodel.FunctionNullableNeverNull, resolution.NullableBehaviour)
	})

	t.Run("ROUND of a decimal keeps the decimal", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "round", "", []sqlmodel.SQLType{
			{Category: sqlmodel.TypeCategoryDecimal, EngineName: "decimal"},
			{Category: sqlmodel.TypeCategoryInteger, EngineName: "int", Width: 32},
		})
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, sqlmodel.TypeCategoryDecimal, resolution.ReturnType.Category)
	})

	t.Run("TRUNCATE of an integer is a BIGINT", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "truncate", "", []sqlmodel.SQLType{
			{Category: sqlmodel.TypeCategoryInteger, EngineName: "int", Width: 32},
			{Category: sqlmodel.TypeCategoryInteger, EngineName: "int", Width: 32},
		})
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, "bigint", resolution.ReturnType.EngineName)
	})

	t.Run("ABS of a string reads it as a DOUBLE", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "abs", "", []sqlmodel.SQLType{
			{Category: sqlmodel.TypeCategoryText, EngineName: "varchar"},
		})
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, "double", resolution.ReturnType.EngineName)
	})

	t.Run("IFNULL returns promoted type", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "IFNULL", "", []sqlmodel.SQLType{
			{Category: sqlmodel.TypeCategoryInteger, EngineName: "int"},
			{Category: sqlmodel.TypeCategoryFloat, EngineName: "double"},
		})
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, sqlmodel.TypeCategoryFloat, resolution.ReturnType.Category)
	})

	t.Run("IFNULL with too few arguments returns nil", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "IFNULL", "", []sqlmodel.SQLType{
			{Category: sqlmodel.TypeCategoryInteger, EngineName: "int"},
		})
		require.NoError(t, err)
		assert.Nil(t, resolution)
	})

	t.Run("COALESCE with known types returns promoted type", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "COALESCE", "", []sqlmodel.SQLType{
			{Category: sqlmodel.TypeCategoryInteger, EngineName: "int"},
			{Category: sqlmodel.TypeCategoryDecimal, EngineName: "decimal"},
			{Category: sqlmodel.TypeCategoryFloat, EngineName: "double"},
		})
		require.NoError(t, err)
		require.NotNil(t, resolution)

		assert.Equal(t, sqlmodel.TypeCategoryFloat, resolution.ReturnType.Category)
	})

	t.Run("COALESCE with all unknown returns unknown", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "COALESCE", "", []sqlmodel.SQLType{
			{Category: sqlmodel.TypeCategoryUnknown, EngineName: ""},
			{Category: sqlmodel.TypeCategoryUnknown, EngineName: ""},
		})
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, sqlmodel.TypeCategoryUnknown, resolution.ReturnType.Category)
	})

	t.Run("GREATEST delegates to coalesce resolver", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "GREATEST", "", []sqlmodel.SQLType{
			{Category: sqlmodel.TypeCategoryInteger, EngineName: "int"},
			{Category: sqlmodel.TypeCategoryText, EngineName: "varchar"},
		})
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, sqlmodel.TypeCategoryText, resolution.ReturnType.Category)
	})

	t.Run("LEAST delegates to coalesce resolver", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "LEAST", "", []sqlmodel.SQLType{
			{Category: sqlmodel.TypeCategoryFloat, EngineName: "float"},
			{Category: sqlmodel.TypeCategoryDecimal, EngineName: "decimal"},
		})
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, sqlmodel.TypeCategoryFloat, resolution.ReturnType.Category)
	})

	t.Run("GROUP_CONCAT returns text aggregate", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "GROUP_CONCAT", "", nil)
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, sqlmodel.TypeCategoryText, resolution.ReturnType.Category)
		assert.True(t, resolution.IsAggregate)
	})

	t.Run("JSON_EXTRACT returns JSON that is NULL for a missing path", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "JSON_EXTRACT", "", nil)
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, sqlmodel.TypeCategoryJSON, resolution.ReturnType.Category)
		assert.Equal(t, sqlmodel.FunctionNullableCalledOnNull, resolution.NullableBehaviour)
	})

	t.Run("JSON_UNQUOTE returns text", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "JSON_UNQUOTE", "", nil)
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, sqlmodel.TypeCategoryText, resolution.ReturnType.Category)
	})

	t.Run("CONCAT returns text", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "CONCAT", "", nil)
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, sqlmodel.TypeCategoryText, resolution.ReturnType.Category)
	})

	t.Run("CONCAT_WS returns text", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "CONCAT_WS", "", nil)
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, sqlmodel.TypeCategoryText, resolution.ReturnType.Category)
	})

	t.Run("SUM of float returns double", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "SUM", "", []sqlmodel.SQLType{
			{Category: sqlmodel.TypeCategoryFloat, EngineName: "float"},
		})
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, "double", resolution.ReturnType.EngineName)
		assert.True(t, resolution.IsAggregate)
	})

	t.Run("SUM of integer returns decimal", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "SUM", "", []sqlmodel.SQLType{
			{Category: sqlmodel.TypeCategoryInteger, EngineName: "int"},
		})
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, "decimal", resolution.ReturnType.EngineName)
	})

	t.Run("SUM with no arguments returns nil", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "SUM", "", nil)
		require.NoError(t, err)
		assert.Nil(t, resolution)
	})

	t.Run("AVG returns double", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "AVG", "", nil)
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, "double", resolution.ReturnType.EngineName)
		assert.True(t, resolution.IsAggregate)
	})

	t.Run("MIN returns identity type", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "MIN", "", []sqlmodel.SQLType{
			{Category: sqlmodel.TypeCategoryText, EngineName: "varchar"},
		})
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, "varchar", resolution.ReturnType.EngineName)
		assert.True(t, resolution.IsAggregate)
	})

	t.Run("MAX returns identity type", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "MAX", "", []sqlmodel.SQLType{
			{Category: sqlmodel.TypeCategoryInteger, EngineName: "bigint"},
		})
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, "bigint", resolution.ReturnType.EngineName)
		assert.True(t, resolution.IsAggregate)
	})

	t.Run("MIN with no arguments returns nil", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "MIN", "", nil)
		require.NoError(t, err)
		assert.Nil(t, resolution)
	})

	t.Run("COUNT returns bigint", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "COUNT", "", nil)
		require.NoError(t, err)
		require.NotNil(t, resolution)
		assert.Equal(t, "bigint", resolution.ReturnType.EngineName)
		assert.Equal(t, sqlmodel.FunctionNullableNeverNull, resolution.NullableBehaviour)
		assert.True(t, resolution.IsAggregate)
	})

	t.Run("unknown function returns nil nil", func(t *testing.T) {
		t.Parallel()

		resolution, err := resolver.ResolveFunctionCall(nil, "UNKNOWN_FUNC", "", nil)
		require.NoError(t, err)
		assert.Nil(t, resolution)
	})
}

func TestPromoteType_CrossCategory(t *testing.T) {
	t.Parallel()

	engine := New()

	t.Run("different categories returns left", func(t *testing.T) {
		t.Parallel()

		left := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "varchar"}
		right := sqlmodel.NewIntegerType("int", 32, false)
		result := engine.PromoteType(left, right)
		assert.Equal(t, "varchar", result.EngineName, "different categories should return left")
	})

	t.Run("default category returns left", func(t *testing.T) {
		t.Parallel()

		left := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryJSON, EngineName: "json"}
		right := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryJSON, EngineName: "json"}
		result := engine.PromoteType(left, right)
		assert.Equal(t, "json", result.EngineName, "non-integer non-float same category should return left")
	})
}

func TestPromoteTypes_Internal(t *testing.T) {
	t.Parallel()

	t.Run("left unknown returns right", func(t *testing.T) {
		t.Parallel()

		left := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUnknown}
		right := sqlmodel.NewIntegerType("int", 32, false)
		result := promoteTypes(left, right)
		assert.Equal(t, "int", result.EngineName)
	})

	t.Run("right unknown returns left", func(t *testing.T) {
		t.Parallel()

		left := sqlmodel.NewFloatType("float", 32)
		right := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUnknown}
		result := promoteTypes(left, right)
		assert.Equal(t, "float", result.EngineName)
	})

	t.Run("same category returns left", func(t *testing.T) {
		t.Parallel()

		left := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryDecimal, EngineName: "decimal"}
		right := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryDecimal, EngineName: "numeric"}
		result := promoteTypes(left, right)
		assert.Equal(t, "decimal", result.EngineName)
	})

	t.Run("integer vs text promotes to text", func(t *testing.T) {
		t.Parallel()

		left := sqlmodel.NewIntegerType("int", 32, false)
		right := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"}
		result := promoteTypes(left, right)
		assert.Equal(t, sqlmodel.TypeCategoryText, result.Category)
	})

	t.Run("float vs decimal promotes to float, as MySQL gives a DOUBLE", func(t *testing.T) {
		t.Parallel()

		left := sqlmodel.NewFloatType("double", 64)
		right := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryDecimal, EngineName: "decimal"}
		result := promoteTypes(left, right)
		assert.Equal(t, sqlmodel.TypeCategoryFloat, result.Category)
		assert.Equal(t, sqlmodel.TypeCategoryFloat, promoteTypes(right, left).Category)
	})

	t.Run("two integers promote to the wider", func(t *testing.T) {
		t.Parallel()

		smallint := sqlmodel.NewIntegerType("smallint", 16, false)
		bigint := sqlmodel.NewIntegerType("bigint", 64, false)
		assert.Equal(t, "bigint", promoteTypes(smallint, bigint).EngineName)
		assert.Equal(t, "bigint", promoteTypes(bigint, smallint).EngineName)
	})

	t.Run("unknown category has zero rank", func(t *testing.T) {
		t.Parallel()

		left := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryJSON, EngineName: "json"}
		right := sqlmodel.NewIntegerType("int", 32, false)
		result := promoteTypes(left, right)
		assert.Equal(t, sqlmodel.TypeCategoryInteger, result.Category,
			"json has rank 0 so integer with rank 1 should win")
	})
}

func TestFunctionCatalogueBuilder_TypeAccessors(t *testing.T) {
	t.Parallel()

	engine := New(WithExtraFunctions(func(builder *FunctionCatalogueBuilder) {

		builder.NullOnNull("test_bigint", builder.Args(Arg{Name: "x", Type: builder.Bigint()}), builder.Bigint())
		builder.NullOnNull("test_float", builder.Args(Arg{Name: "x", Type: builder.Float()}), builder.Float())
		builder.NullOnNull("test_double", builder.Args(Arg{Name: "x", Type: builder.Double()}), builder.Double())
		builder.NullOnNull("test_decimal", builder.Args(Arg{Name: "x", Type: builder.Decimal()}), builder.Decimal())
		builder.NullOnNull("test_text", builder.Args(Arg{Name: "x", Type: builder.Text()}), builder.Text())
		builder.NullOnNull("test_varchar", builder.Args(Arg{Name: "x", Type: builder.Varchar()}), builder.Varchar())
		builder.NullOnNull("test_boolean", builder.Args(Arg{Name: "x", Type: builder.Boolean()}), builder.Boolean())
		builder.NullOnNull("test_bytea", builder.Args(Arg{Name: "x", Type: builder.Bytea()}), builder.Bytea())
		builder.NullOnNull("test_date", builder.Args(Arg{Name: "x", Type: builder.Date()}), builder.Date())
		builder.NullOnNull("test_time", builder.Args(Arg{Name: "x", Type: builder.Time()}), builder.Time())
		builder.NullOnNull("test_datetime", builder.Args(Arg{Name: "x", Type: builder.Datetime()}), builder.Datetime())
		builder.NullOnNull("test_timestamp", builder.Args(Arg{Name: "x", Type: builder.Timestamp()}), builder.Timestamp())
		builder.NullOnNull("test_json", builder.Args(Arg{Name: "x", Type: builder.JSON()}), builder.JSON())
		builder.NullOnNull("test_geometry", builder.Args(Arg{Name: "x", Type: builder.Geometry()}), builder.Geometry())
		builder.Variadic("test_variadic",
			builder.Args(Arg{Name: "values", Type: builder.Text()}),
			1,
			builder.Text(),
		)
	}))

	catalogue := engine.BuiltinFunctions()

	expectedFunctions := []string{
		"test_bigint", "test_float", "test_double", "test_decimal",
		"test_text", "test_varchar", "test_boolean", "test_bytea",
		"test_date", "test_time", "test_datetime", "test_timestamp",
		"test_json", "test_geometry", "test_variadic",
	}
	for _, name := range expectedFunctions {
		signatures, exists := catalogue.Functions[name]
		assert.True(t, exists, "expected function %q to be registered", name)
		assert.NotEmpty(t, signatures, "expected at least one signature for %q", name)
	}

	variadicSignatures := catalogue.Functions["test_variadic"]
	require.Len(t, variadicSignatures, 1)
	assert.True(t, variadicSignatures[0].IsVariadic)
	assert.Equal(t, 1, variadicSignatures[0].MinArguments)
}

func TestTableValuedFunctionColumnsFromCatalogue_CompositeType(t *testing.T) {
	t.Parallel()

	engine := New()

	t.Run("resolves composite type columns from set-returning function", func(t *testing.T) {
		t.Parallel()

		catalogue := &sqlmodel.Catalogue{
			SearchPath: []string{"public"},
			Schemas: map[string]*sqlmodel.Schema{
				"public": {
					Name: "public",
					Functions: map[string][]*sqlmodel.Function{
						"get_users": {
							{
								Name:       "get_users",
								ReturnsSet: true,
								ReturnType: sqlmodel.SQLType{
									Category:   sqlmodel.TypeCategoryUnknown,
									EngineName: "user_type",
								},
							},
						},
					},
					CompositeTypes: map[string]*sqlmodel.CompositeType{
						"user_type": {
							Fields: []sqlmodel.Column{
								{Name: "id", SQLType: sqlmodel.NewIntegerType("int", 32, false)},
								{Name: "name", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "varchar"}},
							},
						},
					},
				},
			},
		}

		columns := catalogueFunctionColumns(t, engine, catalogue, "get_users")
		require.Len(t, columns, 2)
		assert.Equal(t, "id", columns[0].Name)
		assert.Equal(t, "name", columns[1].Name)
	})

	t.Run("returns nil for non-set-returning function", func(t *testing.T) {
		t.Parallel()

		catalogue := &sqlmodel.Catalogue{
			SearchPath: []string{"public"},
			Schemas: map[string]*sqlmodel.Schema{
				"public": {
					Name: "public",
					Functions: map[string][]*sqlmodel.Function{
						"scalar_fn": {
							{
								Name:       "scalar_fn",
								ReturnsSet: false,
								ReturnType: sqlmodel.SQLType{
									Category:   sqlmodel.TypeCategoryInteger,
									EngineName: "int",
								},
							},
						},
					},
				},
			},
		}

		columns := catalogueFunctionColumns(t, engine, catalogue, "scalar_fn")
		assert.Nil(t, columns)
	})

	t.Run("resolves composite type from different schema", func(t *testing.T) {
		t.Parallel()

		catalogue := &sqlmodel.Catalogue{
			SearchPath: []string{"public"},
			Schemas: map[string]*sqlmodel.Schema{
				"public": {
					Name: "public",
					Functions: map[string][]*sqlmodel.Function{
						"get_items": {
							{
								Name:       "get_items",
								ReturnsSet: true,
								ReturnType: sqlmodel.SQLType{
									Category:   sqlmodel.TypeCategoryUnknown,
									EngineName: "item_type",
									Schema:     "other",
								},
							},
						},
					},
				},
				"other": {
					Name: "other",
					CompositeTypes: map[string]*sqlmodel.CompositeType{
						"item_type": {
							Fields: []sqlmodel.Column{
								{Name: "item_id", SQLType: sqlmodel.NewIntegerType("int", 32, false)},
							},
						},
					},
				},
			},
		}

		columns := catalogueFunctionColumns(t, engine, catalogue, "get_items")
		require.Len(t, columns, 1)
		assert.Equal(t, "item_id", columns[0].Name)
	})
}

func TestTableValuedFunctionColumnsFromCatalogueFollowsTheSearchPath(t *testing.T) {
	t.Parallel()

	setReturning := func(compositeType string) []*sqlmodel.Function {
		return []*sqlmodel.Function{{
			Name:       "rows_of",
			ReturnsSet: true,
			ReturnType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUnknown, EngineName: compositeType},
		}}
	}
	schemaWith := func(name, column string) *sqlmodel.Schema {
		return &sqlmodel.Schema{
			Name:      name,
			Functions: map[string][]*sqlmodel.Function{"rows_of": setReturning("row_type")},
			CompositeTypes: map[string]*sqlmodel.CompositeType{
				"row_type": {Fields: []sqlmodel.Column{{Name: column, SQLType: sqlmodel.NewIntegerType("int", 32, false)}}},
			},
		}
	}
	schemas := map[string]*sqlmodel.Schema{"alpha": schemaWith("alpha", "from_alpha"), "beta": schemaWith("beta", "from_beta")}

	testCases := []struct {
		name         string
		searchPath   []string
		functionName string
		wantColumn   string
	}{
		{name: "the first schema on the path wins over an earlier name", searchPath: []string{"beta", "alpha"},
			functionName: "rows_of", wantColumn: "from_beta"},
		{name: "a schema off the path is passed over", searchPath: []string{"alpha"}, functionName: "rows_of",
			wantColumn: "from_alpha"},
		{name: "a qualified name reads its own schema", searchPath: []string{"alpha"}, functionName: "beta.rows_of",
			wantColumn: "from_beta"},
		{name: "a name in another case finds the function, as MySQL ignores the case of function names",
			searchPath: []string{"alpha"}, functionName: "ROWS_OF", wantColumn: "from_alpha"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue := &sqlmodel.Catalogue{Schemas: schemas, SearchPath: testCase.searchPath, Identifiers: New().Info().Identifiers}

			columns := catalogueFunctionColumns(t, New(), catalogue, testCase.functionName)

			require.Len(t, columns, 1)
			assert.Equal(t, testCase.wantColumn, columns[0].Name)
		})
	}
}

func TestTableValuedFunctionColumnsFromCatalogueSkipsMissingEntries(t *testing.T) {
	t.Parallel()

	setReturning := &sqlmodel.Function{
		Name:       "rows_of",
		ReturnsSet: true,
		ReturnType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUnknown, EngineName: "row_type", Schema: "elsewhere"},
	}

	testCases := []struct {
		name      string
		catalogue *sqlmodel.Catalogue
	}{
		{name: "a nil catalogue", catalogue: nil},
		{name: "a nil schema", catalogue: &sqlmodel.Catalogue{Schemas: map[string]*sqlmodel.Schema{"": nil}}},
		{name: "a nil overload", catalogue: &sqlmodel.Catalogue{Schemas: map[string]*sqlmodel.Schema{
			"": {Functions: map[string][]*sqlmodel.Function{"rows_of": {nil}}},
		}}},
		{name: "a nil composite type and a nil schema it names", catalogue: &sqlmodel.Catalogue{Schemas: map[string]*sqlmodel.Schema{
			"":          {Functions: map[string][]*sqlmodel.Function{"rows_of": {setReturning}}, CompositeTypes: map[string]*sqlmodel.CompositeType{"row_type": nil}},
			"elsewhere": nil,
		}}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			columns, err := New().TableValuedFunctionColumnsFromCatalogue(t.Context(), testCase.catalogue, "rows_of")

			require.NoError(t, err)
			assert.Nil(t, columns)
		})
	}
}

func TestInfoReturnsACopyTheCallerMayChange(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		change  func(info *dialect.Info)
		options []Option
	}{
		{name: "the system columns", change: func(info *dialect.Info) { info.SystemColumns[0] = "changed" }},
		{name: "the operator features", change: func(info *dialect.Info) { clear(info.OperatorFeatures) }},
		{name: "the operator features of a dialect without JSON arrows", options: []Option{WithJSONArrows(false)},
			change: func(info *dialect.Info) { info.OperatorFeatures[operatorJSONExtract] = ast.SQLFeatureJSONOp }},
		{name: "the operator features shared with a dialect without JSON arrows", options: []Option{WithJSONArrows(false)},
			change: func(info *dialect.Info) { clear(info.OperatorFeatures) }},
		{name: "the placeholder forms", change: func(info *dialect.Info) { info.Placeholders.Sigils[0].Prefix = "$" }},
		{name: "the JSON path functions", change: func(info *dialect.Info) { info.Filters.JSONPathFunctions[0] = "changed" }},
		{name: "the casts", change: func(info *dialect.Info) { info.Filters.Casts[0].Before = "changed(" }},
		{name: "the cast types", change: func(info *dialect.Info) { info.Filters.CastTypes[0] = "changed" }},
		{name: "the drivers", change: func(info *dialect.Info) { info.Drivers[0] = "changed" }},
		{name: "the search path", change: func(info *dialect.Info) { info.SchemaSearchPath[0] = "changed" }},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(testCase.options...)
			before := withoutLexicalCallbacks(engine.Info())
			withJSONArrows := withoutLexicalCallbacks(New().Info())

			testCase.change(new(engine.Info()))

			assert.Equal(t, before, withoutLexicalCallbacks(engine.Info()))
			assert.Equal(t, before, withoutLexicalCallbacks(New(testCase.options...).Info()))
			assert.Equal(t, withJSONArrows, withoutLexicalCallbacks(New().Info()))
		})
	}
}

func withoutLexicalCallbacks(info dialect.Info) dialect.Info {
	info.Lexical.ProceduralBlock = nil
	info.Lexical.RequiresNoTransaction = nil
	return info
}

func TestADialectWhoseBuiltinsDidNotDecodeReportsItWhenUsed(t *testing.T) {
	t.Parallel()

	errBroken := errors.New("a spec line did not decode")
	configured := settings{}
	configured.Name = "mysql"
	engine := newDialect(configured, func() (*dialectkit.FunctionSpec, error) { return nil, errBroken })
	statements, err := engine.ParseStatements(t.Context(), "CREATE TABLE t (a INT); SELECT 1")
	require.NoError(t, err)
	require.Len(t, statements, 2)

	testCases := []struct {
		name string
		use  func(ctx context.Context) error
	}{
		{name: "ApplyDDL", use: func(ctx context.Context) error {
			_, applyError := engine.ApplyDDL(ctx, statements[0])
			return applyError
		}},
		{name: "AnalyseQuery", use: func(ctx context.Context) error {
			_, analyseError := engine.AnalyseQuery(ctx, nil, statements[1])
			return analyseError
		}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := testCase.use(t.Context())

			require.ErrorIs(t, err, errBroken)
			assert.Contains(t, err.Error(), "mysql: building the dialect: registering the built-in functions")
		})
	}
}

func TestADialectBuiltWholeReportsNoBuildError(t *testing.T) {
	t.Parallel()

	assert.NoError(t, New().initErr)
}

func TestHooksAreAskedMostRecentFirst(t *testing.T) {
	t.Parallel()

	first := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "first")
	second := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "second")
	allowed, refused := true, false
	engine := New(
		WithPromoteTypeHook(func(sqlmodel.SQLType, sqlmodel.SQLType) *sqlmodel.SQLType { return &first }),
		WithPromoteTypeHook(func(sqlmodel.SQLType, sqlmodel.SQLType) *sqlmodel.SQLType { return &second }),
		WithImplicitCastHook(func(sqlmodel.SQLType, sqlmodel.SQLType) *bool { return &refused }),
		WithImplicitCastHook(func(sqlmodel.SQLType, sqlmodel.SQLType) *bool { return &allowed }),
		WithTypeNormaliserHook(func(string, []int) *sqlmodel.SQLType { return &first }),
		WithTypeNormaliserHook(func(string, []int) *sqlmodel.SQLType { return nil }),
		WithFunctionCallHook(func(string, []sqlmodel.SQLType) *ast.FunctionResolution {
			return &ast.FunctionResolution{ReturnType: first}
		}),
		WithFunctionCallHook(func(string, []sqlmodel.SQLType) *ast.FunctionResolution {
			return &ast.FunctionResolution{ReturnType: second}
		}),
	)
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")

	resolution, err := engine.ResolveFunctionCall(t.Context(), nil, "lower", "", []sqlmodel.SQLType{text})

	require.NoError(t, err)
	require.NotNil(t, resolution)
	assert.Equal(t, "second", resolution.ReturnType.EngineName)
	assert.Equal(t, "second", engine.PromoteType(text, text).EngineName)
	assert.True(t, engine.CanImplicitCast(text, text))
	assert.Equal(t, "first", engine.NormaliseTypeName("anything").EngineName)
}

func TestCanImplicitCast_FloatToDecimal(t *testing.T) {
	t.Parallel()

	engine := New()
	assert.True(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryFloat, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "")),
		"float to decimal cast should be allowed")
}

func TestIsParsedStatement(t *testing.T) {
	t.Parallel()

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), "SELECT 1;")
	require.NoError(t, err)
	require.Len(t, statements, 1)

	parsed, ok := statements[0].Raw.(*parsedStatement)
	require.True(t, ok)
	parsed.IsParsedStatement()
}

func TestEntryPointsRejectAStatementParsedByAnotherDialect(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		call func(ctx context.Context, engine *Dialect, statement ast.ParsedStatement) (any, error)
		name string
	}{
		{
			name: "ApplyDDL",
			call: func(ctx context.Context, engine *Dialect, statement ast.ParsedStatement) (any, error) {
				return engine.ApplyDDL(ctx, statement)
			},
		},
		{
			name: "AnalyseQuery",
			call: func(ctx context.Context, engine *Dialect, statement ast.ParsedStatement) (any, error) {
				return engine.AnalyseQuery(ctx, nil, statement)
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, err := testCase.call(t.Context(), New(), ast.ParsedStatement{Raw: &fakeStatement{}})

			assert.Nil(t, result)
			require.ErrorIs(t, err, dialect.ErrForeignStatement)
			assert.EqualError(t, err, "the statement was parsed by another dialect: *mysql.fakeStatement")
		})
	}
}

func TestResolveFunctionCall_ViaEngine(t *testing.T) {
	t.Parallel()

	engine := New()

	resolution, err := engine.ResolveFunctionCall(t.Context(), nil, "COUNT", "", nil)
	require.NoError(t, err)
	require.NotNil(t, resolution)
	assert.Equal(t, "bigint", resolution.ReturnType.EngineName)
}

func TestApplyDDLReturnsSyntaxErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sql       string
		wantError string
	}{
		{name: "rename column without TO", sql: "ALTER TABLE t RENAME COLUMN a b", wantError: "expected keyword TO"},
		{name: "unterminated DEFAULT expression", sql: "CREATE TABLE t (a INT DEFAULT (1", wantError: "unmatched parenthesis"},
		{name: "unterminated CHECK expression", sql: "CREATE TABLE t (a INT CHECK (a > 0", wantError: "unmatched parenthesis"},
		{name: "unterminated REFERENCES column list", sql: "CREATE TABLE t (a INT REFERENCES u(id", wantError: "unmatched parenthesis"},
		{name: "unterminated generated column expression", sql: "CREATE TABLE t (a INT GENERATED ALWAYS AS (a + 1", wantError: "unmatched parenthesis"},
		{name: "unterminated ON UPDATE precision", sql: "CREATE TABLE t (a TIMESTAMP ON UPDATE CURRENT_TIMESTAMP(3", wantError: "unmatched parenthesis"},
		{name: "unterminated group in an unrecognised ALTER action", sql: "ALTER TABLE t ENGINE = (x", wantError: "unmatched parenthesis"},
		{name: "index without ON", sql: "CREATE INDEX i t (a)", wantError: "expected keyword ON"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.NotEmpty(t, statements)

			mutation, err := engine.ApplyDDL(t.Context(), statements[0])

			require.Error(t, err)
			assert.Nil(t, mutation)
			assert.Contains(t, err.Error(), testCase.wantError)
			assert.NotContains(t, err.Error(), "panic")
		})
	}
}

func TestAnalyseQueryReturnsSyntaxErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		wantSentinel error
		name         string
		sql          string
		wantError    string
		options      []Option
	}{
		{name: "UPDATE without SET", sql: "UPDATE t a = 1", wantError: "expected keyword SET"},
		{name: "UPDATE with a malformed qualified table", sql: "UPDATE db.(x) SET a = 1", wantError: "expected identifier"},
		{name: "compound arm that is not a query", sql: "SELECT 1 UNION 2", wantError: "expected keyword SELECT"},
		{name: "unterminated USING list", sql: "SELECT * FROM a JOIN b USING (id", wantError: "unmatched parenthesis"},
		{name: "unterminated index hint list", sql: "SELECT * FROM t USE INDEX (i", wantError: "unmatched parenthesis"},
		{name: "unterminated PARTITION list", sql: "INSERT INTO t PARTITION (p1 VALUES (1)", wantError: "unmatched parenthesis"},
		{name: "unterminated CAST type modifiers", sql: "SELECT CAST(? AS DECIMAL(10 FROM t", wantError: "parsing CAST type modifiers"},
		{name: "unterminated ANY operand", sql: "SELECT a = ANY (1 FROM t", wantError: "ANY/ALL operand"},
		{
			name:         "expression nested past the cap inside a CTE",
			sql:          "WITH c AS (SELECT ((((((((((1)))))))))) AS x) SELECT x FROM c",
			options:      []Option{WithMaxParseDepth(8)},
			wantSentinel: dialectkit.ErrParseDepth,
		},
		{
			name:         "expression nested past the cap inside a scalar subquery",
			sql:          "SELECT (SELECT ((((((((((1))))))))))) FROM t",
			options:      []Option{WithMaxParseDepth(8)},
			wantSentinel: dialectkit.ErrParseDepth,
		},
		{
			name:         "expression nested past the cap inside a derived table",
			sql:          "SELECT * FROM (SELECT ((((((((((1)))))))))) AS x) d",
			options:      []Option{WithMaxParseDepth(8)},
			wantSentinel: dialectkit.ErrParseDepth,
		},
		{
			name:         "expression nested past the cap inside an IN subquery",
			sql:          "SELECT a FROM t WHERE a IN (SELECT ((((((((((1)))))))))))",
			options:      []Option{WithMaxParseDepth(8)},
			wantSentinel: dialectkit.ErrParseDepth,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(testCase.options...)
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.NotEmpty(t, statements)

			analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])

			require.Error(t, err)
			assert.Nil(t, analysis)
			assert.NotContains(t, err.Error(), "panic")
			if testCase.wantSentinel != nil {
				assert.ErrorIs(t, err, testCase.wantSentinel)
				return
			}
			assert.Contains(t, err.Error(), testCase.wantError)
		})
	}
}

func TestApplyDDLRecoversFromHandlerPanic(t *testing.T) {
	original := ddlHandlers[statementKindCreateTable]
	ddlHandlers[statementKindCreateTable] = func(*parser, *Dialect) (*ast.CatalogueMutation, error) {
		panic("handler bug")
	}
	t.Cleanup(func() { ddlHandlers[statementKindCreateTable] = original })

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), "CREATE TABLE t (a INT)")
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])

	require.ErrorAs(t, err, new(*dialectkit.PanicError))
	assert.Nil(t, mutation)
	assert.Equal(t, "mysql: apply DDL: panic: handler bug", err.Error())
	assert.ErrorIs(t, err, fault.ErrPanic)
}

func TestAnalyseQueryRecoversFromAnalyserPanic(t *testing.T) {
	original := queryAnalysers[statementKindSelect]
	queryAnalysers[statementKindSelect] = func(*parser) (*ast.RawQueryAnalysis, error) {
		panic("analyser bug")
	}
	t.Cleanup(func() { queryAnalysers[statementKindSelect] = original })

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), "SELECT 1")
	require.NoError(t, err)

	analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])

	require.ErrorAs(t, err, new(*dialectkit.PanicError))
	assert.Nil(t, analysis)
	assert.Equal(t, "mysql: analyse query: panic: analyser bug", err.Error())
	assert.ErrorIs(t, err, fault.ErrPanic)
}

func TestWithMaxTokensPerStatement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		limit int
		want  int
	}{
		{name: "positive limit is applied", limit: 500, want: 500},
		{name: "zero keeps the default", limit: 0, want: dialectkit.DefaultMaxTokensPerStatement},
		{name: "negative keeps the default", limit: -3, want: dialectkit.DefaultMaxTokensPerStatement},
		{name: "a limit past the ceiling stops at the ceiling", limit: 2 * dialectkit.MaxTokensPerStatementCeiling,
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

func TestTokenBudgetRejectsWalkedStatements(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		sql   string
		isDDL bool
	}{
		{name: "DDL statement over budget", sql: "CREATE TABLE t (a INT, b TEXT, c BLOB)", isDDL: true},
		{name: "query over budget", sql: "SELECT a, b, c FROM t WHERE a = ?", isDDL: false},
		{name: "REPLACE over budget", sql: "REPLACE INTO t (a, b) VALUES (?, ?)", isDDL: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(WithMaxTokensPerStatement(5))
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)

			if testCase.isDDL {
				_, err = engine.ApplyDDL(t.Context(), statements[0])
			} else {
				_, err = engine.AnalyseQuery(t.Context(), nil, statements[0])
			}

			require.ErrorIs(t, err, dialectkit.ErrTokenBudget)
			assert.Contains(t, err.Error(), "more than 5 tokens")
		})
	}
}

func TestTokenBudgetIgnoresStatementsTheParserDoesNotWalk(t *testing.T) {
	t.Parallel()

	engine := New(WithMaxTokensPerStatement(5))
	statements, err := engine.ParseStatements(t.Context(), "INSERT INTO t (a, b, c) VALUES (1, 2, 3); SET NAMES utf8mb4")
	require.NoError(t, err)
	require.Len(t, statements, 2)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	assert.Nil(t, mutation)

	analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[1])
	require.NoError(t, err)
	assert.Equal(t, &ast.RawQueryAnalysis{}, analysis)
}

func TestParseStatementsRecordsPerStatementByteLength(t *testing.T) {
	t.Parallel()

	const first = "SELECT 1"
	const second = "SELECT 22"
	sql := first + "; " + second

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.Len(t, statements, 2)

	assert.Equal(t, 0, statements[0].Location)
	assert.Equal(t, len(first), statements[0].Length,
		"first statement Length should span only its own tokens, not the whole source")

	secondLocation := len(first) + len("; ")
	assert.Equal(t, secondLocation, statements[1].Location)
	assert.Equal(t, len(second), statements[1].Length,
		"second statement Length should span only its own tokens")
}

func TestApplyDDLReturnsErrorsForTruncatedAndMismatchedStatements(t *testing.T) {
	t.Parallel()

	engine := New()
	for kind, handler := range ddlHandlers {
		if handler == nil {
			continue
		}
		for _, sql := range sampleDDLStatements {
			for _, tokens := range statementPrefixes(t, sql) {
				statement := ast.ParsedStatement{Raw: &parsedStatement{tokens: tokens, kind: statementKind(kind)}}

				_, err := engine.ApplyDDL(t.Context(), statement)

				if err != nil {
					assert.NotContains(t, err.Error(), "panic", "kind %d over %d tokens of %q", kind, len(tokens), sql)
				}
			}
		}
	}
}

func TestAnalyseQueryReturnsErrorsForTruncatedAndMismatchedStatements(t *testing.T) {
	t.Parallel()

	engine := New()
	for kind, analyser := range queryAnalysers {
		if analyser == nil {
			continue
		}
		for _, sql := range append(sampleQueryStatements, sampleDDLStatements...) {
			for _, tokens := range statementPrefixes(t, sql) {
				statement := ast.ParsedStatement{Raw: &parsedStatement{tokens: tokens, kind: statementKind(kind)}}

				_, err := engine.AnalyseQuery(t.Context(), nil, statement)

				if err != nil {
					assert.NotContains(t, err.Error(), "panic", "kind %d over %d tokens of %q", kind, len(tokens), sql)
				}
			}
		}
	}
}

func statementPrefixes(t *testing.T, sql string) [][]token {
	t.Helper()

	statements := splitTokens(t, sql)
	require.NotEmpty(t, statements)
	whole := statements[0]

	prefixes := make([][]token, 0, len(whole)+1)
	for length := range len(whole) + 1 {
		prefixes = append(prefixes, whole[:length:length])
	}
	return prefixes
}

func TestOptionsAreAdditive(t *testing.T) {
	t.Parallel()

	textType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "first_text")
	secondType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "second_text")
	engine := New(
		WithExtraTypes(map[string]sqlmodel.SQLType{"First": textType}),
		WithExtraTypes(map[string]sqlmodel.SQLType{"second": secondType}),
		WithExtraFunctions(func(builder *FunctionCatalogueBuilder) {
			builder.NeverNull("first_function", nil, builder.Type("first"))
		}),
		WithExtraFunctions(func(builder *FunctionCatalogueBuilder) {
			builder.NeverNull("second_function", nil, builder.Text())
		}),
		WithTypeNormaliserHook(func(name string, _ []int) *sqlmodel.SQLType {
			if name == "hooked" || name == "overridden" {
				return &textType
			}
			return nil
		}),
		WithTypeNormaliserHook(func(name string, _ []int) *sqlmodel.SQLType {
			if name == "overridden" {
				return &secondType
			}
			return nil
		}),
		WithImplicitCastHook(func(sqlmodel.SQLType, sqlmodel.SQLType) *bool { return new(true) }),
		WithImplicitCastHook(func(from, _ sqlmodel.SQLType) *bool {
			if from.Category == sqlmodel.TypeCategoryJSON {
				return new(false)
			}
			return nil
		}),
	)

	require.Equal(t, "first_text", engine.NormaliseTypeName("FIRST").EngineName, "extra types match ignoring case")
	require.Equal(t, "second_text", engine.NormaliseTypeName("second").EngineName)
	require.Equal(t, "first_text", engine.NormaliseTypeName("hooked").EngineName, "an earlier hook still applies")
	require.Equal(t, "second_text", engine.NormaliseTypeName("overridden").EngineName, "the latest hook is consulted first")
	require.Equal(t, "int", engine.NormaliseTypeName("int").EngineName, "declining hooks fall through to the built-ins")
	require.Len(t, engine.BuiltinFunctions().Functions["first_function"], 1)
	require.Len(t, engine.BuiltinFunctions().Functions["second_function"], 1)
	require.Equal(t, "first_text", engine.BuiltinFunctions().Functions["first_function"][0].ReturnType.EngineName,
		"the builder resolves extra types")
	require.True(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "")))
	require.False(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "")))
}

func TestExtraTypesReachEveryTypeNormalisation(t *testing.T) {
	t.Parallel()

	engine := New(WithExtraTypes(map[string]sqlmodel.SQLType{
		"Money": sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "money"),
	}))

	statements, err := engine.ParseStatements(t.Context(), "CREATE TABLE t (price MONEY NOT NULL);"+
		"SELECT CAST(? AS money) AS price FROM t")
	require.NoError(t, err)
	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[1])
	require.NoError(t, err)

	require.Equal(t, sqlmodel.TypeCategoryDecimal, mutation.Columns[0].SQLType.Category)
	require.Len(t, analysis.ParameterReferences, 1)
	require.Equal(t, sqlmodel.TypeCategoryDecimal, analysis.ParameterReferences[0].CastType.Category)
}

func TestSyntaxAndLimitErrorsWrapTheKitSentinels(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		options  []Option
		sentinel error
	}{
		{name: "token budget", sql: "SELECT a, b FROM t", options: []Option{WithMaxTokensPerStatement(2)}, sentinel: dialectkit.ErrTokenBudget},
		{name: "analysis depth", sql: "SELECT (SELECT (SELECT 1)) FROM t", options: []Option{WithMaxParseDepth(2)}, sentinel: dialectkit.ErrParseDepth},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(testCase.options...)
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)

			_, err = engine.AnalyseQuery(t.Context(), nil, statements[0])

			require.ErrorIs(t, err, testCase.sentinel)
		})
	}
}

func TestBuiltinFunctionsCoverTheJSONAndStringFamilies(t *testing.T) {
	t.Parallel()

	functions := New().BuiltinFunctions().Functions

	testCases := []struct {
		name     string
		nullable sqlmodel.FunctionNullableBehaviour
		result   string
	}{
		{name: "concat", nullable: sqlmodel.FunctionNullableReturnsNullOnNull, result: "text"},
		{name: "concat_ws", nullable: sqlmodel.FunctionNullableNullWhenArgumentNull, result: "text"},
		{name: "json_value", nullable: sqlmodel.FunctionNullableCalledOnNull, result: "text"},
		{name: "json_quote", nullable: sqlmodel.FunctionNullableReturnsNullOnNull, result: "text"},
		{name: "json_search", nullable: sqlmodel.FunctionNullableCalledOnNull, result: "json"},
		{name: "json_depth", nullable: sqlmodel.FunctionNullableReturnsNullOnNull, result: "int"},
		{name: "json_overlaps", nullable: sqlmodel.FunctionNullableReturnsNullOnNull, result: "tinyint"},
		{name: "json_merge_patch", nullable: sqlmodel.FunctionNullableReturnsNullOnNull, result: "json"},
		{name: "json_array_append", nullable: sqlmodel.FunctionNullableReturnsNullOnNull, result: "json"},
		{name: "json_arrayagg", nullable: sqlmodel.FunctionNullableCalledOnNull, result: "json"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.NotEmpty(t, functions[testCase.name])
			signature := functions[testCase.name][0]
			require.Equal(t, testCase.nullable, signature.NullableBehaviour)
			require.Equal(t, testCase.result, signature.ReturnType.EngineName)
		})
	}
}

func TestResolveFunctionCallResolvesOnlyFunctionsTheCatalogueRegisters(t *testing.T) {
	t.Parallel()

	engine := New()
	arguments := []sqlmodel.SQLType{
		{Category: sqlmodel.TypeCategoryInteger, EngineName: "int", Width: 32, Nullable: true},
		{Category: sqlmodel.TypeCategoryInteger, EngineName: "int", Width: 32},
	}
	testCases := []struct {
		name     string
		function string
		resolves bool
	}{
		{name: "IFNULL, which MySQL has", function: "ifnull", resolves: true},
		{name: "NVL, which only MariaDB has", function: "nvl", resolves: false},
		{name: "NVL2, which only MariaDB has", function: "nvl2", resolves: false},
		{name: "DECODE_ORACLE, which only MariaDB has", function: "decode_oracle", resolves: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := engine.ResolveFunctionCall(t.Context(), nil, testCase.function, "", arguments)
			require.NoError(t, err)
			require.Equal(t, testCase.resolves, resolution != nil)
		})
	}
}

func TestCoerceArgumentReadsStringsAndNumbersAsDates(t *testing.T) {
	t.Parallel()

	engine := New()
	date := sqlmodel.NewTemporalType("date", sqlmodel.TemporalKindDate, false)
	clock := sqlmodel.NewTemporalType("time", sqlmodel.TemporalKindTime, false)
	testCases := []struct {
		name      string
		argument  sqlmodel.SQLType
		parameter sqlmodel.SQLType
		coerces   bool
	}{
		{name: "a string passed to a date", argument: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar"), parameter: date, coerces: true},
		{name: "a string passed to a time", argument: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), parameter: clock, coerces: true},
		{name: "an integer passed to a date", argument: sqlmodel.NewIntegerType("int", widthInt, false), parameter: date, coerces: true},
		{name: "a decimal passed to a date", argument: sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "decimal"), parameter: date, coerces: true},
		{name: "a JSON document passed to a date", argument: sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "json"), parameter: date, coerces: false},
		{name: "a string passed to an integer", argument: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar"), parameter: sqlmodel.NewIntegerType("int", widthInt, false), coerces: false},
		{name: "a date passed to a string", argument: date, parameter: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), coerces: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			coercion, coerces := engine.CoerceArgument(dialect.CallArgument{Type: testCase.argument, Literal: false}, testCase.parameter)
			require.Equal(t, testCase.coerces, coerces)
			require.Equal(t, testCase.coerces, coercion.MayBeNull)
		})
	}
}

func TestCoercedDateArgumentsLeaveCommonTypesAlone(t *testing.T) {
	t.Parallel()

	engine := New()
	require.False(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar"),
		sqlmodel.NewTemporalType("date", sqlmodel.TemporalKindDate, false)))
	var _ dialect.ArgumentCoercerPort = engine
}

func TestWithoutBuiltinFunctionsWithholdsTheNamedBuiltins(t *testing.T) {
	t.Parallel()

	engine := New(
		WithoutBuiltinFunctions("ANY_VALUE", "regexp_like"),
		WithoutBuiltinFunctions("lag"),
		WithExtraFunctions(func(builder *FunctionCatalogueBuilder) {
			builder.CalledOnNull("lag", builder.Args(Arg{Name: "expression", Type: builder.Any()}), builder.Any())
		}),
	)

	functions := engine.BuiltinFunctions().Functions
	require.NotContains(t, functions, "any_value")
	require.NotContains(t, functions, "regexp_like")
	require.Len(t, functions["lag"], 1, "a withheld name can be registered again by an extra function")
	require.Len(t, functions["lag"][0].Arguments, 1)
	require.Contains(t, functions, "lead", "only the named built-ins are withheld")
	require.Contains(t, New().BuiltinFunctions().Functions, "any_value")

	resolution, err := engine.ResolveFunctionCall(t.Context(), nil, "any_value", "", []sqlmodel.SQLType{sqlmodel.NewIntegerType("int", widthInt, false)})
	require.NoError(t, err)
	require.Nil(t, resolution)
}

func TestWithFunctionCallHook(t *testing.T) {
	t.Parallel()

	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar")
	engine := New(
		WithFunctionCallHook(func(name string, _ []sqlmodel.SQLType) *ast.FunctionResolution {
			if name != "date_add" {
				return nil
			}
			return &ast.FunctionResolution{ReturnType: text, NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull, DataAccess: sqlmodel.DataAccessReadOnly}
		}),
		WithFunctionCallHook(func(name string, _ []sqlmodel.SQLType) *ast.FunctionResolution {
			if name != "unix_timestamp" {
				return nil
			}
			return &ast.FunctionResolution{ReturnType: text, DataAccess: sqlmodel.DataAccessReadOnly}
		}),
	)
	unknown := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")

	shifted, err := engine.ResolveFunctionCall(t.Context(), nil, "DATE_ADD", "", []sqlmodel.SQLType{unknown, unknown})
	require.NoError(t, err)
	require.NotNil(t, shifted)
	require.Equal(t, "varchar", shifted.ReturnType.EngineName, "an earlier hook still applies")

	epoch, err := engine.ResolveFunctionCall(t.Context(), nil, "unix_timestamp", "", []sqlmodel.SQLType{unknown})
	require.NoError(t, err)
	require.Equal(t, "varchar", epoch.ReturnType.EngineName, "the latest hook is consulted first")

	subtracted, err := engine.ResolveFunctionCall(t.Context(), nil, "date_sub", "", []sqlmodel.SQLType{unknown, unknown})
	require.NoError(t, err)
	require.Equal(t, "datetime", subtracted.ReturnType.EngineName, "declining hooks fall through to the built-in rules")

	unregistered, err := engine.ResolveFunctionCall(t.Context(), nil, "nvl", "", []sqlmodel.SQLType{unknown, unknown})
	require.NoError(t, err)
	require.Nil(t, unregistered, "a hook types only a function the catalogue registers")
}

func TestUngroupedColumnsAreWarnedUnlessAnOptionSaysOtherwise(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		options []Option
		want    dialect.UngroupedColumnRule
	}{
		{name: "the default mode rejects the query", options: nil, want: dialect.UngroupedColumnsWarned},
		{name: "an option sets the rule", options: []Option{WithUngroupedColumns(dialect.UngroupedColumnsNullable)}, want: dialect.UngroupedColumnsNullable},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, New(testCase.options...).Info().UngroupedColumns)
		})
	}
}

func TestDoubleQuotesFollowTheSQLMode(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		engine      *Dialect
		doubleQuote sqllex.DoubleQuoteMeaning
		parameters  []string
		columns     []string
	}{
		{
			name:        "a string in the default mode",
			engine:      New(),
			doubleQuote: sqllex.DoubleQuoteString,
			parameters:  []string{"unknown::"},
			columns:     []string{"id", "joined"},
		},
		{
			name:        "an identifier under ANSI_QUOTES",
			engine:      New(WithANSIQuotes(true)),
			doubleQuote: sqllex.DoubleQuoteIdentifier,
			parameters:  []string{"comparison:order:"},
			columns:     []string{"id", "joined"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, err := analyseWith(t, testCase.engine, `SELECT id, CONCAT(id, ":", id) AS joined FROM items WHERE "order" = ?`)

			require.NoError(t, err)
			require.Equal(t, testCase.parameters, parameterSummary(analysis))
			require.Equal(t, testCase.columns, outputColumnNames(analysis))
			require.Equal(t, testCase.doubleQuote, testCase.engine.Info().Lexical.Quotes.DoubleQuote)
		})
	}
}

func outputColumnNames(analysis *ast.RawQueryAnalysis) []string {
	names := make([]string, 0, len(analysis.OutputColumns))
	for _, column := range analysis.OutputColumns {
		names = append(names, column.Name)
	}
	return names
}

func catalogueFunctionColumns(t *testing.T, engine *Dialect, catalogue *sqlmodel.Catalogue, name string) []ast.ScopedColumn {
	t.Helper()
	columns, err := engine.TableValuedFunctionColumnsFromCatalogue(t.Context(), catalogue, name)
	require.NoError(t, err)
	return columns
}

func TestEntryPointsReturnAPanicAsAnErrorAndLogItOnce(t *testing.T) {
	t.Parallel()

	panickingTypeHook := WithTypeNormaliserHook(func(string, []int) *sqlmodel.SQLType { panic("type hook failed") })
	panickingCallHook := WithFunctionCallHook(func(string, []sqlmodel.SQLType) *ast.FunctionResolution {
		panic("call hook failed")
	})

	testCases := []struct {
		call      func(ctx context.Context, engine *Dialect) (any, error)
		name      string
		operation string
		wantError string
		options   []Option
	}{
		{
			name:      "a type hook reached from CREATE TABLE",
			options:   []Option{panickingTypeHook},
			call:      applyFirstStatement("CREATE TABLE t (a INT)"),
			operation: "mysql: apply DDL",
			wantError: "mysql: apply DDL: panic: type hook failed",
		},
		{
			name:      "a type hook reached from a view body",
			options:   []Option{panickingTypeHook},
			call:      applyFirstStatement("CREATE VIEW v AS SELECT CAST(? AS SIGNED) AS a"),
			operation: "mysql: apply DDL",
			wantError: "mysql: apply DDL: panic: type hook failed",
		},
		{
			name:      "a type hook reached from a query",
			options:   []Option{panickingTypeHook},
			call:      analyseFirstStatement("SELECT CAST(? AS SIGNED) AS a"),
			operation: "mysql: analyse query",
			wantError: "mysql: analyse query: panic: type hook failed",
		},
		{
			name:    "a function-call hook",
			options: []Option{panickingCallHook},
			call: func(ctx context.Context, engine *Dialect) (any, error) {
				return engine.ResolveFunctionCall(ctx, nil, "COUNT", "", nil)
			},
			operation: "mysql: resolve function call",
			wantError: "mysql: resolve function call: panic: call hook failed",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, logs := newLoggingContext(t)

			result, err := testCase.call(ctx, New(testCase.options...))

			assert.Nil(t, result)
			require.EqualError(t, err, testCase.wantError)
			_, isPanic := errors.AsType[*dialectkit.PanicError](err)
			require.True(t, isPanic, "want a *dialectkit.PanicError, got %T", err)
			require.ErrorIs(t, err, fault.ErrPanic)
			records := decodeLogRecords(t, logs)
			require.Len(t, records, 1)
			assert.Equal(t, "WARN", records[0]["level"])
			assert.Equal(t, testCase.operation, records[0]["operation"])
			assert.Contains(t, records[0]["stack"], "seagull.sh/seagull/internal/dialect/mysql")
		})
	}
}

func TestParseStatementsReturnsAClassifierPanicAsAnError(t *testing.T) {
	original := firstWordClassifiers[keywordCREATE]
	firstWordClassifiers[keywordCREATE] = func([]token) statementKind { panic("classifier bug") }
	t.Cleanup(func() { firstWordClassifiers[keywordCREATE] = original })

	testCases := []struct {
		name      string
		sql       string
		wantError string
		wantLogs  int
	}{
		{name: "a statement whose classifier panics", sql: "CREATE TABLE t (a INT)",
			wantError: "mysql: parse statements: panic: classifier bug", wantLogs: 1},
		{name: "a statement classified without it", sql: "SELECT 1", wantError: "", wantLogs: 0},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, logs := newLoggingContext(t)

			statements, err := New().ParseStatements(ctx, testCase.sql)

			assert.Len(t, decodeLogRecords(t, logs), testCase.wantLogs)
			if testCase.wantError == "" {
				require.NoError(t, err)
				require.Len(t, statements, 1)
				return
			}
			assert.Nil(t, statements)
			require.EqualError(t, err, testCase.wantError)
			require.ErrorIs(t, err, fault.ErrPanic)
		})
	}
}

func TestEntryPointsStopWithTheContextCauseWhenCancelledMidWalk(t *testing.T) {
	t.Parallel()

	columns := strings.Repeat(", a", columnsPastAnInterruptCheck)
	definitions := strings.Repeat(", b INT", columnsPastAnInterruptCheck)

	testCases := []struct {
		call func(ctx context.Context, engine *Dialect) (any, error)
		name string
	}{
		{name: "a query", call: analyseFirstStatement("SELECT CAST(? AS SIGNED) AS c" + columns + " FROM t")},
		{name: "a table definition", call: applyFirstStatement("CREATE TABLE t (a INT" + definitions + ")")},
		{name: "a view body", call: applyFirstStatement("CREATE VIEW v AS SELECT CAST(? AS SIGNED) AS c" + columns + " FROM t")},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			loggingContext, logs := newLoggingContext(t)
			ctx, cancel := context.WithCancelCause(loggingContext)
			t.Cleanup(func() { cancel(nil) })
			engine := New(WithTypeNormaliserHook(func(string, []int) *sqlmodel.SQLType {
				cancel(errWalkCancelled)
				return nil
			}))

			result, err := testCase.call(ctx, engine)

			assert.Nil(t, result)
			require.ErrorIs(t, err, errWalkCancelled)
			_, isPanic := errors.AsType[*dialectkit.PanicError](err)
			assert.False(t, isPanic, "a cancelled walk is not a panic")
			assert.Empty(t, decodeLogRecords(t, logs), "a cancelled walk logs nothing")
		})
	}
}

func TestEntryPointsReturnTheContextCauseWhenCancelledBeforeTheyStart(t *testing.T) {
	t.Parallel()

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), "CREATE TABLE t (a INT); SELECT a FROM t")
	require.NoError(t, err)
	require.Len(t, statements, 2)

	testCases := []struct {
		call func(ctx context.Context) error
		name string
	}{
		{name: "ParseStatements", call: func(ctx context.Context) error {
			_, callError := engine.ParseStatements(ctx, "SELECT 1")
			return callError
		}},
		{name: "ApplyDDL", call: func(ctx context.Context) error {
			_, callError := engine.ApplyDDL(ctx, statements[0])
			return callError
		}},
		{name: "AnalyseQuery", call: func(ctx context.Context) error {
			_, callError := engine.AnalyseQuery(ctx, nil, statements[1])
			return callError
		}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancelCause(t.Context())
			cancel(errWalkCancelled)

			require.ErrorIs(t, testCase.call(ctx), errWalkCancelled)
		})
	}
}

func TestStatementParserChildrenShareTheStatementInterrupt(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		child func(p *parser) *parser
		name  string
	}{
		{name: "a child parser", child: func(p *parser) *parser { return p.newChildParser(p.Tokens) }},
		{name: "a nested query parser", child: func(p *parser) *parser { return p.newNestedQueryParser(p.Tokens) }},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statements, err := engine.ParseStatements(t.Context(), "SELECT a FROM t")
			require.NoError(t, err)
			parsed, ok := statements[0].Raw.(*parsedStatement)
			require.True(t, ok)
			reads := 0
			p := engine.newStatementParser(func() error {
				reads++
				return nil
			}, parsed)

			p.Current()
			testCase.child(p).Current()
			testCase.child(testCase.child(p)).Current()

			assert.Equal(t, 3, reads, "every parser of the statement counts against the one interrupt")
		})
	}
}

func applyFirstStatement(sql string) func(ctx context.Context, engine *Dialect) (any, error) {
	return func(ctx context.Context, engine *Dialect) (any, error) {
		statements, err := engine.ParseStatements(ctx, sql)
		if err != nil {
			return nil, err
		}
		return engine.ApplyDDL(ctx, statements[0])
	}
}

func analyseFirstStatement(sql string) func(ctx context.Context, engine *Dialect) (any, error) {
	return func(ctx context.Context, engine *Dialect) (any, error) {
		statements, err := engine.ParseStatements(ctx, sql)
		if err != nil {
			return nil, err
		}
		return engine.AnalyseQuery(ctx, nil, statements[0])
	}
}

func newLoggingContext(t *testing.T) (context.Context, *bytes.Buffer) {
	t.Helper()

	logs := &bytes.Buffer{}
	return logging.ContextWithLogger(t.Context(), slog.New(slog.NewJSONHandler(logs, nil))), logs
}

func decodeLogRecords(t *testing.T, logs *bytes.Buffer) []map[string]any {
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

func TestCheckRunOnFindsARunOnAnywhereInAStatement(t *testing.T) {
	t.Parallel()

	seed := "INSERT INTO a VALUES " + strings.TrimSuffix(strings.Repeat("(1, 'x'), ", 2000), ", ")
	testCases := []struct {
		name         string
		sql          string
		maxTokens    int
		wantError    string
		wantPosition int
	}{
		{name: "a run-on within the budget is found", sql: "INSERT INTO a VALUES (1)\nDROP TABLE b", maxTokens: 100,
			wantError: "a semicolon is missing before DROP", wantPosition: 25},
		{name: "a run-on among the kept tokens of a statement past the budget is found",
			sql: "INSERT INTO a VALUES (1)\nDROP TABLE b, c, d, e", maxTokens: 10,
			wantError: "a semicolon is missing before DROP", wantPosition: 25},
		{name: "a run-on past the budget of a statement passed over is found",
			sql: "INSERT INTO a VALUES (1, 2, 3, 4)\nDROP TABLE b", maxTokens: 8,
			wantError: "a semicolon is missing before DROP", wantPosition: 34},
		{name: "a run-on after a seed many times the budget is found", sql: seed + "\nDROP TABLE b", maxTokens: 100,
			wantError: "a semicolon is missing before DROP", wantPosition: len(seed) + 1},
		{name: "a statement past the budget that does not run on passes", sql: seed, maxTokens: 100},
		{name: "a character the tokeniser refuses past the budget is reported",
			sql: "INSERT INTO a VALUES (1, 2, 3, 4) \u20ac", maxTokens: 8, wantError: "unexpected character"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(WithMaxTokensPerStatement(testCase.maxTokens))
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)

			mutation, err := engine.ApplyDDL(t.Context(), statements[0])

			assert.Nil(t, mutation)
			if testCase.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, testCase.wantError)
			if testCase.wantPosition > 0 {
				parseError, isParseError := errors.AsType[*dialectkit.ParseError](err)
				require.True(t, isParseError, "got %v", err)
				assert.Equal(t, testCase.wantPosition, parseError.Position)
			}
		})
	}
}

func analyseWith(t *testing.T, engine *Dialect, sql string) (*ast.RawQueryAnalysis, error) {
	t.Helper()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.Len(t, statements, 1)
	return engine.AnalyseQuery(t.Context(), nil, statements[0])
}

func requireParseErrorAt(t *testing.T, err error, token string) {
	t.Helper()
	parseError, ok := errors.AsType[*dialectkit.ParseError](err)
	require.True(t, ok, "want a syntax error at %q, got %v", token, err)
	require.Equal(t, token, parseError.Token)
}

func parameterSummary(analysis *ast.RawQueryAnalysis) []string {
	summary := make([]string, 0, len(analysis.ParameterReferences))
	for _, reference := range analysis.ParameterReferences {
		column := ""
		if reference.ColumnReference != nil {
			column = reference.ColumnReference.ColumnName
		}
		castType := ""
		if reference.CastType != nil {
			castType = reference.CastType.EngineName
		}
		summary = append(summary, reference.Context.String()+":"+column+":"+castType)
	}
	return summary
}

func TestAnalyseQueryRejectsTokensAfterTheStatement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		sql   string
		token string
	}{
		{name: "unbalanced parentheses after a select", sql: "SELECT id FROM items WHERE id = ? ) )", token: ")"},
		{name: "word after a limit", sql: "SELECT id FROM items LIMIT 1 garbage", token: "garbage"},
		{name: "unparsed tail of an update", sql: "UPDATE items SET name = ? WHERE id = ? )", token: ")"},
		{name: "trailing tokens inside a derived table", sql: "SELECT x.id FROM (SELECT id FROM items oops more) AS x", token: "more"},
		{name: "trailing tokens inside a predicate subquery", sql: "SELECT id FROM items WHERE id IN (SELECT id FROM items a b)", token: "b"},
		{name: "trailing tokens inside a CTE", sql: "WITH c AS (SELECT id FROM items a b) SELECT id FROM c", token: "b"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, err := analyseWith(t, New(), testCase.sql)

			require.Nil(t, analysis)
			requireParseErrorAt(t, err, testCase.token)
		})
	}
}

func TestAnalyseQueryRefusesSelectIntoWithAClearMessage(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
	}{
		{name: "a parameter into a variable", sql: "SELECT ? INTO @x"},
		{name: "a column into a variable before FROM", sql: "SELECT name INTO @y FROM items WHERE id = ?"},
		{name: "a column into a variable after WHERE", sql: "SELECT name FROM items WHERE id = ? INTO @z"},
		{name: "rows into a file", sql: "SELECT id FROM items INTO OUTFILE '/tmp/items.txt'"},
		{name: "a locked row into a variable", sql: "SELECT name FROM items WHERE id = 1 FOR UPDATE INTO @x"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, err := analyseWith(t, New(), testCase.sql)

			require.Nil(t, analysis)
			requireParseErrorAt(t, err, "INTO")
			require.ErrorContains(t, err, "SELECT ... INTO is not supported")
		})
	}
}

func TestAnalyseQueryKeepsEveryPlaceholder(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		sql        string
		parameters []string
	}{
		{
			name:       "leading-dot number in the select list",
			sql:        "SELECT .5 AS half, price * .5 AS discounted FROM items WHERE id = ? AND name = ?",
			parameters: []string{"comparison:id:", "comparison:name:"},
		},
		{
			name:       "delimited reserved words in the predicate",
			sql:        "SELECT id, `group` FROM reserved_items WHERE `where` = ? AND `order` = ?",
			parameters: []string{"comparison:where:", "comparison:order:"},
		},
		{
			name:       "index hint after the table",
			sql:        "SELECT id FROM items USE INDEX (by_name) WHERE name = ?",
			parameters: []string{"comparison:name:"},
		},
		{
			name:       "named window after the having clause",
			sql:        "SELECT id, SUM(qty) OVER w AS total FROM items WHERE id > ? WINDOW w AS (ORDER BY id)",
			parameters: []string{"comparison:id:"},
		},
		{
			name:       "delete with a using clause",
			sql:        "DELETE FROM items USING items JOIN orders ON orders.item_id = items.id WHERE orders.id = ?",
			parameters: []string{"comparison:id:"},
		},
		{
			name:       "insert with modifiers and value rows",
			sql:        "INSERT LOW_PRIORITY IGNORE INTO items (id, name) VALUES ROW(?, ?)",
			parameters: []string{"assignment:id:", "assignment:name:"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, err := analyseWith(t, New(), testCase.sql)

			require.NoError(t, err)
			require.Equal(t, testCase.parameters, parameterSummary(analysis))
		})
	}
}

func TestAnalyseQueryBindsComparedParameters(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		sql        string
		parameters []string
	}{
		{
			name:       "not in list binds the column",
			sql:        "SELECT id FROM items WHERE id NOT IN (?, ?)",
			parameters: []string{"in-list:id:", "in-list:id:"},
		},
		{
			name:       "comparison with a function call takes its result type",
			sql:        "SELECT id FROM orders WHERE JSON_UNQUOTE(basket) = ?",
			parameters: []string{"comparison::text"},
		},
		{
			name:       "comparison with a nested call binds no inner name",
			sql:        "SELECT id FROM orders WHERE JSON_UNQUOTE(JSON_EXTRACT(basket, '$.fish')) = ?",
			parameters: []string{"comparison::text"},
		},
		{
			name:       "comparison with a parenthesised column keeps the column",
			sql:        "SELECT id FROM items WHERE (price) = ?",
			parameters: []string{"comparison:price:"},
		},
		{
			name:       "integer division binds the column",
			sql:        "SELECT id FROM items WHERE qty DIV ? = 2",
			parameters: []string{"comparison:qty:"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, err := analyseWith(t, New(), testCase.sql)

			require.NoError(t, err)
			require.Equal(t, testCase.parameters, parameterSummary(analysis))
		})
	}
}

func TestAnalyseQueryReportsAMalformedSubqueryInsteadOfDroppingItsParameters(t *testing.T) {
	t.Parallel()

	analysis, err := analyseWith(t, New(), "SELECT id FROM items WHERE EXISTS (SELECT id FROM items a b WHERE x = ?) AND id = ?")

	require.Nil(t, analysis)
	requireParseErrorAt(t, err, "b")
}

func TestAnalyseQueryTypesLiteralsFromTheirShape(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, "SELECT .5 AS half, 1.10 AS exact, 1e3 AS sci, 2.5E-2 AS small, 0x1E AS hex, "+
		"0b101 AS bits, 7 AS small_int, 3000000000 AS big_int, 99999999999999999999 AS huge FROM items")

	types := make(map[string]string, len(analysis.OutputColumns))
	for _, column := range analysis.OutputColumns {
		literal, ok := column.Expression.(*ast.LiteralExpression)
		require.True(t, ok, "column %s", column.Name)
		types[column.Name] = literal.TypeName
	}
	require.Equal(t, map[string]string{
		"half": "decimal", "exact": "decimal", "sci": "double", "small": "double", "hex": "varbinary",
		"bits": "varbinary", "small_int": "int", "big_int": "bigint", "huge": "decimal",
	}, types)
}

func TestAnalyseQueryParsesKeywordOperators(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		operator string
	}{
		{name: "integer division", sql: "SELECT qty DIV 2 AS result FROM items", operator: "DIV"},
		{name: "modulo keyword", sql: "SELECT qty mod 2 AS result FROM items", operator: "MOD"},
		{name: "logical exclusive or", sql: "SELECT a XOR b AS result FROM items", operator: "XOR"},
		{name: "double ampersand", sql: "SELECT a && b AS result FROM items", operator: "AND"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, testCase.sql)

			require.Len(t, analysis.OutputColumns, 1)
			switch expression := analysis.OutputColumns[0].Expression.(type) {
			case *ast.BinaryOpExpression:
				require.Equal(t, testCase.operator, expression.Operator)
			case *ast.LogicalOpExpression:
				require.Equal(t, testCase.operator, expression.Operator)
			default:
				require.Failf(t, "unexpected expression", "got %T", expression)
			}
		})
	}
}

func TestAnalyseQueryParsesParenthesisedFromItems(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		sql    string
		tables []string
		joins  []string
	}{
		{
			name:   "parenthesised join",
			sql:    "SELECT t.id FROM (t JOIN u ON t.id = u.id) WHERE t.id = ?",
			tables: []string{"t"},
			joins:  []string{"INNER:u"},
		},
		{
			name:   "parenthesised table",
			sql:    "SELECT id FROM (t)",
			tables: []string{"t"},
			joins:  []string{},
		},
		{
			name:   "left join to a parenthesised join",
			sql:    "SELECT a.id FROM a LEFT JOIN (b JOIN c ON b.id = c.id) ON a.id = b.id",
			tables: []string{"a"},
			joins:  []string{"LEFT:b", "LEFT:c"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, testCase.sql)

			tables := make([]string, 0, len(analysis.FromTables))
			for _, table := range analysis.FromTables {
				tables = append(tables, table.Name)
			}
			joins := make([]string, 0, len(analysis.JoinClauses))
			for _, join := range analysis.JoinClauses {
				joins = append(joins, join.Kind.String()+":"+join.Table.Name)
			}
			require.Equal(t, testCase.tables, tables)
			require.Equal(t, testCase.joins, joins)
		})
	}
}

func TestAnalyseQueryParsesMySQLExpressionForms(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
	}{
		{name: "json value with returning and handlers", sql: "SELECT JSON_VALUE(doc, '$.n' RETURNING DECIMAL(10,2) NULL ON EMPTY ERROR ON ERROR) AS n FROM t"},
		{name: "member of", sql: "SELECT 1 MEMBER OF (doc->'$.ids') AS present FROM t"},
		{name: "match against", sql: "SELECT MATCH (title, body) AGAINST ('fish' IN NATURAL LANGUAGE MODE) AS score FROM t"},
		{name: "character set introducer", sql: "SELECT _utf8mb4'gull' COLLATE utf8mb4_bin AS name, N'tern' AS other FROM t"},
		{name: "typed temporal literal", sql: "SELECT DATE '2026-01-01' AS day FROM t"},
		{name: "cast with a character set", sql: "SELECT CAST(name AS CHAR(10) CHARACTER SET utf8mb4) AS short FROM t"},
		{name: "convert using", sql: "SELECT CONVERT(name USING utf8mb4) AS converted FROM t"},
		{name: "json table columns", sql: "SELECT jt.id FROM t, JSON_TABLE(t.doc, '$[*]' COLUMNS (rn FOR ORDINALITY, " +
			"id INT PATH '$.id' DEFAULT '0' ON EMPTY, NESTED PATH '$.tags[*]' COLUMNS (tag VARCHAR(20) PATH '$'))) AS jt"},
		{name: "values statement with rows", sql: "VALUES ROW(1, 'a'), ROW(2, 'b')"},
		{name: "locking with a wait", sql: "SELECT id FROM t FOR UPDATE WAIT 5"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := analyseWith(t, New(), testCase.sql)

			require.NoError(t, err)
		})
	}
}

func TestAnalyseQueryReadsTheJSONTableColumns(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, "SELECT jt.id FROM t, JSON_TABLE(t.doc, '$[*]' COLUMNS (rn FOR ORDINALITY, "+
		"id INT PATH '$.id', NESTED PATH '$.tags[*]' COLUMNS (tag VARCHAR(20) PATH '$'))) AS jt")

	require.Len(t, analysis.RawTableValuedFunctions, 1)
	require.Equal(t, "jt", analysis.RawTableValuedFunctions[0].Alias)
	require.Equal(t, []ast.TVFColumnDefinition{
		{Name: "rn", TypeName: "int unsigned"},
		{Name: "id", TypeName: "INT"},
		{Name: "tag", TypeName: "VARCHAR(20)"},
	}, analysis.RawTableValuedFunctions[0].ColumnDefinitions)
}

func TestTableFunctionColumnTypesKeepTheirModifiers(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		sql           string
		wantCategory  sqlmodel.SQLTypeCategory
		wantPrecision *int
		wantScale     *int
		wantLength    *int
	}{
		{
			name:          "a JSON_TABLE column with a precision and a scale",
			sql:           "SELECT jt.price FROM t, JSON_TABLE(t.doc, '$[*]' COLUMNS (price DECIMAL(10, 2) PATH '$.price')) AS jt",
			wantCategory:  sqlmodel.TypeCategoryDecimal,
			wantPrecision: new(10),
			wantScale:     new(2),
		},
		{
			name:         "a JSON_TABLE column with a length",
			sql:          "SELECT jt.name FROM t, JSON_TABLE(t.doc, '$[*]' COLUMNS (name VARCHAR(40) PATH '$.name')) AS jt",
			wantCategory: sqlmodel.TypeCategoryText,
			wantLength:   new(40),
		},
		{
			name:         "a JSON_TABLE column with no modifiers",
			sql:          "SELECT jt.n FROM t, JSON_TABLE(t.doc, '$[*]' COLUMNS (n INT PATH '$.n')) AS jt",
			wantCategory: sqlmodel.TypeCategoryInteger,
		},
		{
			name:          "a column list entry with a fractional-second precision",
			sql:           "SELECT x.at FROM f(?) AS x (at DATETIME(3))",
			wantCategory:  sqlmodel.TypeCategoryTemporal,
			wantPrecision: new(3),
		},
		{
			name:          "a column list entry with a precision and a scale",
			sql:           "SELECT x.amount FROM f(?) AS x (amount NUMERIC(12, 4))",
			wantCategory:  sqlmodel.TypeCategoryDecimal,
			wantPrecision: new(12),
			wantScale:     new(4),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			analysis := analyseQuery(t, testCase.sql)
			require.Len(t, analysis.RawTableValuedFunctions, 1)
			require.Len(t, analysis.RawTableValuedFunctions[0].ColumnDefinitions, 1)

			sqlType := engine.NormaliseTypeName(analysis.RawTableValuedFunctions[0].ColumnDefinitions[0].TypeName)

			assert.Equal(t, testCase.wantCategory, sqlType.Category)
			assert.Equal(t, testCase.wantPrecision, sqlType.Precision)
			assert.Equal(t, testCase.wantScale, sqlType.Scale)
			assert.Equal(t, testCase.wantLength, sqlType.Length)
		})
	}
}

func TestNormaliseTypeNameReadsTheModifiersWrittenInTheName(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		input          string
		modifiers      []int
		wantCategory   sqlmodel.SQLTypeCategory
		wantEngineName string
		wantPrecision  *int
		wantScale      *int
		wantLength     *int
	}{
		{name: "a precision and a scale", input: "decimal(10,2)", wantCategory: sqlmodel.TypeCategoryDecimal,
			wantEngineName: "decimal", wantPrecision: new(10), wantScale: new(2)},
		{name: "upper case with spaces", input: " DECIMAL( 12 , 4 ) ", wantCategory: sqlmodel.TypeCategoryDecimal,
			wantEngineName: "decimal", wantPrecision: new(12), wantScale: new(4)},
		{name: "a length", input: "varchar(255)", wantCategory: sqlmodel.TypeCategoryText,
			wantEngineName: "varchar", wantLength: new(255)},
		{name: "a fractional-second precision", input: "datetime(6)", wantCategory: sqlmodel.TypeCategoryTemporal,
			wantEngineName: "datetime", wantPrecision: new(6)},
		{name: "a precision past the largest modifier is held at it", input: "decimal(99999999999)",
			wantCategory: sqlmodel.TypeCategoryDecimal, wantEngineName: "decimal", wantPrecision: new(dialectkit.MaxTypeModifier)},
		{name: "a two-word type", input: "int unsigned(11)", wantCategory: sqlmodel.TypeCategoryInteger,
			wantEngineName: "int unsigned"},
		{name: "more modifiers than any type takes", input: "decimal(1,2,3)", wantCategory: sqlmodel.TypeCategoryUnknown,
			wantEngineName: "decimal(1,2,3)"},
		{name: "a modifier that is not a number", input: "decimal(x)", wantCategory: sqlmodel.TypeCategoryUnknown,
			wantEngineName: "decimal(x)"},
		{name: "a signed modifier", input: "decimal(-1)", wantCategory: sqlmodel.TypeCategoryUnknown,
			wantEngineName: "decimal(-1)"},
		{name: "an empty modifier", input: "decimal(10,)", wantCategory: sqlmodel.TypeCategoryUnknown,
			wantEngineName: "decimal(10,)"},
		{name: "a list with no type before it", input: "(10)", wantCategory: sqlmodel.TypeCategoryUnknown,
			wantEngineName: "(10)"},
		{name: "a list left open", input: "decimal(10", wantCategory: sqlmodel.TypeCategoryUnknown,
			wantEngineName: "decimal(10"},
		{name: "modifiers passed alongside the name are the only ones read", input: "decimal(10,2)", modifiers: []int{8},
			wantCategory: sqlmodel.TypeCategoryUnknown, wantEngineName: "decimal(10,2)"},
		{name: "modifiers passed with a bare name", input: "decimal", modifiers: []int{8, 3},
			wantCategory: sqlmodel.TypeCategoryDecimal, wantEngineName: "decimal", wantPrecision: new(8), wantScale: new(3)},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			sqlType := New().NormaliseTypeName(testCase.input, testCase.modifiers...)

			assert.Equal(t, testCase.wantCategory, sqlType.Category)
			assert.Equal(t, testCase.wantEngineName, sqlType.EngineName)
			assert.Equal(t, testCase.wantPrecision, sqlType.Precision)
			assert.Equal(t, testCase.wantScale, sqlType.Scale)
			assert.Equal(t, testCase.wantLength, sqlType.Length)
		})
	}
}

func TestInsertRowAliasFollowsTheDialectCapability(t *testing.T) {
	t.Parallel()

	const sql = "INSERT INTO stock (item, quantity) VALUES (?, ?) AS new (i, q) " +
		"ON DUPLICATE KEY UPDATE quantity = stock.quantity + new.q"

	analysis, err := analyseWith(t, New(), sql)
	require.NoError(t, err)
	require.Equal(t, []string{"assignment:item:", "assignment:quantity:"}, parameterSummary(analysis))

	analysis, err = analyseWith(t, New(WithInsertRowAlias(false)), sql)
	require.Nil(t, analysis)
	requireParseErrorAt(t, err, "AS")
}

func TestJSONArrowsFollowTheDialectCapability(t *testing.T) {
	t.Parallel()

	const sql = "SELECT doc ->> '$.name' AS name FROM t WHERE doc -> '$.id' = ?"

	_, err := analyseWith(t, New(), sql)
	require.NoError(t, err)

	analysis, err := analyseWith(t, New(WithJSONArrows(false)), sql)
	require.Nil(t, analysis)
	requireParseErrorAt(t, err, "->>")
}

func TestStatementsMustCloseEveryParenthesisTheyOpen(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sql       string
		wantToken string
	}{
		{name: "a select item left open", sql: "SELECT (((1", wantToken: "("},
		{name: "a call left open in a select list", sql: "SELECT count(*", wantToken: "("},
		{name: "a table function's column list left open", sql: "SELECT * FROM f(?) AS x (a INT", wantToken: "("},
		{name: "a view whose query is left open", sql: "CREATE VIEW v AS SELECT (((1", wantToken: "("},
		{name: "a table created from a query left open", sql: "CREATE TABLE t AS SELECT (1", wantToken: "("},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)

			mutation, applyError := engine.ApplyDDL(t.Context(), statements[0])
			analysis, analyseError := engine.AnalyseQuery(t.Context(), nil, statements[0])

			if strings.HasPrefix(testCase.sql, "CREATE") {
				assert.Nil(t, mutation)
				requireParseErrorAt(t, applyError, testCase.wantToken)
				require.ErrorContains(t, applyError, unmatchedParenthesis)
				return
			}
			assert.Nil(t, analysis)
			requireParseErrorAt(t, analyseError, testCase.wantToken)
			require.ErrorContains(t, analyseError, unmatchedParenthesis)
		})
	}
}

func TestBuiltinIfLeavesItsTypeToTheResolver(t *testing.T) {
	t.Parallel()

	overloads := New().BuiltinFunctions().Functions["if"]
	require.Len(t, overloads, 1)
	assert.Equal(t, sqlmodel.TypeCategoryUnknown, overloads[0].ReturnType.Category,
		"IF takes its branches' type, so the catalogue must not fix it as text")
}
