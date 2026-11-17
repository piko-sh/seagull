-- sgl.query(name: AsNothing, command: many)
SELECT k AS nothing FROM a;

-- sgl.query(name: AsNull, command: many)
SELECT k AS null FROM a;

-- sgl.query(name: BareLeft, command: many)
SELECT k left FROM a;

-- sgl.query(name: AsLeft, command: many)
SELECT k AS left FROM a;

-- sgl.query(name: QuotedNothing, command: many)
SELECT k AS "nothing" FROM a;
