-- sgl.query(InsertEntity, exec)
INSERT INTO entities (id, label) VALUES ({id:UInt64}, {label:String});

-- sgl.query(UpdateLabel, exec)
ALTER TABLE entities UPDATE label = {new_label:String} WHERE id = {id:UInt64};

-- sgl.query(All, many)
SELECT id, label FROM entities ORDER BY id;
