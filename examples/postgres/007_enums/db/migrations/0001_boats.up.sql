CREATE TYPE boat_status AS ENUM ('moored', 'at_sea', 'dry_dock');

CREATE TABLE boats (
    id     BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name   TEXT        NOT NULL,
    status boat_status NOT NULL DEFAULT 'moored'
);
