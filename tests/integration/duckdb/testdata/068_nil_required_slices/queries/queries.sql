-- sgl.query(AddBlob, exec)
INSERT INTO blobs (id, data, tags, nums, maybe) VALUES ($1, $2, $3, $4, $5);

-- sgl.query(AddBlobs, batch)
INSERT INTO blobs (id, data, tags, nums, maybe) VALUES ($1, $2, $3, $4, $5);

-- sgl.query(CopyBlobs, copyfrom)
INSERT INTO blobs (id, data, tags, nums, maybe) VALUES ($1, $2, $3, $4, $5);

-- sgl.query(ListBlobs, many)
SELECT id, octet_length(data) AS data_length, len(tags) AS tag_count, len(nums) AS num_count, maybe IS NULL AS maybe_missing FROM blobs ORDER BY id;
