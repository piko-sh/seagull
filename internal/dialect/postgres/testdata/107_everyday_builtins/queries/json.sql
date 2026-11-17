-- sgl.query(name: Paths, command: many)
SELECT
    jsonb_path_query_first(payload, '$.items[0]') AS first_item,
    jsonb_path_query_array(payload, '$.items[*].id') AS item_ids,
    jsonb_set(payload, tags, '1') AS tagged,
    jsonb_insert(payload, ARRAY['items', '0'], '{}', true) AS inserted,
    jsonb_set_lax(payload, '{gone}', NULL, true, 'delete_key') AS lax,
    jsonb_object(tags, tags) AS paired,
    num_nulls(body, NULL) AS nulls
FROM events;

-- sgl.query(name: Elements, command: many)
SELECT id, jsonb_array_elements(payload -> 'items') AS item, jsonb_object_keys(payload) AS key
FROM events;

-- sgl.query(name: Series, command: many)
SELECT generate_series(1, 3) AS n, generate_series(created_at, created_at + interval '2 days', interval '1 day') AS day
FROM events;
