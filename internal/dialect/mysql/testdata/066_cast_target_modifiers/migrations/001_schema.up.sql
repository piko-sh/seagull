-- sgl.column(prices.amount, go_type: "github.com/shopspring/decimal.Decimal")
CREATE TABLE prices (
    id INT AUTO_INCREMENT PRIMARY KEY,
    amount DECIMAL(10, 2) NOT NULL,
    label VARCHAR(40) NOT NULL,
    listed_at DATETIME(3) NOT NULL,
    details JSON NOT NULL
);
