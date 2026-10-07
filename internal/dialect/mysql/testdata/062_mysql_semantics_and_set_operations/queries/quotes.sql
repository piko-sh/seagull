-- sgl.query(name: Labels, command: many)
SELECT id, CONCAT(first_name, ":", last_name) AS label, "member" AS kind, 'a' "b" AS joined, visits 'visit_count'
FROM people
WHERE "x" = ?;
