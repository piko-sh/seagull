CREATE SEQUENCE task_ids;

CREATE TABLE tasks (
    id         BIGINT    PRIMARY KEY DEFAULT nextval('task_ids'),
    title      VARCHAR   NOT NULL,
    completed  BOOLEAN   NOT NULL DEFAULT false,
    created_at TIMESTAMP NOT NULL DEFAULT current_timestamp
);
