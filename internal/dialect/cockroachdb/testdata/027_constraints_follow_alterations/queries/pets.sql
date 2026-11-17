-- sgl.query(name: AddPet, command: exec)
INSERT INTO pets (id, tag, account, weight) VALUES ($1, $2, $3, $4);
