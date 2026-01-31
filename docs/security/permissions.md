# Permission Model

## 1. Format
Permissions use the format: `resource.action`.

### Common Permissions
- `camera.create`, `camera.read`, `camera.update`, `camera.delete`
- `stream.view_live`, `stream.view_hls`
- `recording.view`, `recording.export`
- `ptz.move`, `ptz.lock`
- `user.invite`, `user.disable`

## 2. Evaluation Logic
The authorization engine follows **Default Deny**.

```python
def check_permission(user, target_resource, permission):
    # 1. Check Tenant Isolation
    if user.tenant_id != target_resource.tenant_id:
        return REJECT ("Not Found") 

    # 2. Gather all roles applicable to this resource scope
    # (Checking Tenant bindings, Site bindings)
    active_roles = get_roles_for_scope(user, target_resource)
    
    # 3. Check if ANY role contains the permission
    for role in active_roles:
         if permission in role.permissions:
             return ALLOW

    return REJECT ("Forbidden")
```

## 3. Audit Hooks
Every `REJECT` decision on a mutating action (POST/PUT/DELETE) implies a potential security incident or misconfiguration and MUST be logged.
