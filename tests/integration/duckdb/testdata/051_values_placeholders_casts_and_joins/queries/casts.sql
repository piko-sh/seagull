-- sgl.query(name: Tried, command: many)
SELECT id, TRY_CAST(note AS INTEGER) AS number, TRY_CAST(city AS INTEGER) AS city_number, try(CAST(city AS INTEGER)) AS attempted,
    CAST([id, id, id] AS INTEGER[3]) AS triple
FROM trips ORDER BY id;
