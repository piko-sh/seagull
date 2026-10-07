-- sgl.query(ThingCasts, one)
SELECT CAST(flag AS BOOLEAN) AS flag_number, CAST(amount AS BOOL) AS amount_number, CAST(doc AS JSON) AS doc_number,
       CAST(doc AS JSONB) AS binary_doc_number, CAST(label AS STRING) AS label_number, CAST(amount AS ANY) AS any_number,
       CAST(label AS TEXT) AS label_text, CAST(amount AS BIGINT) AS whole
FROM things WHERE id = ?;

-- sgl.query(ThingsWhen, many)
SELECT id FROM things WHERE CAST(? AS BOOLEAN) ORDER BY id;
