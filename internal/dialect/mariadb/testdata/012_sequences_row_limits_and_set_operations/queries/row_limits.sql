-- sgl.query(name: InvoicePage, command: many)
SELECT id, shop FROM invoices ORDER BY id OFFSET ? ROWS FETCH NEXT ? ROWS ONLY;

-- sgl.query(name: TopInvoices, command: many)
SELECT id, amount FROM invoices ORDER BY amount DESC FETCH FIRST 3 ROWS WITH TIES;
