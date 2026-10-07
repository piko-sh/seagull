-- sgl.query(CreateTask, one)
INSERT INTO tasks (title) VALUES ($1) RETURNING id, title, completed, created_at;

-- sgl.query(ListTasks, many)
SELECT id, title, completed, created_at FROM tasks ORDER BY id;

-- sgl.query(CompleteTask, execrows)
UPDATE tasks SET completed = true WHERE id = $1;
