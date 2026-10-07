-- sgl.query(name: GetDoubled, command: one)
SELECT pure_double(quantity) AS doubled FROM items WHERE id = ?;

-- sgl.query(name: GetItemCount, command: one)
SELECT get_item_count() AS total;
