-- sgl.query(AddValue, exec)
INSERT INTO binary_values (value) VALUES ($1);

-- sgl.query(ReadValues, many)
SELECT value, value IS NULL AS is_null FROM binary_values ORDER BY value NULLS FIRST;

-- sgl.query(ReadSlice, many)
-- $1 as sgl.param(values, kind: slice)
SELECT value FROM binary_values WHERE value IN ($1) ORDER BY value;

-- sgl.query(BrowseValues, many, dynamic: runtime)
SELECT value FROM binary_values;
