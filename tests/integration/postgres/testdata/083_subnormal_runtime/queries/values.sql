-- sgl.query(AddFloat, exec)
INSERT INTO fuzz_floats (value) VALUES ($1);

-- sgl.query(ReadFloat, many, dynamic: runtime)
SELECT value FROM fuzz_floats WHERE value = $1;

-- sgl.query(ReadArithmetic, many, dynamic: runtime)
SELECT value FROM fuzz_floats WHERE value = $1 + CAST(0 AS DOUBLE PRECISION);

-- sgl.query(ReadSlice, many)
-- $1 as sgl.param(value, kind: slice)
SELECT value FROM fuzz_floats WHERE value IN ($1);

-- sgl.query(BrowseFloats, many, dynamic: runtime)
SELECT value FROM fuzz_floats;

-- sgl.query(AddNullable, exec)
INSERT INTO fuzz_nullable (value) VALUES ($1);

-- sgl.query(ReadNullable, many, dynamic: runtime)
SELECT value FROM fuzz_nullable WHERE value = $1;
