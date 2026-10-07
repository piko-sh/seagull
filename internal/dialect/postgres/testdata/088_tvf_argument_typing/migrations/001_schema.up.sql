CREATE SCHEMA app;

CREATE TABLE app.users (
    id INTEGER PRIMARY KEY,
    email TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE OR REPLACE FUNCTION app.list_users_since(_min_id INTEGER, _active BOOLEAN DEFAULT TRUE)
RETURNS TABLE (
    user_id INTEGER,
    user_email TEXT,
    user_created_at TIMESTAMPTZ
)
LANGUAGE sql STABLE
AS $$
    SELECT id, email, created_at
    FROM app.users
    WHERE id >= _min_id;
$$;
