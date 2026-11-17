-- sgl.query(name: AsSelect, command: many)
SELECT k AS `select` FROM a;

-- sgl.query(name: AsReserved, command: many)
SELECT k AS interval FROM a;

-- sgl.query(name: BareReserved, command: many)
SELECT k rank FROM a;

-- sgl.query(name: AsUnreserved, command: many)
SELECT k AS status FROM a;
