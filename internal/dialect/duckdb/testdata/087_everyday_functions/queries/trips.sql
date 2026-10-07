-- sgl.query(name: CityStats, command: many)
SELECT city, mode(fare) AS usual_fare, quantile_cont(fare, [0.25, 0.75]) AS quartiles
FROM trips
GROUP BY city;

-- sgl.query(name: Parsed, command: many)
SELECT regexp_extract(note, '(\d+)-(\d+)', 2) AS second_number, regexp_replace(note, '\s+', ' ', 'g') AS squeezed,
       strftime('%Y-%m', started_at) AS month, contains(stops, 3) AS stops_at_three, contains(tags, 'night') AS tagged,
       isfinite(started_at) AS finite, md5(raw) AS checksum
FROM trips;

-- sgl.query(name: Lists, command: many)
SELECT id, range(3) AS counter, generate_series(started_at, started_at + INTERVAL 2 HOUR, INTERVAL 1 HOUR) AS hours,
       list_aggregate(stops, 'string_agg', '|') AS joined
FROM trips;

-- sgl.query(name: Documents, command: many)
SELECT json_extract(doc, ['$.a', '$.b']) AS parts, json_extract_string(doc, 0) AS first_text,
       json_keys(doc, '$.meta') AS meta_keys, json_array_length(doc, '$.items') AS item_count
FROM trips;
