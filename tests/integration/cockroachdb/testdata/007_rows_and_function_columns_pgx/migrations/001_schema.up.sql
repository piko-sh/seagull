CREATE TABLE harbours (
    id INT PRIMARY KEY,
    name STRING NOT NULL,
    facilities JSONB NOT NULL
);

CREATE TABLE berths (
    id INT PRIMARY KEY,
    harbour_id INT NOT NULL REFERENCES harbours (id),
    label STRING
);
