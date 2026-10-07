CREATE TABLE zebras (
    id integer PRIMARY KEY,
    stripe_count integer NOT NULL
);

CREATE TABLE apples (
    id integer PRIMARY KEY,
    zebra_id integer NOT NULL REFERENCES zebras (id),
    variety text NOT NULL
);
