CREATE TABLE places (id bigint PRIMARY KEY, label text NOT NULL, location point NOT NULL, body bytea NOT NULL, doc jsonb NOT NULL,
    created timestamptz NOT NULL, tags text[] NOT NULL);
CREATE TABLE marks (id bigint PRIMARY KEY, row integer NOT NULL);
CREATE FUNCTION greet(who text, punctuation text = '!') RETURNS text LANGUAGE sql AS $$ SELECT 'hi ' || who || punctuation $$;
