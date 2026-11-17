-- sgl.query(name: Everyday, command: many)
SELECT obj_description('places'::regclass, 'pg_class') AS described, col_description('places'::regclass, 1) AS column_described,
    pg_database_size(current_database()) AS database_size, pg_relation_size('places') AS relation_size, date(created) AS day,
    date_add(created, interval '1 day') AS tomorrow, date_subtract(created, interval '1 day', 'UTC') AS yesterday,
    trim_array(tags, 1) AS trimmed, character_length(label) AS characters, txid_current() AS transaction_id,
    pg_current_xact_id() AS current_xact_id
FROM places;

-- sgl.query(name: Bytes, command: many)
SELECT octet_length(body) AS octets, bit_length(body) AS bits, get_byte(body, 0) AS first_byte, set_byte(body, 0, 1) AS set,
    get_bit(body, 0) AS first_bit, bit_count(body) AS ones, ltrim(body, '\x00'::bytea) AS trimmed,
    overlay(body PLACING '\x01'::bytea FROM 1) AS overlaid, length(body, 'UTF8') AS characters
FROM places;

-- sgl.query(name: Paths, command: many)
SELECT jsonb_path_exists(doc, '$.a ? (@ > $min)', $1) AS over, jsonb_path_match(doc, 'exists($.a)', vars => '{}', silent => true) AS has
FROM places;

-- sgl.query(name: Greetings, command: one)
SELECT greet($1) AS plain, greet(who => $2, punctuation => '?') AS asked;
