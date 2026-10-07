-- sgl.query(name: Cleaned, command: many)
SELECT
    regexp_replace(body, '\s+', ' ', 'g') AS squeezed,
    regexp_match(body, '(\d+)') AS first_number,
    regexp_split_to_array(body, ',') AS parts,
    regexp_count(body, 'a') AS a_count,
    regexp_like(body, '^x', 'i') AS starts_with_x,
    regexp_substr(body, '\d+') AS digits,
    substring(body FROM '[0-9]+') AS pattern_match
FROM events;

-- sgl.query(name: Digests, command: one)
SELECT sha256(raw) AS digest, md5(raw) AS checksum, encode(sha512(raw), 'hex') AS long_digest,
       convert_from(raw, 'UTF8') AS decoded, convert_to(body, 'UTF8') AS encoded, to_hex(id) AS hex_id,
       length(raw) AS size, substring(raw FROM 1 FOR 4) AS head
FROM events
WHERE id = $1;

-- sgl.query(name: Words, command: many)
SELECT regexp_split_to_table(body, '\s+') AS word FROM events;
