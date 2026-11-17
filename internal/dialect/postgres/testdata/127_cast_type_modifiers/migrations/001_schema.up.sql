CREATE TABLE prices (
    id integer PRIMARY KEY,
    amount numeric(10, 2) NOT NULL,
    hundreds numeric(5, -2) NOT NULL,
    sku varchar(40) NOT NULL,
    seen timestamp(3) with time zone NOT NULL,
    opened time(0) without time zone NOT NULL
);
