-- sgl.query(AddFloat, exec)
INSERT INTO fuzz_floats (value) VALUES ({value:Float64});

-- sgl.query(ReadFloat, many, dynamic: runtime)
SELECT value FROM fuzz_floats WHERE value = {value:Float64};

-- sgl.query(ReadArithmetic, many, dynamic: runtime)
SELECT value FROM fuzz_floats WHERE value = {value:Float64} + CAST(0 AS Float64);

-- sgl.query(ReadSlice, many)
-- {value:Float64} as sgl.param(value, kind: slice)
SELECT value FROM fuzz_floats WHERE value IN ({value:Float64});

-- sgl.query(BrowseFloats, many, dynamic: runtime)
SELECT value FROM fuzz_floats;

-- sgl.query(AddNullable, exec)
INSERT INTO fuzz_nullable (value) VALUES ({value:Nullable(Float64)});

-- sgl.query(ReadNullable, many, dynamic: runtime)
SELECT value FROM fuzz_nullable WHERE value = {value:Nullable(Float64)};

-- sgl.query(ReadArray, many, dynamic: runtime)
SELECT value FROM fuzz_floats WHERE has({values:Array(Float64)}, value);

-- sgl.query(ReadNullableArray, many, dynamic: runtime)
SELECT value FROM fuzz_floats WHERE has({values:Array(Nullable(Float64))}, value);

-- sgl.query(ReadNestedArray, many, dynamic: runtime)
SELECT value FROM fuzz_floats WHERE has({values:Array(Array(Float64))}[1], value);

-- sgl.query(ReadMap, many, dynamic: runtime)
SELECT value FROM fuzz_floats WHERE has(mapValues({values:Map(String, Float64)}), value);

-- sgl.query(ReadNamedMap, many)
SELECT value FROM fuzz_floats WHERE has(mapValues({values:Map(String, Float64)}), value);

-- sgl.query(ReadTuple, many, dynamic: runtime)
SELECT value FROM fuzz_floats WHERE value = tupleElement({pair:Tuple(score Float64, label String)}, 1);

-- sgl.query(AddFloatBatch, batch)
INSERT INTO fuzz_floats (value) VALUES ({value:Float64});

-- sgl.query(AddFloatRows, batch)
INSERT INTO fuzz_floats (value) VALUES ({value:Float64} + toFloat64(0));
