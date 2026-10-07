--   seagull.query( GetCustomer ,one )
SELECT id, name, email FROM customers WHERE id = ?;

-- seagull.query(ListCustomers,many)
SELECT id, name, email FROM customers ORDER BY name;
