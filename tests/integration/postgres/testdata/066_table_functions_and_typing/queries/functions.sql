-- sgl.query(name: UsersNamed, command: many)
SELECT * FROM users_named($1);

-- sgl.query(name: OneUser, command: one)
SELECT u.id, u.name FROM one_user($1) AS u;

-- sgl.query(name: TwoOuts, command: one)
SELECT * FROM two_outs($1);

-- sgl.query(name: AuthorID, command: one)
SELECT x.id FROM add_author($1) AS x;

-- sgl.query(name: Twice, command: one)
SELECT t FROM twice($1) AS t;

-- sgl.query(name: Builtins, command: one)
SELECT l, now FROM lower('ABC') AS l, now();

-- sgl.query(name: Today, command: one)
SELECT * FROM CURRENT_DATE;

-- sgl.query(name: NameLengths, command: many)
SELECT u.id, n FROM users u, length(u.name) n;

-- sgl.query(name: MadePair, command: one)
SELECT * FROM make_pair();
