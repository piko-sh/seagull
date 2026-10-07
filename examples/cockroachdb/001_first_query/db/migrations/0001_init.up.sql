CREATE TABLE tasks (
    id         INT8        NOT NULL DEFAULT unique_rowid() PRIMARY KEY,
    title      STRING      NOT NULL,
    completed  BOOL        NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
