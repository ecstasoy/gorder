USE gorder;

-- Seed 1000 deterministic stock rows for EXPLAIN / index experiments.
-- This script is re-runnable: existing mock_prod_xxxx rows are skipped.
INSERT INTO o_stock (product_id, quantity, version, created_at, updated_at)
SELECT
    CONCAT('mock_prod_', LPAD(nums.n, 4, '0')) AS product_id,
    50 + MOD(nums.n * 17, 950) AS quantity,
    MOD(nums.n, 5) AS version,
    TIMESTAMP('2026-01-01 00:00:00') + INTERVAL MOD(nums.n, 90) DAY + INTERVAL MOD(nums.n * 7, 86400) SECOND AS created_at,
    TIMESTAMP('2026-03-01 00:00:00') + INTERVAL MOD(nums.n, 30) DAY + INTERVAL MOD(nums.n * 11, 86400) SECOND AS updated_at
FROM (
    SELECT ones.n + tens.n * 10 + hundreds.n * 100 + 1 AS n
    FROM (
        SELECT 0 AS n UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4
        UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9
    ) AS ones
    CROSS JOIN (
        SELECT 0 AS n UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4
        UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9
    ) AS tens
    CROSS JOIN (
        SELECT 0 AS n UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4
        UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9
    ) AS hundreds
) AS nums
LEFT JOIN o_stock existing
    ON existing.product_id = CONCAT('mock_prod_', LPAD(nums.n, 4, '0'))
WHERE nums.n <= 1000
  AND existing.id IS NULL;

SELECT COUNT(*) AS stock_rows FROM o_stock;
