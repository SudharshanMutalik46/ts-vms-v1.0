# RBAC Structure

## 1. Hierarchy
The system enforces a strict 4-tier hierarchy. Data never leaks across tenants.

```mermaid
graph TD
    Tenant[Tenant (Org)] --> Site[Site (Location)]
    Site --> Camera[Camera]
```

## 2. Roles
Roles are collections of permissions bound to a user at a specific **Scope**.

| Role | Scope Level | Description |
| :--- | :--- | :--- |
| **Org Admin** | Tenant | Full control over users, billing, all sites. |
| **Site Admin** | Site | Manage cameras and users within one site. |
| **Operator** | Site (filtered) | View live/recorded, ack alarms. No config. |
| **Viewer** | Site/Camera | Read-only live view. |

## 3. Role Binding
A User can have multiple bindings:
- `User_A` is `Org Admin` of `Tenant_1`.
- `User_B` is `Site Admin` of `Site_X` (in `Tenant_1`).

## 4. Inheritance
- Permissions granted at **Tenant** level apply to ALL **Sites** and **Cameras**.
- Permissions granted at **Site** level apply to ALL **Cameras** in that site.
- **Camera Groups:** These are logical tags (e.g., "Perimeter"). An `Operator` role can be bound to a Site *with a filter* for `tag=Perimeter`.

## 5. Invariants
- **Multi-Tenant Isolation:** A user bound to `Tenant A` MUST returns 404/403 for any resource in `Tenant B`, regardless of ID guessing.
