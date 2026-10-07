-- sgl.query(FindTasks, many)
-- :title as sgl.param(title, optinal: true)
SELECT id, title, done FROM tasks WHERE (:title IS NULL OR title = :title) ORDER BY id;

-- sgl.query(ListTasks, many)
SELECT id, title, done FROM tasks ORDER BY id;
