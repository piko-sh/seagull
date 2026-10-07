-- sgl.query(CreateTask, one)
INSERT INTO tasks (title) VALUES (?) RETURNING id, title, completed;

-- sgl.query(ListTasks, many)
SELECT id, title, completed FROM tasks ORDER BY id;

-- sgl.query(SetNotes, exec)
UPDATE tasks SET notes = ? WHERE id = ?;

-- sgl.query(FindTask, one, optional: true)
SELECT id, title, notes FROM tasks WHERE id = ?;

-- sgl.query(AddProject, exec)
INSERT INTO projects (name) VALUES (?);

-- sgl.query(AssignProject, exec)
UPDATE tasks SET project_id = ? WHERE id = ?;

-- sgl.query(SearchTasks, many)
-- sgl.embed(projects, from: p)
-- ?1 as sgl.param(title, optional: true)
-- ?2 as sgl.param(page_size, default: 10, max: 50)
SELECT t.id, t.title, p.id, p.name
FROM tasks t LEFT JOIN projects p ON p.id = t.project_id
WHERE (?1 IS NULL OR t.title = ?1)
ORDER BY t.id LIMIT ?2;

-- sgl.query(TaskPriority, one)
SELECT priority FROM tasks WHERE id = ?;
