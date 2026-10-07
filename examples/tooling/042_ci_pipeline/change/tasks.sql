-- sgl.query(CreateTask, one)
INSERT INTO tasks (title) VALUES (?) ON CONFLICT (title) DO NOTHING RETURNING id, title, done;

-- sgl.query(ListTasks, many)
SELECT id, title, done FROM tasks ORDER BY id;
