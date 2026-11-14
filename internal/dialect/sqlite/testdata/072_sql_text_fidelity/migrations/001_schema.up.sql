CREATE TABLE customers (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  company_name TEXT NOT NULL
);

CREATE TABLE quotes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  description TEXT NOT NULL,
  category TEXT NOT NULL,
  price INTEGER NOT NULL,
  quote_date TIMESTAMP NOT NULL,
  customer_id INTEGER NOT NULL REFERENCES customers (id),
  deleted_at TIMESTAMP
);
