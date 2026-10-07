-- sgl.query(name: GetOrderCount, command: one)
SELECT order_count() AS total_orders;

-- sgl.query(name: GetLatestCustomer, command: one)
SELECT latest_customer() AS customer;
