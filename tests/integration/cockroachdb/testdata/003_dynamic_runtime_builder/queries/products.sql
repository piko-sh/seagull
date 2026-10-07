-- sgl.query(name: InsertProduct, command: exec)
INSERT INTO products (name, price, in_stock) VALUES ($1, $2, $3);

-- sgl.query(name: SearchProducts, command: many, dynamic: runtime)
SELECT id, name, price, in_stock FROM products

-- sgl.query(name: SearchStocked, command: many, dynamic: runtime)
SELECT id, name, price, in_stock FROM products WHERE in_stock = $1
