-- sgl.query(name: LastInvoiceNumber, command: one)
SELECT PREVIOUS VALUE FOR invoice_numbers AS previous_number;

-- sgl.query(name: NextInvoiceNumbers, command: one)
SELECT NEXT VALUE FOR invoice_numbers AS next_number, NEXTVAL(invoice_numbers) AS after_that,
    LASTVAL(invoice_numbers) AS last_number;

-- sgl.query(name: MoveInvoiceNumbers, command: one)
SELECT SETVAL(invoice_numbers, 2000) AS moved_to;

-- sgl.query(name: MoveInvoiceNumbersBack, command: one)
SELECT SETVAL(invoice_numbers, 10) AS moved_to;

-- sgl.query(name: AddInvoice, command: exec)
INSERT INTO invoices (id, shop, items, issued) VALUES (NEXT VALUE FOR invoice_numbers, ?, ?, ?);

-- sgl.query(name: InvoiceIDs, command: many)
SELECT id FROM invoices ORDER BY id;
