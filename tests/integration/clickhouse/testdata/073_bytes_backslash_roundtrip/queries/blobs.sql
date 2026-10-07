-- sgl.query(InsertBlob, exec)
INSERT INTO blobs (id, payload) VALUES ({id:UInt64}, {payload:String});

-- sgl.query(ListBlobs, many)
SELECT id, payload, length(payload) AS payload_length FROM blobs ORDER BY id;
