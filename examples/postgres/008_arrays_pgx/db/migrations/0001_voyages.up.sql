CREATE TABLE voyages (
    id        BIGINT   GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    boat      TEXT     NOT NULL,
    ports     TEXT[]   NOT NULL,
    catch_kg  BIGINT[] NOT NULL,
    sightings TEXT[]
);
-- sgl.column(voyages.ports, non_null_elements: true)
