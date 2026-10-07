-- sgl.query(name: InsertReading, command: exec)
INSERT INTO readings (ts, device, value) VALUES ($1, $2, $3);

-- sgl.query(name: PickChunk, command: one)
SELECT show_chunks('readings')::text AS chunk_name
LIMIT 1;

-- sgl.query(name: ConvertChunk, command: exec)
CALL convert_to_columnstore($1::regclass);

-- sgl.query(name: TotalAfterConvert, command: one)
SELECT
    sum(value)::double precision AS total,
    count(*)::bigint             AS row_count
FROM readings;
