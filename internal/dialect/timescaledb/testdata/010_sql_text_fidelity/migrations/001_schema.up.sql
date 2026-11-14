CREATE TABLE customers (
  id bigserial PRIMARY KEY,
  company_name text NOT NULL
);

CREATE TABLE quotes (
  id bigserial PRIMARY KEY,
  description text NOT NULL,
  category text NOT NULL,
  price integer NOT NULL,
  tags text[] NOT NULL,
  quote_date timestamptz NOT NULL,
  customer_id bigint NOT NULL REFERENCES customers (id),
  deleted_at timestamptz
);
