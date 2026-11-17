-- sgl.query(name: AddAccount, command: exec)
INSERT INTO accounts (id, name) VALUES (?, ?);

-- sgl.query(name: AddOrder, command: exec)
INSERT INTO orders (total) VALUES (?);
