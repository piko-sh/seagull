-- sgl.query(name: ListItems, command: many)
SELECT * FROM items;

-- sgl.query(name: ListCopies, command: many)
SELECT id, label, price FROM item_copies;

-- sgl.query(name: ListTotals, command: many)
SELECT id, price FROM item_totals;
