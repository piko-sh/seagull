CREATE TABLE tasks (
    id INTEGER PRIMARY KEY,
    title TEXT NOT NULL,
    done INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE open_tasks AS SELECT id, title, upper(title) AS shout FROM tasks WHERE done = 0;
