-- sgl.query(name: Readings, command: many)
SELECT
    plain,
    single,
    widened,
    widest,
    scaled,
    CAST(plain AS FLOAT(53)) AS plain_doubled,
    CAST(widened AS FLOAT(10)) AS widened_narrowed
FROM readings
WHERE widened > CAST(? AS FLOAT(40));
