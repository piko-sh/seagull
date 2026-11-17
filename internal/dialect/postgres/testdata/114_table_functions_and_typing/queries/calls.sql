-- sgl.query(name: CreateUser, command: one)
CALL create_user($1, NULL);

-- sgl.query(name: Tidy, command: exec)
CALL tidy($1);
