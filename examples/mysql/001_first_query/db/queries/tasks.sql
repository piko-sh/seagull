-- sgl.query(CreateTask, execresult)
INSERT INTO tasks (title) VALUES (?);

-- sgl.query(ListTasks, many)
SELECT id, title, completed, created_at FROM tasks ORDER BY id;

-- sgl.query(CompleteTask, execrows)
UPDATE tasks SET completed = 1 WHERE id = ?;
