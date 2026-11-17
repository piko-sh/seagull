-- sgl.query(name: InvoicePage, command: many)
SELECT id, shop FROM invoices ORDER BY id OFFSET ? ROWS FETCH NEXT ? ROWS ONLY;

-- sgl.query(name: MostItems, command: many)
SELECT id, items FROM invoices ORDER BY items DESC FETCH FIRST 1 ROWS WITH TIES;
