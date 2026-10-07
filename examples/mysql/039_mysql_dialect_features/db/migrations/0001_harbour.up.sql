CREATE TABLE boats (
    registration CHAR(6)                                     NOT NULL PRIMARY KEY,
    name         VARCHAR(50)                                 NOT NULL,
    kit          SET('radio', 'radar', 'liferaft', 'winch')  NOT NULL DEFAULT '' COMMENT 'The equipment on board, any of the SET''s values.',
    crossings    INT UNSIGNED                                NOT NULL DEFAULT 0 COMMENT 'How many times the boat has left the harbour.'
) COMMENT = 'Boats on the harbour register, one per registration.';

CREATE TABLE crew (
    id           BIGINT      NOT NULL AUTO_INCREMENT PRIMARY KEY,
    registration CHAR(6)     NOT NULL,
    name         VARCHAR(50) NOT NULL,
    `rank`       VARCHAR(20) NOT NULL,
    FOREIGN KEY (registration) REFERENCES boats (registration)
);

CREATE TABLE berths (
    berth        TINYINT UNSIGNED NOT NULL PRIMARY KEY,
    registration CHAR(6)          NOT NULL UNIQUE
);
