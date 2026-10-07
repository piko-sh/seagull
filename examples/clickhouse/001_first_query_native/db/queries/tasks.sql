-- sgl.query(CreateTask, exec)
INSERT INTO tasks (id, title) VALUES ({id:UInt64}, {title:String});

-- sgl.query(GetTask, one)
SELECT id, title, completed, created_at FROM tasks WHERE id = {id:UInt64};

-- sgl.query(ListTasks, many)
SELECT id, title, completed, created_at FROM tasks ORDER BY id;

-- sgl.query(CompleteTask, asyncexec)
ALTER TABLE tasks UPDATE completed = 1 WHERE id = {id:UInt64};
