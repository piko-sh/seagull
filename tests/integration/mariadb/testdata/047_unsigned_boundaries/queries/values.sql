-- sgl.query(AddValue, exec)
INSERT INTO unsigned_values (value) VALUES (?);

-- sgl.query(ReadValues, many)
SELECT value FROM unsigned_values ORDER BY value;

-- sgl.query(ReadSlice, many)
-- ?1 as sgl.param(values, kind: slice)
SELECT value FROM unsigned_values WHERE value IN (?) ORDER BY value;

-- sgl.query(ReadOptional, many)
-- ?1 as sgl.param(value, optional: true)
SELECT value FROM unsigned_values WHERE value = ? ORDER BY value;

-- sgl.query(BrowseValues, many, dynamic: runtime)
SELECT value FROM unsigned_values WHERE value >= ?;

-- sgl.query(AddNullable, exec)
INSERT INTO unsigned_optional (value) VALUES (?);

-- sgl.query(ReadNullable, many)
SELECT value FROM unsigned_optional ORDER BY value;
