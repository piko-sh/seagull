-- sgl.query(AddFloat, exec)
INSERT INTO fuzz_floats (value) VALUES (?);

-- sgl.query(ReadFloat, many, dynamic: runtime)
SELECT value FROM fuzz_floats WHERE value = ?;

-- sgl.query(ReadArithmetic, many, dynamic: runtime)
SELECT value FROM fuzz_floats WHERE value = ? + CAST(0 AS DOUBLE);

-- sgl.query(ReadSlice, many)
-- ?1 as sgl.param(value, kind: slice)
SELECT value FROM fuzz_floats WHERE value IN (?);

-- sgl.query(BrowseFloats, many, dynamic: runtime)
SELECT value FROM fuzz_floats;

-- sgl.query(AddNullable, exec)
INSERT INTO fuzz_nullable (value) VALUES (?);

-- sgl.query(ReadNullable, many, dynamic: runtime)
SELECT value FROM fuzz_nullable WHERE value = ?;
