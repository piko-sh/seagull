-- sgl.query(ListParcelsByStatus, many)
-- ?1 as sgl.param(statuses, kind: slice)
SELECT id, recipient, status FROM parcels WHERE status IN (?) ORDER BY id;

-- sgl.query(SearchParcels, many, dynamic: runtime)
SELECT id, recipient, status, weight_grams FROM parcels
