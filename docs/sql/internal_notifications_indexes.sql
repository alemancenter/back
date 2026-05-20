CREATE INDEX idx_notifications_user_unread_created
ON notifications (notifiable_type, notifiable_id, read_at, created_at);

CREATE INDEX idx_permissions_name
ON permissions (name);

CREATE INDEX idx_model_has_permissions_model
ON model_has_permissions (model_type, model_id, permission_id);

CREATE INDEX idx_model_has_roles_model
ON model_has_roles (model_type, model_id, role_id);

CREATE INDEX idx_role_has_permissions_role_permission
ON role_has_permissions (role_id, permission_id);
