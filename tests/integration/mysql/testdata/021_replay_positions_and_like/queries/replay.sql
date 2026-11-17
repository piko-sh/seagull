-- sgl.query(name: ListT, command: many)
SELECT * FROM t ORDER BY a;

-- sgl.query(name: ListUserCopies, command: many)
SELECT * FROM users_copy ORDER BY id;

-- sgl.query(name: ListUserParenthesisedCopies, command: many)
SELECT * FROM users_copy_parenthesised ORDER BY id;

-- sgl.query(name: ListPairs, command: many)
SELECT * FROM pairs ORDER BY left_id;

-- sgl.query(name: ListLaterPairs, command: many)
SELECT * FROM later_pairs ORDER BY left_id;

-- sgl.query(name: ListRenamed, command: many)
SELECT * FROM new_name ORDER BY id;

-- sgl.query(name: ListRenamedView, command: many)
SELECT * FROM new_view ORDER BY id;
