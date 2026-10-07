-- sgl.query(name: MySQLOnlyFunctions, command: many)
SELECT
    label,
    ANY_VALUE(tally) AS any_tally,
    REGEXP_LIKE(label, '^a') AS starts_with_a,
    BIN_TO_UUID(MAX(token)) AS token_text,
    IS_UUID(label) AS label_is_uuid,
    UUID_TO_BIN(UUID()) AS fresh_token,
    JSON_STORAGE_SIZE(MAX(document)) AS document_size,
    GROUPING(label) AS label_grouped
FROM readings
GROUP BY label WITH ROLLUP;

-- sgl.query(name: ArgumentsMariaDBTakes, command: many)
SELECT
    id,
    REGEXP_INSTR(label, 'a') AS first_a,
    REGEXP_SUBSTR(label, '[0-9]+') AS label_number,
    REGEXP_REPLACE(label, 'a', 'b') AS replaced,
    LAG(tally, 1) OVER (ORDER BY id) AS previous_tally,
    LEAD(tally) OVER (ORDER BY id) AS next_tally
FROM readings;

-- sgl.query(name: ArgumentsOnlyMySQLTakes, command: many)
SELECT
    id,
    REGEXP_INSTR(label, 'a', 2) AS later_a,
    REGEXP_SUBSTR(label, '[0-9]+', 1, 2) AS second_number,
    REGEXP_REPLACE(label, 'a', 'b', 1, 1) AS first_replaced,
    LAG(tally, 1, 0) OVER (ORDER BY id) AS previous_tally
FROM readings;
