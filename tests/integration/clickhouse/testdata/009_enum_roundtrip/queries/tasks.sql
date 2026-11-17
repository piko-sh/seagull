-- sgl.query(InsertTask, exec)
INSERT INTO tasks (id, status) VALUES ({id:UInt64}, {status:String});

-- sgl.query(GetTask, one)
SELECT id, status FROM tasks WHERE id = {id:UInt64};

-- sgl.query(ListTasks, many)
SELECT id, status FROM tasks ORDER BY id;
