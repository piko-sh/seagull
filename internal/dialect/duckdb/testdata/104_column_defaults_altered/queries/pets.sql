-- sgl.query(name: AddPet, command: exec)
INSERT INTO pets (id, weight) VALUES ($1, $2);
