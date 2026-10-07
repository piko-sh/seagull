-- sgl.query(InsertPlant, exec)
INSERT INTO plants (id, name, height) VALUES (?, ?, ?);

-- sgl.query(ListPlants, many)
SELECT * FROM plant_list ORDER BY id;

-- sgl.query(ListPlantNames, many)
SELECT * FROM plant_names ORDER BY id;

-- sgl.query(ListPlantStack, many)
SELECT * FROM plant_stack ORDER BY id;
