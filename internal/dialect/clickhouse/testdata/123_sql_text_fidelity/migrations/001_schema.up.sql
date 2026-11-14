CREATE TABLE customers (
  id UInt64,
  company_name String
) ENGINE = MergeTree ORDER BY id;

CREATE TABLE quotes (
  id UInt64,
  description String,
  category String,
  price Int32,
  tags Array(String),
  quote_date DateTime,
  customer_id UInt64,
  deleted_at Nullable(DateTime)
) ENGINE = MergeTree ORDER BY id;
