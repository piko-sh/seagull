-- sgl.query(name: Page, command: many)
SELECT id FROM places ORDER BY id LIMIT :size::int OFFSET (:page::int - 1) * :size;

-- sgl.query(name: Labels, command: many)
VALUES (0, 'none') UNION ALL SELECT id, label FROM places ORDER BY 1;

-- sgl.query(name: Extremes, command: many)
SELECT x.id FROM ((SELECT id FROM places ORDER BY id LIMIT 1) UNION (SELECT id FROM places ORDER BY id DESC LIMIT 1)) AS x ORDER BY x.id;

-- sgl.query(name: EveryMark, command: many)
SELECT t.id, t.row FROM (TABLE marks) AS t WHERE EXISTS (TABLE places) ORDER BY t.id;

-- sgl.query(name: MarkRows, command: many)
SELECT row.id, row.row FROM marks AS row WHERE row.row = $1 ORDER BY row.id;

-- sgl.query(name: Renamed, command: many)
SELECT p.key, p.title FROM places AS p (key, title) WHERE p.key = $1;

-- sgl.query(name: Nearest, command: one)
SELECT id, location <-> point($1, $2) AS distance FROM places ORDER BY location <-> point($1, $2) LIMIT 1;

-- sgl.query(name: Everyday, command: one)
SELECT date(created) AS day, date_add(created, interval '1 day') AS tomorrow, trim_array(tags, 1) AS trimmed,
    character_length(label) AS characters, octet_length(body) AS octets, get_byte(body, 0) AS first_byte,
    bit_count(body) AS ones, ltrim(body, '\x00'::bytea) AS trimmed_body, jsonb_path_exists(doc, '$.a ? (@ > $min)', $1) AS over,
    greet(label) AS greeting, greet(who => label, punctuation => '?') AS asked, pg_current_xact_id() AS transaction
FROM places WHERE id = $2;
