CREATE SCHEMA analytics;

CREATE FUNCTION analytics.stats()
RETURNS TABLE (a integer)
LANGUAGE sql STABLE
AS $$
    SELECT 1;
$$;

CREATE FUNCTION public.stats()
RETURNS TABLE (b text)
LANGUAGE sql STABLE
AS $$
    SELECT 'b'::text;
$$;
