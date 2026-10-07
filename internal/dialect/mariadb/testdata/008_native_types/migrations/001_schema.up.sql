CREATE TABLE devices (
    id INT PRIMARY KEY,
    public_id UUID NOT NULL DEFAULT UUID(),
    owner UUID NULL,
    last_ip4 INET4 NULL,
    last_ip6 INET6 NOT NULL,
    embedding VECTOR(3) NOT NULL,
    VECTOR INDEX (embedding)
);
