# API Conventions

## 1. Error Model

### 1.1 HTTP Status Codes
| Code | Meaning |
| :--- | :--- |
| **200** | Success (Synchronous) |
| **201** | Created (Resource created) |
| **202** | Accepted (Async job started) |
| **400** | Bad Request (Validation failed) |
| **401** | Unauthorized (Missing/Bad Token) |
| **403** | Forbidden (RBAC) |
| **404** | Not Found |
| **409** | Conflict (Duplicate ID) |
| **500** | Internal Server Error |

### 1.2 JSON Error Body
```json
{
  "code": 400,
  "message": "Invalid IP address format",
  "request_id": "req_12345"
}
```

## 2. Pagination
We use **Cursor-based Pagination** for stability.

**Request:**
- `page_size`: (int, default=20, max=100)
- `page_token`: (string, opaque cursor)

**Response:**
- `items`: [...]
- `next_page_token`: (string, present if more pages exist)

## 3. Time Format
All timestamps must be **ISO 8601** (RFC 3339) in **UTC** with the 'Z' suffix.
- Example: `2026-02-01T14:30:00Z`
- Do NOT use local time or Unix timestamp integers in the public API.

## 4. ID Format
- All Public IDs (Camera ID, User ID) must be **UUID v4**.
- Internal short-lived IDs (pipeline sockets) can be strings.
