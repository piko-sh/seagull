-- sgl.query(CreateTask, one)
INSERT INTO tasks (title) VALUES (?) RETURNING id, title, completed;

-- sgl.query(ListTasks, many)
SELECT id, title, completed FROM tasks ORDER BY id;

-- sgl.query(SetNotes, exec)
UPDATE tasks SET notes = ? WHERE id = ?;

-- sgl.query(FindTask, one, optional: true)
SELECT id, title, notes FROM tasks WHERE id = ?;
