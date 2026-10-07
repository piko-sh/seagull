-- sgl.query(CreateTask, one)
INSERT INTO tasks (title) VALUES ($1) RETURNING id, title, completed;

-- sgl.query(ListTasks, many)
SELECT id, title, completed FROM tasks ORDER BY id;
