CREATE TABLE authors (id Int32, name String) ENGINE = MergeTree ORDER BY id;
CREATE TABLE books (id Int32, author_id Int32, title String, price Nullable(Decimal(10, 2))) ENGINE = MergeTree ORDER BY id;
