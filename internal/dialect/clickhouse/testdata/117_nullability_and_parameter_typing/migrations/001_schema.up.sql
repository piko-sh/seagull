CREATE TABLE authors (id Int32, name String, nickname Nullable(String)) ENGINE = MergeTree ORDER BY id;
CREATE TABLE books (id Int32, author_id Nullable(Int32), title String, price Nullable(Decimal(10, 2)), published DateTime)
    ENGINE = MergeTree ORDER BY id;
