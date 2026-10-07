CREATE TABLE ledgers (
    id integer PRIMARY KEY,
    ident uuid NOT NULL,
    price money NOT NULL,
    refund money,
    opens timetz NOT NULL,
    closes timetz,
    flags bit(3) NOT NULL,
    mask varbit,
    span interval NOT NULL,
    moments timestamp[] NOT NULL,
    days date[],
    amounts numeric[] NOT NULL,
    blobs bytea[],
    rates numeric[] NOT NULL
);
