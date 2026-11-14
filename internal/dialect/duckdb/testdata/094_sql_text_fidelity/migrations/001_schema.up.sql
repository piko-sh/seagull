CREATE TABLE customers (
  id BIGINT PRIMARY KEY,
  company_name VARCHAR NOT NULL
);

CREATE TABLE quotes (
  id BIGINT PRIMARY KEY,
  description VARCHAR NOT NULL,
  category VARCHAR NOT NULL,
  price INTEGER NOT NULL,
  tags VARCHAR[] NOT NULL,
  quote_date TIMESTAMP NOT NULL,
  customer_id BIGINT NOT NULL,
  deleted_at TIMESTAMP
);
