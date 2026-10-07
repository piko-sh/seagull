CREATE TABLE tasks (
    id         BIGSERIAL   PRIMARY KEY,
    title      TEXT        NOT NULL,
    completed  BOOLEAN     NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE tasks IS 'Jobs to do around the harbour.';
COMMENT ON COLUMN tasks.completed IS 'Whether the job is done.';
