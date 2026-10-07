-- sgl.query(name: UsualFares, command: many)
SELECT city, mode(fare) AS usual_fare FROM trips GROUP BY city ORDER BY city;

-- sgl.query(name: Parsed, command: many)
SELECT id, regexp_extract(note, '(\d+)-(\d+)', 2) AS second_number, regexp_replace(note, '\s+', ' ', 'g') AS squeezed,
       strftime('%Y-%m', started_at) AS month, contains(stops, 3) AS stops_at_three
FROM trips
ORDER BY id;

-- sgl.query(name: Lists, command: many)
SELECT id, range(3) AS counter, generate_series(started_at, started_at + INTERVAL 2 HOUR, INTERVAL 1 HOUR) AS hours
FROM trips
ORDER BY id;

-- sgl.query(name: Documents, command: many)
SELECT id, json_extract_string(doc, ['$.a', '$.b']) AS texts, json_extract(doc, ['$.a']) AS parts, json_keys(doc, '$.meta') AS meta_keys
FROM trips
ORDER BY id;
