-- sgl.query(name: NextInvoiceNumbers, command: one)
SELECT NEXT VALUE FOR invoice_numbers AS next_number, NEXTVAL(invoice_numbers) AS after_that,
    PREVIOUS VALUE FOR invoice_numbers AS previous_number, LASTVAL(invoice_numbers) AS last_number;

-- sgl.query(name: MoveInvoiceNumbers, command: one)
SELECT SETVAL(invoice_numbers, 5000) AS moved_to, SETVAL(invoice_numbers, 6000, 0, 1) AS moved_unused;

-- sgl.query(name: AddInvoice, command: exec)
INSERT INTO invoices (id, shop, amount, items, issued) VALUES (NEXT VALUE FOR invoice_numbers, ?, ?, ?, ?);
