# Split-Brain Prevention

## 1. Definition
Split-brain occurs when parts of the system lose connectivity and independently decide they are the "Master," leading to data corruption.

## 2. Configuration Authority (Single Node)
- **Principle:** The `vms-control` service running on the authorized Server is the **Single Source of Truth**.
- **Locking:** It holds an exclusive file-system lock (pidfile) on the Config DB directory.
- **Enforcement:** If a second instance starts, it fails to acquire lock and shuts down immediately.

## 3. Recording Authority
- **Disk Allocation:** Only ONE Recorder service is allowed to write to a specific Volume/Path at a time.
- **Overlap:** If multiple recorders write to `D:\Recordings`, index corruption occurs.
- **Prevention:** Recorder checks for ownership marker on disk mount.

## 4. High Availability (Future Scope)
If Multi-Node HA is enabled in future phases:
- **Consensus:** We will use Raft (via `etcd` or embedded) to elect a leader.
- **Rules:** No Leader = Read Only Mode.
- **Quorum:** Minimum 3 nodes required to tolerate 1 failure.
