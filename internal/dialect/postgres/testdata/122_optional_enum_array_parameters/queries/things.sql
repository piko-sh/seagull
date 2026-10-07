-- sgl.query(name: FilterThings, command: many)
-- :colours as sgl.param(optional: true)
SELECT id FROM things WHERE (:colours::colour[] IS NULL OR colours && :colours::colour[]) ORDER BY id;

-- sgl.query(name: FilterByFavourite, command: many)
-- :wanted as sgl.param(optional: true)
SELECT id FROM things WHERE (:wanted::colour[] IS NULL OR favourite = ANY(:wanted::colour[])) ORDER BY id;
