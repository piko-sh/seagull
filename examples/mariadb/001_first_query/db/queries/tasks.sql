-- sgl.query(CreateTask, one)
INSERT INTO tasks (title) VALUES (?) RETURNING id, title, completed, created_at;

-- sgl.query(ListTasks, many)
SELECT id, title, completed, created_at FROM tasks ORDER BY id;

-- sgl.query(CompleteTask, execrows)
UPDATE tasks SET completed = 1 WHERE id = ?;
