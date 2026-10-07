-- sgl.column(hosts.addr, go_type: "net/netip.Addr")
-- sgl.column(hosts.label, go_type: "html/template.HTML")
-- sgl.column(hosts.code, go_type: "html/template.URL")
CREATE TABLE hosts (
    id integer PRIMARY KEY,
    addr inet NOT NULL,
    label text NOT NULL,
    code varchar(8) NOT NULL
);
