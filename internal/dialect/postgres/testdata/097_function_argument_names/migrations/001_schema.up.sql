CREATE TABLE catches (
    id integer PRIMARY KEY,
    kilos integer NOT NULL
);

CREATE FUNCTION scale_price(amount integer, factor double precision) RETURNS double precision
LANGUAGE sql IMMUTABLE STRICT AS $$ SELECT amount * factor $$;

CREATE PROCEDURE tidy_catches(older_than integer, keep integer)
LANGUAGE sql AS $$ DELETE FROM catches WHERE id < older_than AND kilos > keep $$;
