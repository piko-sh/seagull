CREATE TABLE events (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL
);

CREATE FUNCTION write_event(event_name TEXT) RETURNS INT
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO events (name) VALUES (event_name);
    RETURN 1;
END;
$$;

CREATE FUNCTION audited(event_name TEXT) RETURNS INT
LANGUAGE sql
RETURN write_event(event_name);

CREATE FUNCTION audited_atomic(event_name TEXT) RETURNS INT
LANGUAGE sql
BEGIN ATOMIC
    SELECT write_event(event_name);
END;

CREATE FUNCTION tidied(event_name TEXT) RETURNS TEXT
LANGUAGE sql
VOLATILE
RETURN lower(trim(event_name));

CREATE FUNCTION described(event_name TEXT) RETURNS TEXT
LANGUAGE plpgsql
AS $$
DECLARE
    label VARCHAR(20);
BEGIN
    -- write_event(event_name) is only mentioned here
    RAISE NOTICE 'INSERT %', event_name;
    RETURN upper(event_name);
END;
$$;
