-- sgl.query(AddBlob, exec)
INSERT INTO blobs (id, tags, nums, maybe) VALUES ({id:Int64}, {tags:Array(String)}, {nums:Array(Int64)}, {maybe:Nullable(String)});

-- sgl.query(AddBlobs, batch)
INSERT INTO blobs (id, tags, nums, maybe) VALUES ({id:Int64}, {tags:Array(String)}, {nums:Array(Int64)}, {maybe:Nullable(String)});

-- sgl.query(CopyBlobs, copyfrom)
INSERT INTO blobs (id, tags, nums, maybe) VALUES ({id:Int64}, {tags:Array(String)}, {nums:Array(Int64)}, {maybe:Nullable(String)});

-- sgl.query(ListBlobs, many)
SELECT id, length(tags) AS tag_count, length(nums) AS num_count, isNull(maybe) AS maybe_missing FROM blobs ORDER BY id;
