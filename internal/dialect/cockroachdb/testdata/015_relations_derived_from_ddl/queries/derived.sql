-- sgl.query(name: ListNotes, command: many)
SELECT rowid, body, kind FROM notes;

-- sgl.query(name: ListMarks, command: many)
SELECT rowid_1, rowid, label FROM marks;

-- sgl.query(name: ListTags, command: many)
SELECT * FROM tags;

-- sgl.query(name: ListLabels, command: many)
SELECT * FROM labels;

-- sgl.query(name: AccountNumber, command: one)
SELECT last_value, log_cnt, is_called FROM account_numbers;

-- sgl.query(name: ListInvoices, command: many)
SELECT i.id, a.email, a.nickname FROM billing.invoices AS i JOIN app.public.accounts AS a ON a.id = i.account_id;
