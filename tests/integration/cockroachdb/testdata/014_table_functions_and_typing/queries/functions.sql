-- sgl.query(name: UsersNamed, command: many)
SELECT * FROM users_named($1);

-- sgl.query(name: TwoOuts, command: one)
SELECT * FROM two_outs($1);

-- sgl.query(name: Total, command: one)
SELECT t FROM one_out($1) AS t;

-- sgl.query(name: Lowered, command: one)
SELECT l FROM lower('ABC') AS l;

-- sgl.query(name: CreateUser, command: one)
CALL create_user($1, NULL);

-- sgl.query(name: Populate, command: one)
SELECT * FROM json_populate_record(NULL::results, $1);

-- sgl.query(name: Fields, command: many)
SELECT (bar).a AS first, (u.bar).b FROM users u WHERE (u.bar).b = $1;

-- sgl.query(name: Arrays, command: one)
SELECT ARRAY(SELECT id FROM users ORDER BY id) AS ids, ARRAY[[1, 2], [3, 4]] AS grid;
