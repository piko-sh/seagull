CREATE TABLE events (
    id integer PRIMARY KEY
);

CREATE FUNCTION "Label"(value integer)
RETURNS integer
LANGUAGE sql IMMUTABLE
AS $$
    SELECT value * 2;
$$;

CREATE FUNCTION label(value integer)
RETURNS text
LANGUAGE sql IMMUTABLE
AS $$
    SELECT value::text;
$$;

CREATE FUNCTION "Scale"(factor bigint)
RETURNS bigint
LANGUAGE sql IMMUTABLE
AS $$
    SELECT factor * 10;
$$;

CREATE FUNCTION scale(factor integer)
RETURNS integer
LANGUAGE sql IMMUTABLE
AS $$
    SELECT factor * 10;
$$;

CREATE FUNCTION "Series"()
RETURNS TABLE (quoted_value integer)
LANGUAGE sql STABLE
AS $$
    SELECT 1;
$$;

CREATE FUNCTION series()
RETURNS TABLE (plain_value text)
LANGUAGE sql STABLE
AS $$
    SELECT 'one'::text;
$$;

CREATE FUNCTION "Purge"()
RETURNS integer
LANGUAGE sql
AS $$
    DELETE FROM events;
    SELECT 0;
$$;

CREATE FUNCTION purge()
RETURNS integer
LANGUAGE sql
AS $$
    SELECT count(*)::integer FROM events;
$$;

CREATE FUNCTION counted()
RETURNS integer
LANGUAGE sql
AS $$
    SELECT purge();
$$;
