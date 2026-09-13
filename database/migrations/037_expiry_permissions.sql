INSERT INTO role_permissions(role_id,permission)
SELECT r.id,p.permission
FROM roles r
CROSS JOIN LATERAL (VALUES('expiry.view'),('expiry.edit'),('expiry.print'),('expiry.export')) p(permission)
WHERE r.key IN('super_admin','admin','stock_manager')
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions(role_id,permission)
SELECT r.id,p.permission
FROM roles r
CROSS JOIN LATERAL (VALUES('expiry.view'),('expiry.print'),('expiry.export')) p(permission)
WHERE r.key='manager'
ON CONFLICT DO NOTHING;
