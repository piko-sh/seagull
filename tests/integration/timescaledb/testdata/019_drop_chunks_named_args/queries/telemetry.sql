-- sgl.query(name: InsertTelemetry, command: exec)
INSERT INTO telemetry (ts, value) VALUES ($1, $2);

-- sgl.query(name: DropOldChunks, command: many)
WITH dropped AS (
    SELECT drop_chunks(
        'telemetry',
        older_than => '2026-01-01'::timestamptz,
        verbose    => true
    ) AS raw_chunk
)
SELECT raw_chunk::text AS dropped_chunk FROM dropped;

-- sgl.query(name: RemainingRows, command: one)
SELECT count(*)::bigint AS row_count FROM telemetry;
