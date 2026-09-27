-- Guest ownership and the unique preview row already make promotion
-- single-use for each anonymous session. Account quota, not a lifetime
-- entitlement, decides whether that recording can enter an existing account.
DROP TABLE IF EXISTS guest_preview_entitlements;
