-- sgl.query(AddDish, exec)
INSERT INTO menu (id, dish, price_pence) VALUES ({id:UInt64}, {dish:String}, {price_pence:UInt32});

-- sgl.query(MenuWithService, many)
SELECT dish, price_pence, with_service(price_pence) AS total_pence
FROM menu
ORDER BY id;
