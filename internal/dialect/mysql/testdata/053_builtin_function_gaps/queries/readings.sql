-- sgl.query(name: SummariseReadings, command: many)
SELECT
    label,
    ROUND(SUM(price), 2) AS total,
    CEIL(MAX(ratio)) AS ratio_ceiling,
    MOD(MAX(ratio), 2) AS ratio_remainder,
    ABS(MAX(id)) AS largest_id,
    CRC32(label) AS checksum,
    MD5(label) AS digest,
    SHA2(label, 256) AS long_digest,
    FIELD(label, 'cold', 'warm') AS position,
    MONTHNAME(MAX(taken_at)) AS month_name,
    EXTRACT(YEAR FROM MAX(taken_at)) AS taken_year,
    UTC_TIMESTAMP(6) AS summarised_at,
    LEFT(MAX(taken_at), 7) AS taken_month,
    LENGTH(MAX(id)) AS id_digits,
    REGEXP_SUBSTR(label, '[0-9]+') AS label_number,
    CONVERT_TZ(MAX(taken_at), '+00:00', '+01:00') AS local_taken_at,
    VAR_POP(ratio) AS ratio_variance
FROM readings
GROUP BY label;
