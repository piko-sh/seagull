-- sgl.query(IssueLicence, exec)
INSERT INTO licences (id, boat, expires) VALUES ({id:UInt64}, {boat:String}, {expires:Date});

-- sgl.query(RenewLicence, asyncexec)
ALTER TABLE licences
UPDATE expires = addYears(expires, 1), renewals = renewals + 1
WHERE id = {id:UInt64};

-- sgl.query(PurgeExpired, asyncexec)
ALTER TABLE licences
DELETE WHERE expires < {today:Date}
SETTINGS mutations_sync = 2;

-- sgl.query(CancelLicence, exec)
DELETE FROM licences WHERE id = {id:UInt64};

-- sgl.query(ListLicences, many)
SELECT id, boat, expires, renewals FROM licences ORDER BY id;
