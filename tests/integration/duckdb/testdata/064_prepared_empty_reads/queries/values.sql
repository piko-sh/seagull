-- sgl.query(AddValue, exec)
INSERT INTO prepared_values (id,value) VALUES ($1,$2);

-- sgl.query(ReadValues, many)
SELECT id,value,value IS NULL AS is_null FROM prepared_values ORDER BY id;

-- sgl.query(ReadBound, many)
SELECT id,value,value IS NULL AS is_null FROM prepared_values WHERE id >= $1 ORDER BY id;

-- sgl.query(StreamValues, stream)
SELECT id,value,value IS NULL AS is_null FROM prepared_values ORDER BY id;
