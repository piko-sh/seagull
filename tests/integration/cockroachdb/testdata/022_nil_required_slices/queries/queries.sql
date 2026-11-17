-- sgl.query(AddBlob, exec)
INSERT INTO blobs (id, data, tags, nums, maybe) VALUES ($1, $2, $3, $4, $5);

-- sgl.query(AddBlobs, batch)
INSERT INTO blobs (id, data, tags, nums, maybe) VALUES ($1, $2, $3, $4, $5);

-- sgl.query(CopyBlobs, copyfrom)
INSERT INTO blobs (id, data, tags, nums, maybe) VALUES ($1, $2, $3, $4, $5);

-- sgl.query(ListBlobs, many)
SELECT id, octet_length(data) AS data_length, COALESCE(array_length(tags, 1), 0) AS tag_count, COALESCE(array_length(nums, 1), 0) AS num_count, maybe IS NULL AS maybe_missing FROM blobs ORDER BY id;
