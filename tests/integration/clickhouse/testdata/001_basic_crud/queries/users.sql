-- sgl.query(InsertUser, exec)
INSERT INTO users (id, name) VALUES ({id:UInt64}, {name:String});

-- sgl.query(GetUser, one)
SELECT id, name FROM users WHERE id = {id:UInt64};

-- sgl.query(ListUsers, many)
SELECT id, name FROM users ORDER BY id;
