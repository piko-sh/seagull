-- sgl.query(name: AsReturning, command: many)
SELECT k AS returning FROM a;

-- sgl.query(name: AsOffset, command: many)
SELECT k AS offset FROM a;

-- sgl.query(name: AsRank, command: many)
SELECT k AS rank FROM a;

-- sgl.query(name: Quoted, command: many)
SELECT k AS `returning` FROM a;
