CREATE TABLE stock (
    id INT PRIMARY KEY,
    qty INT CHECK (qty > 0),
    price INT NOT NULL CHECK (price > 0),
    amount INT,
    CONSTRAINT stock_qty_bound CHECK (qty < 100),
    CHECK (price < 1000)
);
ALTER TABLE stock ADD COLUMN spare INT CHECK (spare > 0);
ALTER TABLE stock ADD COLUMN width INT CHECK (width > 0) AFTER id;
ALTER TABLE stock DROP COLUMN spare;
ALTER TABLE stock RENAME COLUMN qty TO quantity;
ALTER TABLE stock MODIFY price INT NOT NULL;
ALTER TABLE stock CHANGE amount total INT CHECK (total > 0);
