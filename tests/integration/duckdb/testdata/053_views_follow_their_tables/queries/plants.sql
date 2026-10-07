-- sgl.query(InsertPlant, exec)
INSERT INTO plants (id, name, stock, height) VALUES ($1, $2, $3, $4);

-- sgl.query(ListPlants, many)
SELECT * FROM plant_list ORDER BY id;

-- sgl.query(ListPlantNames, many)
SELECT * FROM plant_names ORDER BY plant_id;

-- sgl.query(ListRenamedPlants, many)
SELECT p.code, p.label, p.stock FROM plants AS p (code, label) ORDER BY p.code;
