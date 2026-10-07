CREATE TABLE readings (
    id INT AUTO_INCREMENT PRIMARY KEY,
    plain FLOAT NOT NULL,
    single FLOAT(24) NOT NULL,
    widened FLOAT(25) NOT NULL,
    widest FLOAT(53) UNSIGNED NOT NULL,
    scaled FLOAT(30, 4) NOT NULL,
    four FLOAT4 NOT NULL,
    four_widened FLOAT4(30) NOT NULL,
    eight FLOAT8 NOT NULL
);
