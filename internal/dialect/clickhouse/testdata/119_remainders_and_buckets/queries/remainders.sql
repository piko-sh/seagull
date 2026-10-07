-- sgl.query(name: Remainders, command: many)
SELECT id, positiveModulo(sensor, slot) AS positive, pmod(sensor, sensor) AS positive_wide,
    positive_modulo(wide, small) AS positive_small, modulo(wide, small) AS signed_remainder, wide % small AS operator_remainder,
    moduloOrZero(sensor, slot) AS remainder_or_zero, positiveModulo(level, slot) AS positive_float
FROM readings;

-- sgl.query(name: Buckets, command: many)
SELECT id, widthBucket(level, 0, 100, buckets) AS bucket, width_bucket(level, 0, 100, toUInt32(buckets)) AS wide_bucket FROM readings;
