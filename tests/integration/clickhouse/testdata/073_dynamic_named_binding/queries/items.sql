-- sgl.query(InsertItem, exec)
INSERT INTO items (id, name, category, price) VALUES ({id:UInt64}, {name:String}, {category:String}, {price:UInt32});

-- sgl.query(SearchItems, many)
-- {category:Nullable(String)} as sgl.param(category, optional: true)
SELECT id, name, price FROM items
WHERE ({category:Nullable(String)} IS NULL OR category = {category:Nullable(String)}) AND price >= {min_price:UInt32}
ORDER BY id;

-- sgl.query(ListItemsSorted, many)
-- sgl.sortable(order_by, [name, price])
SELECT id, name, price FROM items WHERE price >= {min_price:UInt32} LIMIT {page_size:UInt32} OFFSET {page_offset:UInt32};

-- sgl.query(name: ItemsInCategory, command: many, dynamic: runtime)
SELECT id, name, price FROM items WHERE category = {category:String}
