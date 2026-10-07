-- sgl.column(hosts.addr, go_type: "net.IP")
-- sgl.column(hosts.label, go_type: "html/template.HTML")
-- sgl.column(hosts.amount, go_type: "encoding/json.Number")
CREATE TABLE hosts (
    id UInt64,
    addr IPv4,
    label String,
    amount Decimal(10, 2)
) ENGINE = MergeTree ORDER BY id;
