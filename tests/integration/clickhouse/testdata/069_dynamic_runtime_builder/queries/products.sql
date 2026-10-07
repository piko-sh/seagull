-- sgl.query(InsertProduct, exec)
INSERT INTO products (id, name, price, in_stock) VALUES ({id:UInt64}, {name:String}, {price:UInt32}, {in_stock:Bool});

-- sgl.query(name: SearchProducts, command: many, dynamic: runtime)
SELECT id, name, price, in_stock FROM products

-- sgl.query(name: SearchStocked, command: many, dynamic: runtime)
SELECT id, name, price, in_stock FROM products WHERE in_stock = {only_stocked:Bool}
