-- sgl.query(name: Matching, command: many)
SELECT DISTINCT ON (category) id, category FROM q ORDER BY category, id DESC;

-- sgl.query(name: Mismatch, command: many)
SELECT DISTINCT ON (category) id FROM q ORDER BY id;

-- sgl.query(name: Swapped, command: many)
SELECT DISTINCT ON (category, id) id FROM q ORDER BY id, category;

-- sgl.query(name: Expression, command: many)
SELECT DISTINCT ON (lower(category)) id FROM q ORDER BY lower(category);

-- sgl.query(name: Alias, command: many)
SELECT DISTINCT ON (day) id, date_trunc('day', d) AS day FROM q ORDER BY day, id;
