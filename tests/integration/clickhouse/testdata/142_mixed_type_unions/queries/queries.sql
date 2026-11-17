-- sgl.query(MixedColumns, many)
SELECT narrow, short FROM (
    SELECT count AS narrow, small AS short FROM readings
    UNION ALL
    SELECT delta, toUInt8(id) FROM readings
)
ORDER BY narrow, short;

-- sgl.query(NoCommonType, many)
SELECT total AS wide FROM readings
UNION ALL
SELECT offset_value FROM readings;

-- sgl.query(Extremes, many)
SELECT greatest(count, delta) AS biggest, least(total, small) AS smallest, greatest(total, offset_value) AS widest
FROM readings
ORDER BY id;
