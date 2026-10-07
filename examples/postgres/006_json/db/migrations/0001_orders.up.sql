CREATE TABLE orders (
    id       BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    customer TEXT   NOT NULL,
    basket   JSONB  NOT NULL,
    delivery JSONB
);
