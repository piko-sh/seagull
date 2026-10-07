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

package clickhouse

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestBuiltinFunctions_StatisticalAggregates(t *testing.T) {
	t.Parallel()

	engine := New()
	functions := engine.BuiltinFunctions()
	require.NotNil(t, functions)

	cases := []string{
		"kolmogorovsmirnovtest",
		"mannwhitneyutest",
		"welchttest",
		"studentttest",
		"studentttestonesample",
		"meanztest",
		"proportionsztest",
		"cramersv",
		"cramersvbiascorrected",
		"theilsu",
		"contingency",
		"categoricalinformationvalue",
		"rankcorr",
		"analysisofvariance",
	}
	for index := range cases {
		key := cases[index]
		t.Run(key, func(testRunner *testing.T) {
			testRunner.Parallel()
			signatures, found := functions.Functions[key]
			require.True(testRunner, found, "function %q is missing from the catalogue", key)
			require.NotEmpty(testRunner, signatures, "function %q has no signatures", key)
			assert.True(testRunner, signatures[0].IsAggregate, "function %q should be tagged as an aggregate", key)
		})
	}
}

func TestBuiltinFunctions_ExtendedAggregates(t *testing.T) {
	t.Parallel()

	engine := New()
	functions := engine.BuiltinFunctions()
	require.NotNil(t, functions)

	cases := []string{
		"argandmin", "argandmax",
		"corrmatrix", "corrstable",
		"covarpopmatrix", "covarpopstable",
		"covarsampmatrix", "covarsampstable",
		"varpopstable", "varsampstable",
		"stddevpopstable", "stddevsampstable",
		"stochasticlogisticregression",
		"distinctdynamictypes", "distinctjsonpaths", "distinctjsonpathsandtypes",
		"estimatecompressionratio", "flamegraph",
		"grouparrayarray", "grouparrayintersect",
		"maxintersections", "maxintersectionsposition",
		"sparkbar",
		"quantiledd", "quantileexactexclusive", "quantileexactinclusive",
		"quantileexacthigh", "quantileexactlow",
		"quantileexactweightedinterpolated", "quantileprometheushistogram",
		"quantilesexactexclusive", "quantilesexactinclusive",
		"quantilesgk", "quantilestimingweighted", "quantiletdigestweighted",
		"summapwithoverflow", "summapfiltered", "summapfilteredwithoverflow",
		"timeserieschangestogrid", "timeseriesdeltatogrid", "timeseriesderivtogrid",
		"timeseriesgrouparray", "timeseriesinstantdeltatogrid",
		"timeseriesinstantratetogrid", "timeserieslasttwosamples",
		"timeseriespredictlineartogrid", "timeseriesratetogrid",
		"timeseriesresampletogridwithstaleness", "timeseriesresetstogrid",
		"sequencematchevents", "sequencenextnode", "uniqupto",
	}
	for index := range cases {
		key := cases[index]
		t.Run(key, func(testRunner *testing.T) {
			testRunner.Parallel()
			signatures, found := functions.Functions[key]
			require.True(testRunner, found, "function %q is missing from the catalogue", key)
			require.NotEmpty(testRunner, signatures, "function %q has no signatures", key)
			assert.True(testRunner, signatures[0].IsAggregate, "function %q should be tagged as an aggregate", key)
		})
	}
}

func TestBuiltinFunctions_BitmapFunctions(t *testing.T) {
	t.Parallel()

	engine := New()
	functions := engine.BuiltinFunctions()
	require.NotNil(t, functions)

	cases := []struct {
		key       string
		aggregate bool
	}{
		{key: "bitmapbuild"},
		{key: "bitmaptoarray"},
		{key: "bitmapsubsetinrange"},
		{key: "bitmapsubsetlimit"},
		{key: "subbitmap"},
		{key: "bitmapcontains"},
		{key: "bitmaphasall"},
		{key: "bitmaphasany"},
		{key: "bitmapmin"},
		{key: "bitmapmax"},
		{key: "bitmaptransform"},
		{key: "bitmapand"},
		{key: "bitmapor"},
		{key: "bitmapxor"},
		{key: "bitmapandnot"},
		{key: "bitmapandcardinality"},
		{key: "bitmaporcardinality"},
		{key: "bitmapxorcardinality"},
		{key: "bitmapandnotcardinality"},
		{key: "bitmapcardinality"},
		{key: "groupbitmap", aggregate: true},
		{key: "groupbitmapand", aggregate: true},
		{key: "groupbitmapor", aggregate: true},
		{key: "groupbitmapxor", aggregate: true},
	}
	for index := range cases {
		testCase := cases[index]
		t.Run(testCase.key, func(testRunner *testing.T) {
			testRunner.Parallel()
			signatures, found := functions.Functions[testCase.key]
			require.True(testRunner, found, "function %q is missing from the catalogue", testCase.key)
			require.NotEmpty(testRunner, signatures, "function %q has no signatures", testCase.key)
			if testCase.aggregate {
				assert.True(testRunner, signatures[0].IsAggregate, "function %q should be tagged as an aggregate", testCase.key)
			}
		})
	}
}

func TestBuiltinFunctions_DistanceAndNormFunctions(t *testing.T) {
	t.Parallel()

	engine := New()
	functions := engine.BuiltinFunctions()
	require.NotNil(t, functions)

	cases := []string{
		"lpdistance", "lpnorm",
		"l2squareddistance", "l2squarednorm",
		"l1normalize", "l2normalize", "linfnormalize", "lpnormalize",
		"l2distancetransposed", "cosinedistancetransposed",
		"tuplehammingdistance", "bytehammingdistance",
		"arrayhammingdistance", "bithammingdistance",
	}
	for index := range cases {
		key := cases[index]
		t.Run(key, func(testRunner *testing.T) {
			testRunner.Parallel()
			signatures, found := functions.Functions[key]
			require.True(testRunner, found, "function %q is missing from the catalogue", key)
			require.NotEmpty(testRunner, signatures, "function %q has no signatures", key)
		})
	}
}

func TestBuiltinFunctions_NLPFunctions(t *testing.T) {
	t.Parallel()

	engine := New()
	functions := engine.BuiltinFunctions()
	require.NotNil(t, functions)

	cases := []string{
		"detectcharset", "detectlanguage", "detectlanguagemixed",
		"detectlanguageunknown", "stem", "lemmatize", "synonyms",
		"detecttonality",
	}
	for index := range cases {
		key := cases[index]
		t.Run(key, func(testRunner *testing.T) {
			testRunner.Parallel()
			signatures, found := functions.Functions[key]
			require.True(testRunner, found, "function %q is missing from the catalogue", key)
			require.NotEmpty(testRunner, signatures, "function %q has no signatures", key)
		})
	}
}

func TestBuiltinFunctions_GeoS2Functions(t *testing.T) {
	t.Parallel()

	engine := New()
	functions := engine.BuiltinFunctions()
	require.NotNil(t, functions)

	cases := []string{
		"geotos2", "s2togeo", "s2getneighbors", "s2cellsintersect",
		"s2capcontains", "s2capunion",
		"s2rectadd", "s2rectcontains", "s2rectunion", "s2rectintersection",
	}
	for index := range cases {
		key := cases[index]
		t.Run(key, func(testRunner *testing.T) {
			testRunner.Parallel()
			signatures, found := functions.Functions[key]
			require.True(testRunner, found, "function %q is missing from the catalogue", key)
			require.NotEmpty(testRunner, signatures, "function %q has no signatures", key)
		})
	}
}

func TestBuiltinFunctions_GeoPolygonFunctions(t *testing.T) {
	t.Parallel()

	engine := New()
	functions := engine.BuiltinFunctions()
	require.NotNil(t, functions)

	cases := []string{
		"readwktpoint", "readwktlinestring", "readwktpolygon",
		"readwktmultipolygon", "readwktring",
		"readwkbpoint", "readwkblinestring", "readwkbpolygon",
		"readwkbmultipolygon", "readwkbmultilinestring",
		"wkt",
		"polygonsintersectioncartesian", "polygonsintersectionspherical",
		"polygonsunioncartesian", "polygonsunionspherical",
		"polygonssymdifferencecartesian", "polygonssymdifferencespherical",
		"polygonsintersectcartesian", "polygonsintersectspherical",
		"polygonswithincartesian", "polygonswithinspherical",
		"polygonsequalscartesian", "polygonconvexhullcartesian",
		"polygonareacartesian", "polygonareaspherical",
		"polygonperimetercartesian", "polygonperimeterspherical",
		"polygonsdistancecartesian", "polygonsdistancespherical",
	}
	for index := range cases {
		key := cases[index]
		t.Run(key, func(testRunner *testing.T) {
			testRunner.Parallel()
			signatures, found := functions.Functions[key]
			require.True(testRunner, found, "function %q is missing from the catalogue", key)
			require.NotEmpty(testRunner, signatures, "function %q has no signatures", key)
		})
	}
}

func TestBuiltinFunctions_H3ExtendedFunctions(t *testing.T) {
	t.Parallel()

	engine := New()
	functions := engine.BuiltinFunctions()
	require.NotNil(t, functions)

	cases := []string{
		"h3edgeangle", "h3edgelengthm", "h3edgelengthkm",
		"h3exactedgelengthm", "h3exactedgelengthkm", "h3exactedgelengthrads",
		"h3hexaream2", "h3hexareakm2", "h3cellaream2", "h3cellarearads2",
		"h3numhexagons", "h3getbasecell", "h3getfaces",
		"h3tochildren", "h3tocenterchild", "h3tostring", "stringtoh3",
		"h3getres0indexes", "h3getpentagonindexes", "h3togeoboundary",
		"h3isresclassiii", "h3ispentagon", "h3indexesareneighbors",
		"h3line", "h3distance", "h3hexring",
		"h3pointdistm", "h3pointdistkm", "h3pointdistrads",
		"h3polygontocells", "h3polygontocellswithcontainment",
		"h3getunidirectionaledge", "h3unidirectionaledgeisvalid",
		"h3getoriginindexfromunidirectionaledge",
		"h3getdestinationindexfromunidirectionaledge",
		"h3getindexesfromunidirectionaledge",
		"h3getunidirectionaledgesfromhexagon",
		"h3getunidirectionaledgeboundary",
	}
	for index := range cases {
		key := cases[index]
		t.Run(key, func(testRunner *testing.T) {
			testRunner.Parallel()
			signatures, found := functions.Functions[key]
			require.True(testRunner, found, "function %q is missing from the catalogue", key)
			require.NotEmpty(testRunner, signatures, "function %q has no signatures", key)
		})
	}
}

func TestBuiltinFunctions_ExtendedMathRoundingBit(t *testing.T) {
	t.Parallel()

	engine := New()
	functions := engine.BuiltinFunctions()
	require.NotNil(t, functions)

	cases := []string{
		"sinh", "cosh", "tanh", "asinh", "acosh", "atanh",
		"erf", "erfc", "lgamma", "tgamma",
		"exp2", "exp10", "intexp2", "intexp10", "log1p",
		"hypot", "degrees", "radians",
		"isprime", "isprobableprime",
		"ceiling", "trunc",
		"roundage", "roundduration", "rounddown", "roundtoexp2",
		"bitslice", "bitpositionstoarray",
	}
	for index := range cases {
		key := cases[index]
		t.Run(key, func(testRunner *testing.T) {
			testRunner.Parallel()
			signatures, found := functions.Functions[key]
			require.True(testRunner, found, "function %q is missing from the catalogue", key)
			require.NotEmpty(testRunner, signatures, "function %q has no signatures", key)
		})
	}
}

func TestBuiltinFunctions_AggregateCombinatorReturnTypes(t *testing.T) {
	t.Parallel()

	resolver := newFunctionResolver()

	sumArguments := []sqlmodel.SQLType{
		{Category: sqlmodel.TypeCategoryFloat, EngineName: "Float64"},
	}

	cases := []struct {
		assertion func(testRunner *testing.T, returnType sqlmodel.SQLType)
		name      string
		arguments []sqlmodel.SQLType
	}{
		{
			name:      "sumIf",
			arguments: sumArguments,
			assertion: func(testRunner *testing.T, returnType sqlmodel.SQLType) {
				testRunner.Helper()
				assert.Equal(testRunner, "Float64", returnType.EngineName, "If preserves base return type")
				assert.False(testRunner, returnType.Nullable, "If does not introduce nullability")
			},
		},
		{
			name:      "sumOrNull",
			arguments: sumArguments,
			assertion: func(testRunner *testing.T, returnType sqlmodel.SQLType) {
				testRunner.Helper()
				assert.Equal(testRunner, "Float64", returnType.EngineName, "OrNull preserves engine name")
				assert.True(testRunner, returnType.Nullable, "OrNull wraps the result in Nullable")
			},
		},
		{
			name:      "sumOrDefault",
			arguments: sumArguments,
			assertion: func(testRunner *testing.T, returnType sqlmodel.SQLType) {
				testRunner.Helper()
				assert.Equal(testRunner, "Float64", returnType.EngineName, "OrDefault preserves base type")
				assert.False(testRunner, returnType.Nullable, "OrDefault produces a non-null fallback")
			},
		},
		{
			name:      "sumArray",
			arguments: sumArguments,
			assertion: func(testRunner *testing.T, returnType sqlmodel.SQLType) {
				testRunner.Helper()
				assert.Equal(testRunner, "Float64", returnType.EngineName, "Array combinator preserves base engine name")
			},
		},
		{
			name:      "sumState",
			arguments: sumArguments,
			assertion: func(testRunner *testing.T, returnType sqlmodel.SQLType) {
				testRunner.Helper()
				assert.Equal(testRunner, "AggregateFunction", returnType.EngineName, "State wraps the result in AggregateFunction")
			},
		},
		{
			name:      "sumMerge",
			arguments: sumArguments,
			assertion: func(testRunner *testing.T, returnType sqlmodel.SQLType) {
				testRunner.Helper()
				assert.Equal(testRunner, "Float64", returnType.EngineName, "Merge reads the state back to the base type")
			},
		},
		{
			name:      "sumMergeState",
			arguments: sumArguments,
			assertion: func(testRunner *testing.T, returnType sqlmodel.SQLType) {
				testRunner.Helper()
				assert.Equal(testRunner, "AggregateFunction", returnType.EngineName, "MergeState yields the same shape as State")
			},
		},
		{
			name:      "sumSimpleState",
			arguments: sumArguments,
			assertion: func(testRunner *testing.T, returnType sqlmodel.SQLType) {
				testRunner.Helper()
				assert.Equal(testRunner, "Float64", returnType.EngineName, "SimpleState gives the value a SimpleAggregateFunction holds, the result's own type")
			},
		},
		{
			name:      "sumResample",
			arguments: sumArguments,
			assertion: func(testRunner *testing.T, returnType sqlmodel.SQLType) {
				testRunner.Helper()
				assert.Equal(testRunner, "Array", returnType.EngineName, "Resample wraps the result in Array")
				require.NotNil(testRunner, returnType.ElementType)
				assert.Equal(testRunner, "Float64", returnType.ElementType.EngineName, "Resample element type matches base")
			},
		},
		{
			name:      "sumDistinct",
			arguments: sumArguments,
			assertion: func(testRunner *testing.T, returnType sqlmodel.SQLType) {
				testRunner.Helper()
				assert.Equal(testRunner, "Float64", returnType.EngineName, "Distinct only affects the input pipeline, not the return")
			},
		},
		{
			name:      "sumForEach",
			arguments: sumArguments,
			assertion: func(testRunner *testing.T, returnType sqlmodel.SQLType) {
				testRunner.Helper()
				assert.Equal(testRunner, "Array", returnType.EngineName, "ForEach wraps the result in Array")
				require.NotNil(testRunner, returnType.ElementType)
				assert.Equal(testRunner, "Float64", returnType.ElementType.EngineName, "ForEach element type matches base")
			},
		},
		{
			name:      "sumMap",
			arguments: sumArguments,
			assertion: func(testRunner *testing.T, returnType sqlmodel.SQLType) {
				testRunner.Helper()
				assert.Equal(testRunner, "Map", returnType.EngineName, "Map wraps the result in Map(String, T)")
				require.NotNil(testRunner, returnType.KeyType)
				require.NotNil(testRunner, returnType.ElementType)
				assert.Equal(testRunner, "String", returnType.KeyType.EngineName, "Map key falls back to String")
				assert.Equal(testRunner, "Float64", returnType.ElementType.EngineName, "Map value matches base")
			},
		},
		{
			name:      "sumArgMin",
			arguments: sumArguments,
			assertion: func(testRunner *testing.T, returnType sqlmodel.SQLType) {
				testRunner.Helper()
				assert.Equal(testRunner, "Float64", returnType.EngineName, "ArgMin preserves the base type")
			},
		},
		{
			name:      "sumArgMax",
			arguments: sumArguments,
			assertion: func(testRunner *testing.T, returnType sqlmodel.SQLType) {
				testRunner.Helper()
				assert.Equal(testRunner, "Float64", returnType.EngineName, "ArgMax preserves the base type")
			},
		},
	}
	for index := range cases {
		testCase := cases[index]
		t.Run(testCase.name, func(testRunner *testing.T) {
			testRunner.Parallel()
			resolution, err := resolver.ResolveFunctionCall(t.Context(), nil, testCase.name, "", testCase.arguments)
			require.NoError(testRunner, err, "resolver should not return an error for %q", testCase.name)
			require.NotNil(testRunner, resolution, "resolver should produce a resolution for %q", testCase.name)
			testCase.assertion(testRunner, resolution.ReturnType)
		})
	}
}

func TestBuiltin_Now64ZeroArgOverload(t *testing.T) {
	t.Parallel()

	functions := New().BuiltinFunctions()
	signatures, found := functions.Functions["now64"]
	require.True(t, found)
	argumentCounts := make(map[int]bool)
	for _, signature := range signatures {
		argumentCounts[len(signature.Arguments)] = true
	}
	assert.True(t, argumentCounts[0], "now64() zero-argument overload must be registered")
	assert.True(t, argumentCounts[1], "now64(precision) overload must remain registered")
}

func TestBuiltin_ExtractURLParameterArity(t *testing.T) {
	t.Parallel()

	functions := New().BuiltinFunctions()
	signatures, found := functions.Functions["extracturlparameter"]
	require.True(t, found)
	require.NotEmpty(t, signatures)
	assert.Len(t, signatures[0].Arguments, 2, "extractURLParameter(URL, name) takes two arguments")
}

func TestBuiltin_PredicateReturnTypes(t *testing.T) {
	t.Parallel()

	functions := New().BuiltinFunctions()
	for _, name := range []string{"bitmapcontains", "s2cellsintersect", "polygonswithincartesian"} {
		signatures, found := functions.Functions[name]
		require.True(t, found, "%q missing", name)
		require.NotEmpty(t, signatures)
		assert.Equal(t, "UInt8", signatures[0].ReturnType.EngineName, "%q should return UInt8", name)
	}
}

func TestBuiltinFunctionSpecDecodes(t *testing.T) {
	t.Parallel()

	_, err := builtinFunctionSpec()

	require.NoError(t, err)
}

func TestBuildFunctionCatalogueReportsEmbeddedTablesThatDoNotDecode(t *testing.T) {
	t.Parallel()

	goodSpecification := dialectkit.EmbeddedFunctionSpec("lower(String) String\nupper(String) String\n")
	badSpecification := dialectkit.EmbeddedFunctionSpec("lower(String) String\nbroken spec line\nupper(String) String\n")
	goodAliases := func() (map[string]string, error) { return parseFunctionAliases("lcase lower\n") }
	badAliases := func() (map[string]string, error) { return parseFunctionAliases("lcase lower\none two three\n") }
	testCases := []struct {
		tables                 builtinTables
		name                   string
		wantSpecificationError bool
		wantAliasError         bool
		wantRegistered         []string
	}{
		{name: "tables that decode", tables: builtinTables{signatures: goodSpecification, aliases: goodAliases},
			wantRegistered: []string{"lower", "upper", "lcase"}},
		{name: "a spec line that does not decode", tables: builtinTables{signatures: badSpecification, aliases: goodAliases},
			wantSpecificationError: true, wantRegistered: []string{"lower", "upper", "lcase"}},
		{name: "an alias line that does not decode", tables: builtinTables{signatures: goodSpecification, aliases: badAliases},
			wantAliasError: true, wantRegistered: []string{"lower", "upper", "lcase"}},
		{name: "both tables with lines that do not decode", tables: builtinTables{signatures: badSpecification, aliases: badAliases},
			wantSpecificationError: true, wantAliasError: true, wantRegistered: []string{"lower", "upper", "lcase"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			functions, err := buildFunctionCatalogue(testCase.tables, nil, buildTypeCatalogue(nil))
			for _, name := range testCase.wantRegistered {
				require.Contains(t, functions.Functions, name, "every line that decoded is registered")
			}
			if !testCase.wantSpecificationError && !testCase.wantAliasError {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			if testCase.wantSpecificationError {
				require.Contains(t, err.Error(), "decoding embedded function spec")
				require.Contains(t, err.Error(), "line 2")
			}
			if testCase.wantAliasError {
				require.ErrorIs(t, err, errMalformedAliasLine)
			} else {
				require.NotErrorIs(t, err, errMalformedAliasLine)
			}
		})
	}
}

func TestEmbeddedTablesDecodeAndBuildTheCatalogueWithoutError(t *testing.T) {
	t.Parallel()

	functions, err := buildFunctionCatalogue(embeddedTables, nil, buildTypeCatalogue(nil))

	require.NoError(t, err)
	require.NoError(t, New().initErr)
	require.Equal(t, New().BuiltinFunctions().Functions, functions.Functions)
}

func TestBuiltinFunctionSpecNamesOnlyKnownTypes(t *testing.T) {
	t.Parallel()

	specification, err := builtinFunctionSpec()
	require.NoError(t, err)

	var unknown []string
	specification.Register(dialectkit.FunctionSpecHooks{
		ResolveType: func(spelling string) sqlmodel.SQLType {
			parsed, parseErr := specTypeResolver.parse(spelling)
			if parseErr != nil || namesUnknownType(parsed.SQLType) {
				unknown = append(unknown, spelling)
			}
			return sqlmodel.SQLType{}
		},
		DefaultNullability: nil,
		Add: func(_ string, signature *sqlmodel.Function) *sqlmodel.Function {
			return signature
		},
	})

	require.Empty(t, unknown)
}

func TestBuiltinFunctionCatalogueKeepsEverySignature(t *testing.T) {
	t.Parallel()

	functions := New().BuiltinFunctions().Functions
	signatures := 0
	for _, overloads := range functions {
		signatures += len(overloads)
	}

	require.Len(t, functions, 1442)
	require.Equal(t, 1599, signatures)
}

func TestBuiltinFunctionsKeepTheirSpellingUnderALowerCasedKey(t *testing.T) {
	t.Parallel()

	functions := New().BuiltinFunctions().Functions

	require.Len(t, functions["ipv4cidrtorange"], 1)
	signature := functions["ipv4cidrtorange"][0]
	assert.Equal(t, "IPv4CIDRToRange", signature.Name)
	require.Len(t, signature.ReturnType.StructFields, 2)
	assert.Equal(t, "low", signature.ReturnType.StructFields[0].Name)
	assert.True(t, signature.ReturnType.StructFields[0].Positional)
	assert.Equal(t, sqlmodel.FunctionNullableReturnsNullOnNull, signature.NullableBehaviour)
	assert.Equal(t, sqlmodel.FunctionNullableCalledOnNull, functions["tonullable"][0].NullableBehaviour)
	assert.Equal(t, sqlmodel.FunctionNullableNeverNull, functions["count"][0].NullableBehaviour)
}

func TestBuiltinFunctionsIgnoreExtraTypes(t *testing.T) {
	t.Parallel()

	engine := New(WithExtraTypes(map[string]sqlmodel.SQLType{
		"String": sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "CustomString"),
	}))

	assert.Equal(t, "String", engine.BuiltinFunctions().Functions["lower"][0].ReturnType.EngineName)
}

func namesUnknownType(sqlType sqlmodel.SQLType) bool {
	if sqlType.Category == sqlmodel.TypeCategoryUnknown && sqlType.EngineName != "" &&
		sqlType.EngineName != "Dynamic" && sqlType.EngineName != "Nothing" {
		return true
	}
	for index := range sqlType.StructFields {
		if namesUnknownType(sqlType.StructFields[index].SQLType) {
			return true
		}
	}
	return (sqlType.ElementType != nil && namesUnknownType(*sqlType.ElementType)) ||
		(sqlType.KeyType != nil && namesUnknownType(*sqlType.KeyType))
}

func TestBuiltinPredicatesReturnUInt8AsClickHouseDoes(t *testing.T) {
	t.Parallel()

	returnsBool := map[string]bool{"tobool": true}
	for name, signatures := range New().BuiltinFunctions().Functions {
		for _, signature := range signatures {
			if returnsBool[name] {
				continue
			}
			assert.NotEqual(t, sqlmodel.TypeCategoryBoolean, signature.ReturnType.Category,
				"%s returns %s, but ClickHouse returns UInt8 from a predicate", name, signature.ReturnType.EngineName)
		}
	}
}

func TestNullTestsNeverReturnNull(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		want sqlmodel.FunctionNullableBehaviour
	}{
		{name: "isNull", want: sqlmodel.FunctionNullableNeverNull},
		{name: "isNotNull", want: sqlmodel.FunctionNullableNeverNull},
		{name: "isZeroOrNull", want: sqlmodel.FunctionNullableNeverNull},
		{name: "isNullable", want: sqlmodel.FunctionNullableNeverNull},
		{name: "hasTokenOrNull", want: sqlmodel.FunctionNullableCalledOnNull},
		{name: "empty", want: sqlmodel.FunctionNullableReturnsNullOnNull},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, scalarNullBehaviour(testCase.name))
		})
	}
}

func TestToTypeNameNamesTheTypeOfAnyArgument(t *testing.T) {
	t.Parallel()

	signatures := New().BuiltinFunctions().Functions["totypename"]

	require.Len(t, signatures, 1)
	require.Equal(t, "toTypeName", signatures[0].Name)
	require.Equal(t, "String", signatures[0].ReturnType.EngineName)
	require.Equal(t, sqlmodel.FunctionNullableNeverNull, signatures[0].NullableBehaviour)
}

func TestFunctionNullBehaviourFollowsClickHouse(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		function  string
		aggregate bool
		want      sqlmodel.FunctionNullableBehaviour
	}{
		{name: "an ordinary function is NULL on a NULL argument", function: "lower", want: sqlmodel.FunctionNullableReturnsNullOnNull},
		{name: "an OrNull conversion may be NULL", function: "toInt32OrNull", want: sqlmodel.FunctionNullableCalledOnNull},
		{name: "nullIf may be NULL", function: "nullIf", want: sqlmodel.FunctionNullableCalledOnNull},
		{name: "isNull is never NULL", function: "isNull", want: sqlmodel.FunctionNullableNeverNull},
		{name: "toTypeName is never NULL", function: "toTypeName", want: sqlmodel.FunctionNullableNeverNull},
		{name: "a NULL-skipping aggregate", function: "max", aggregate: true, want: sqlmodel.FunctionNullableReturnsNullOnNull},
		{name: "count is never NULL", function: "countIf", aggregate: true, want: sqlmodel.FunctionNullableNeverNull},
		{name: "uniq is never NULL", function: "uniqExact", aggregate: true, want: sqlmodel.FunctionNullableNeverNull},
		{name: "groupArray is never NULL", function: "groupArraySorted", aggregate: true, want: sqlmodel.FunctionNullableNeverNull},
		{name: "topK is never NULL", function: "topK", aggregate: true, want: sqlmodel.FunctionNullableNeverNull},
		{name: "an OrNull aggregate may be NULL", function: "singleValueOrNull", aggregate: true, want: sqlmodel.FunctionNullableCalledOnNull},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			behaviour := scalarNullBehaviour(testCase.function)
			if testCase.aggregate {
				behaviour = aggregateNullBehaviour(testCase.function)
			}
			assert.Equal(t, testCase.want, behaviour)
		})
	}
}

func TestExtraFunctionRegistrationsFileSignaturesAsTheirMethodSays(t *testing.T) {
	t.Parallel()

	text, integer, float := primitiveType("String"), primitiveType("UInt64"), primitiveType("Float64")
	testCases := []struct {
		register      func(builder *FunctionCatalogueBuilder)
		name          string
		key           string
		wantName      string
		wantNull      sqlmodel.FunctionNullableBehaviour
		wantArguments int
		wantMinimum   int
		wantAggregate bool
		wantVariadic  bool
	}{
		{
			name:          "Register files a scalar under its lower-cased name",
			register:      func(builder *FunctionCatalogueBuilder) { builder.Register("MyScalar", text, integer) },
			key:           "myscalar",
			wantName:      "MyScalar",
			wantArguments: 1,
			wantNull:      sqlmodel.FunctionNullableReturnsNullOnNull,
		},
		{
			name:          "Register gives an OrNull scalar a result that may be NULL",
			register:      func(builder *FunctionCatalogueBuilder) { builder.Register("parseMineOrNull", integer, text) },
			key:           "parsemineornull",
			wantName:      "parseMineOrNull",
			wantArguments: 1,
			wantNull:      sqlmodel.FunctionNullableCalledOnNull,
		},
		{
			name:          "RegisterVariadic files a scalar whose last argument repeats",
			register:      func(builder *FunctionCatalogueBuilder) { builder.RegisterVariadic("joinAll", text, 2, text) },
			key:           "joinall",
			wantName:      "joinAll",
			wantArguments: 1,
			wantMinimum:   2,
			wantVariadic:  true,
			wantNull:      sqlmodel.FunctionNullableReturnsNullOnNull,
		},
		{
			name: "RegisterNullPropagating is NULL exactly on a NULL argument whatever its name",
			register: func(builder *FunctionCatalogueBuilder) {
				builder.RegisterNullPropagating("toMineOrNull", integer, text)
			},
			key:           "tomineornull",
			wantName:      "toMineOrNull",
			wantArguments: 1,
			wantNull:      sqlmodel.FunctionNullableReturnsNullOnNull,
		},
		{
			name:          "RegisterAggregate files an aggregate",
			register:      func(builder *FunctionCatalogueBuilder) { builder.RegisterAggregate("sumMine", integer, integer) },
			key:           "summine",
			wantName:      "sumMine",
			wantArguments: 1,
			wantAggregate: true,
			wantNull:      sqlmodel.FunctionNullableReturnsNullOnNull,
		},
		{
			name:          "RegisterAggregate follows ClickHouse's rule for a counting aggregate",
			register:      func(builder *FunctionCatalogueBuilder) { builder.RegisterAggregate("countMine", integer, text) },
			key:           "countmine",
			wantName:      "countMine",
			wantArguments: 1,
			wantAggregate: true,
			wantNull:      sqlmodel.FunctionNullableNeverNull,
		},
		{
			name:          "RegisterAggregateNoArgs files an aggregate without arguments that is never NULL",
			register:      func(builder *FunctionCatalogueBuilder) { builder.RegisterAggregateNoArgs("rowsSeen", integer) },
			key:           "rowsseen",
			wantName:      "rowsSeen",
			wantAggregate: true,
			wantNull:      sqlmodel.FunctionNullableNeverNull,
		},
		{
			name: "RegisterVariadicAggregate files a variadic aggregate that is never NULL",
			register: func(builder *FunctionCatalogueBuilder) {
				builder.RegisterVariadicAggregate("correlateAll", float, 2, float)
			},
			key:           "correlateall",
			wantName:      "correlateAll",
			wantArguments: 1,
			wantMinimum:   2,
			wantAggregate: true,
			wantVariadic:  true,
			wantNull:      sqlmodel.FunctionNullableNeverNull,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(WithExtraFunctions(testCase.register))
			require.NoError(t, engine.initErr)
			signatures := engine.BuiltinFunctions().Functions[testCase.key]
			require.Len(t, signatures, 1)
			signature := signatures[0]
			assert.Equal(t, testCase.wantName, signature.Name)
			assert.Len(t, signature.Arguments, testCase.wantArguments)
			assert.Equal(t, testCase.wantAggregate, signature.IsAggregate)
			assert.Equal(t, testCase.wantVariadic, signature.IsVariadic)
			assert.Equal(t, testCase.wantMinimum, signature.MinArguments)
			assert.Equal(t, testCase.wantNull, signature.NullableBehaviour)
			_, filedUnderWrittenName := engine.BuiltinFunctions().Functions[testCase.wantName]
			assert.Equal(t, testCase.wantName == testCase.key, filedUnderWrittenName)
		})
	}
}
