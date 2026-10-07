CREATE TYPE mood AS ENUM ('sad', 'ok', 'happy');

CREATE TABLE events (
    id integer PRIMARY KEY,
    body text NOT NULL,
    raw bytea NOT NULL,
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL,
    happened timestamp NOT NULL,
    amount numeric NOT NULL,
    ratio float8 NOT NULL,
    tags text[] NOT NULL,
    booked tstzrange NOT NULL,
    seats int4range NOT NULL,
    feeling mood NOT NULL
);
