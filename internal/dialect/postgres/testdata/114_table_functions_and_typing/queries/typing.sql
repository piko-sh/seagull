-- sgl.query(name: Arrays, command: one)
SELECT ARRAY[[1, 2], [3, 4]] AS grid, ARRAY(SELECT id FROM users) AS ids, ARRAY(SELECT jsonb_build_object('id', id) FROM users) AS documents;

-- sgl.query(name: Fields, command: many)
SELECT (bar).a AS first, (u.bar).b, (make_pair()).a AS made FROM users u WHERE (u.bar).b = $1;

-- sgl.query(name: Rename, command: exec)
UPDATE users SET name = :new_name WHERE id = $1 AND name = :old_name;

-- sgl.query(name: Documents, command: many)
SELECT id, doc FROM users;
