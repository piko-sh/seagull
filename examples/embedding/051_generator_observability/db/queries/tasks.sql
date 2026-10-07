-- sgl.query(CreateTask, one)
INSERT INTO tasks (title) VALUES (?) RETURNING id, title, done;

-- sgl.query(ListOpenTasks, many)
SELECT id, title FROM tasks WHERE done = 0 ORDER BY id;
