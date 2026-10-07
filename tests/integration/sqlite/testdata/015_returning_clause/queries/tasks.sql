-- sgl.query(name: CreateTask, command: one)
INSERT INTO tasks (title) VALUES (?) RETURNING id, title, done;

-- sgl.query(name: MarkAllDone, command: many)
UPDATE tasks SET done = 1 WHERE done = 0 RETURNING id, title;

-- sgl.query(name: DeleteTask, command: one)
DELETE FROM tasks WHERE id = ? RETURNING id, title;
