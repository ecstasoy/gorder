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

-- ADR-0001 Step 4: stock reservation ledger. order_id is the idempotency
-- key; status walks held → confirmed (实扣) or held → released (还回).
-- One row per (order_id, product_id); a single order over multiple items
-- has multiple rows that share order_id and walk the state machine together.
DROP TABLE IF EXISTS `o_stock_reservation`;

CREATE TABLE `o_stock_reservation` (
    id          INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    order_id    VARCHAR(64)  NOT NULL,
    product_id  VARCHAR(255) NOT NULL,
    quantity    INT UNSIGNED NOT NULL,
    status      ENUM('held','confirmed','released') NOT NULL DEFAULT 'held',
    created_at  TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP    DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY  uk_order_product (order_id, product_id),
    KEY         idx_status_updated (status, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ADR-0004 Flash Sale Activity:把 "活动" 从隐式(Redis TTL)升级为一级 entity。
-- 同一 SKU 可以承载多场活动(早场 / 晚场 / A/B 测试),通过 activity_id 区分。
-- Order 上记录 activity_id,审计 / 退款 / 报表都可以按活动维度拉取。
-- 状态机:draft → scheduled → active → ended / cancelled
DROP TABLE IF EXISTS `flash_activities`;

CREATE TABLE `flash_activities` (
    id            VARCHAR(64)  PRIMARY KEY COMMENT 'uuid',
    name          VARCHAR(255) NOT NULL,
    product_id    VARCHAR(255) NOT NULL COMMENT '对应 o_stock.product_id',
    total_stock   INT UNSIGNED NOT NULL,
    start_time    TIMESTAMP    NOT NULL,
    end_time      TIMESTAMP    NOT NULL,
    status        ENUM('draft','scheduled','active','ended','cancelled') NOT NULL DEFAULT 'draft',
    warmup_done   BOOLEAN      NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMP    DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    KEY idx_product_status (product_id, status),
    KEY idx_status_start (status, start_time)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;