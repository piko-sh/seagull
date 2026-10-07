-- CreateTask adds a task and returns it with the id and creation time the database
-- gave it.
-- sgl.query(CreateTask, one)
INSERT INTO tasks (title) VALUES ($1) RETURNING id, title, completed, created_at;

-- ListTasks returns every task, oldest first.
-- sgl.query(ListTasks, many)
SELECT id, title, completed, created_at FROM tasks ORDER BY id;

-- CompleteTask marks a task done and returns how many tasks it changed.
-- sgl.query(CompleteTask, execrows)
UPDATE tasks SET completed = true WHERE id = $1;
