-- sgl.query(name: ListPartitionReadings, command: many)
SELECT id, taken_on, value, note FROM readings_2026 WHERE taken_on >= $1;

-- sgl.query(name: ListLaterReadings, command: many)
SELECT * FROM app.public.readings_2027;

-- sgl.query(name: ReadingNumbers, command: one)
SELECT last_value, log_cnt, is_called FROM reading_numbers;

-- sgl.query(name: InvoiceNumber, command: one)
SELECT n.last_value FROM billing.invoice_numbers AS n;

-- sgl.query(name: ListInvoices, command: many)
SELECT i.id, i.total, t.total AS view_total FROM billing.invoices AS i JOIN billing.invoice_totals AS t ON t.id = i.id;

-- sgl.query(name: ListPayments, command: many)
SELECT p.id, r.id AS refund_id FROM billing.payments AS p LEFT JOIN billing.refunds AS r ON r.payment_id = p.id;

-- sgl.query(name: ListNoticesAndAudits, command: many)
SELECT n.id, a.id AS audit_id FROM notices AS n JOIN audit_events AS a ON a.id = n.id;
