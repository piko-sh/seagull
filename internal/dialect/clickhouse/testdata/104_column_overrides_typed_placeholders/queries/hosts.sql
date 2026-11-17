-- sgl.query(name: CompareAddresses, command: many)
SELECT id FROM hosts WHERE addr = {addr:IPv4} OR {nullable_addr:Nullable(IPv4)} <> addr OR addr = {text:String};

-- sgl.query(name: ListAddresses, command: many)
SELECT id FROM hosts WHERE addr IN ({first:IPv4}, {second:IPv4}) OR addr IN {many:Array(IPv4)};

-- sgl.query(name: BoundAddresses, command: many)
SELECT id FROM hosts WHERE addr BETWEEN {low:IPv4} AND {high:IPv4};

-- sgl.query(name: MatchLabels, command: many)
SELECT id FROM hosts WHERE label = {label:String} OR label LIKE {pattern:String} OR lower(label) = {lowered:String};

-- sgl.query(name: CompareAmounts, command: many)
SELECT id FROM hosts WHERE amount = {amount:Decimal(10, 2)} OR amount > {wider:Decimal(12, 4)};

-- sgl.query(name: InsertHost, command: exec)
INSERT INTO hosts (id, addr, label, amount) VALUES ({id:UInt64}, {addr:IPv4}, concat({label:String}, ''), {amount:Decimal(10, 2)});

-- sgl.query(name: MoveHost, command: asyncexec)
ALTER TABLE hosts UPDATE addr = {addr:IPv4}, label = {label:String} WHERE id = {id:UInt64};

-- sgl.query(name: RelabelHost, command: exec)
UPDATE hosts SET label = {label:String} WHERE addr = {addr:IPv4};
