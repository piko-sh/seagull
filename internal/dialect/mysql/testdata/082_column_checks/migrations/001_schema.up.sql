CREATE TABLE stock (
    id INT PRIMARY KEY,
    qty INT CHECK (qty > 0),
    price INT NOT NULL CHECK (price > 0),
    amount INT,
    CONSTRAINT stock_id_bound CHECK (id < 100),
    CHECK (price < 1000)
);
ALTER TABLE stock ADD COLUMN spare INT CHECK (spare > 0);
ALTER TABLE stock ADD COLUMN width INT CHECK (width > 0);
ALTER TABLE stock DROP COLUMN spare;
ALTER TABLE stock MODIFY price INT NOT NULL;
ALTER TABLE stock DROP COLUMN qty;
ALTER TABLE stock CHANGE amount total INT CHECK (total > 0);
