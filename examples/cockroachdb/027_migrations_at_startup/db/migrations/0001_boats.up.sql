CREATE TABLE boats (
    id   INT8   NOT NULL DEFAULT unique_rowid() PRIMARY KEY,
    name STRING NOT NULL
);
