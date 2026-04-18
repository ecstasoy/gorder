CREATE DATABASE IF NOT EXISTS gorder;
  USE gorder;

DROP TABLE IF EXISTS `o_stock`;

CREATE TABLE `o_stock` (
                           id         INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
                           product_id VARCHAR(255) NOT NULL,
                           quantity   INT UNSIGNED NOT NULL DEFAULT 0,
                           version    INT NOT NULL DEFAULT 0,
                           created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                           updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
                           UNIQUE KEY uk_product_id (product_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Regular SKUs
INSERT INTO o_stock (product_id, quantity, version)
VALUES ('prod_U9k6VcIEwQb83T', 1000, 0),
       ('prod_U9k6gLFpAGaHQl', 500, 0);

-- Flash SKUs (independent product_id, separate Stripe products)
INSERT INTO o_stock (product_id, quantity, version)
VALUES ('prod_UL9Jg69oRkUThn', 0, 0),
       ('prod_UL9KdWV8DIH7dD', 0, 0);