-- sgl.query(name: ReadBasketPaths, command: many)
SELECT
    JSON_EXTRACT(basket, '$.fish') AS fish,
    JSON_UNQUOTE(JSON_EXTRACT(basket, '$.fish')) AS fish_text,
    JSON_KEYS(basket) AS top_keys,
    JSON_KEYS(basket, '$.crate') AS crate_keys,
    JSON_LENGTH(basket) AS size,
    JSON_LENGTH(basket, '$.crate') AS crate_size,
    JSON_CONTAINS(basket, '"cod"', '$.fish') AS has_cod,
    JSON_CONTAINS(basket, note) AS has_note,
    JSON_VALUE(basket, '$.weight' RETURNING UNSIGNED) AS weight,
    CAST(id AS SIGNED) AS signed_id,
    JSON_UNQUOTE(basket) AS document
FROM baskets
WHERE JSON_KEYS(note) IS NOT NULL;
