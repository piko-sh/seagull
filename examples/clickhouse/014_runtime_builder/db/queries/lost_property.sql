-- sgl.query(HandIn, exec)
INSERT INTO lost_property (id, item, colour, found_at, days_held)
VALUES ({id:UInt64}, {item:String}, {colour:String}, {found_at:String}, {days_held:UInt32});

-- sgl.query(Claim, asyncexec)
ALTER TABLE lost_property
UPDATE claimed = true, owner_phone = {owner_phone:Nullable(String)}
WHERE id = {id:UInt64};

-- sgl.query(SearchUnclaimed, many, dynamic: runtime)
SELECT id, item, colour, found_at, days_held
FROM lost_property
WHERE claimed = false
