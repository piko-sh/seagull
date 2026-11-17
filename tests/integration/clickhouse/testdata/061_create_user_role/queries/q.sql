-- sgl.query(InsertNoop, exec)
INSERT INTO noop (id) VALUES ({id:UInt64});

-- sgl.query(CountUsers, one)
SELECT count() AS user_count
FROM system.users
WHERE name = 'test_user';

-- sgl.query(CountRoles, one)
SELECT count() AS role_count
FROM system.roles
WHERE name = 'test_reader';
