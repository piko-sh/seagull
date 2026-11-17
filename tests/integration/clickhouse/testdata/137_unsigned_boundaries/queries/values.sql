-- sgl.query(AddValue, exec)
INSERT INTO unsigned_values (value) VALUES ({value:UInt64});

-- sgl.query(ReadValues, many)
SELECT value FROM unsigned_values ORDER BY value;

-- sgl.query(ReadSlice, many)
-- {values:UInt64} as sgl.param(values, kind: slice)
SELECT value FROM unsigned_values WHERE value IN ({values:UInt64}) ORDER BY value;

-- sgl.query(ReadOptional, many)
-- {value:Nullable(UInt64)} as sgl.param(value, optional: true)
SELECT value FROM unsigned_values WHERE value = {value:Nullable(UInt64)} ORDER BY value;

-- sgl.query(BrowseValues, many, dynamic: runtime)
SELECT value FROM unsigned_values WHERE value >= {value:UInt64};

-- sgl.query(AddNullable, exec)
INSERT INTO unsigned_optional (value) VALUES ({value:Nullable(UInt64)});

-- sgl.query(ReadNullable, many)
SELECT value FROM unsigned_optional ORDER BY value NULLS FIRST;
