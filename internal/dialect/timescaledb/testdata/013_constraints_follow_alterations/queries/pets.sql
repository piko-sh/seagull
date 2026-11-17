-- sgl.query(name: AddPet, command: exec)
INSERT INTO pets (id, owner, account, weight) VALUES ($1, $2, $3, $4);
