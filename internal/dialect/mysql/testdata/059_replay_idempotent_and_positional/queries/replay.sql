-- sgl.query(name: ListT, command: many)
SELECT * FROM t;

-- sgl.query(name: ListUserCopies, command: many)
SELECT c.id, c.name, c.bio, p.id AS other_id FROM users_copy c JOIN users_copy_parenthesised p ON p.id = c.id;

-- sgl.query(name: ListPairs, command: many)
SELECT p.left_id, p.right_id, l.left_id AS later_left_id, l.right_id AS later_right_id FROM pairs p JOIN later_pairs l ON l.note = p.note;

-- sgl.query(name: ListRenamed, command: many)
SELECT n.id, v.label FROM new_name n JOIN new_view v ON v.id = n.id;

-- sgl.query(name: ListMoved, command: many)
SELECT id FROM archive.moved;
