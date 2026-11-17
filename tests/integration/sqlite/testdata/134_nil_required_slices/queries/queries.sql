-- sgl.query(AddBlob, exec)
INSERT INTO blobs (id, data, maybe) VALUES (?, ?, ?);

-- sgl.query(AddBlobs, batch)
INSERT INTO blobs (id, data, maybe) VALUES (?, ?, ?);

-- sgl.query(CopyBlobs, copyfrom)
INSERT INTO blobs (id, data, maybe) VALUES (?, ?, ?);

-- sgl.query(ListBlobs, many)
SELECT id, length(data) AS data_length, maybe IS NULL AS maybe_missing FROM blobs ORDER BY id;
