CREATE SCHEMA inventory;
CREATE TABLE inventory.items (id bigint PRIMARY KEY, name text NOT NULL, price numeric);
CREATE TABLE places (id bigint PRIMARY KEY, location point NOT NULL, label text NOT NULL, body bytea NOT NULL,
    doc jsonb NOT NULL, created timestamptz NOT NULL, tags text[] NOT NULL, query tsquery);
CREATE TABLE marks (id bigint PRIMARY KEY, row integer NOT NULL);
CREATE FUNCTION greet(who text, punctuation text = '!') RETURNS text LANGUAGE sql AS $$ SELECT 'hi ' || who || punctuation $$;
