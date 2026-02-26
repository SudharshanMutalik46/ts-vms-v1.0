CREATE TABLE IF NOT EXISTS recording_schedules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id VARCHAR(255) NOT NULL,
    site_id VARCHAR(255) NOT NULL,
    camera_id VARCHAR(255) NOT NULL,
    schedule_type VARCHAR(50) NOT NULL, -- '24x7', 'time_window', 'event_triggered'
    days JSONB,
    start_time VARCHAR(10), -- 'HH:MM'
    end_time VARCHAR(10),   -- 'HH:MM'
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(camera_id) -- One active schedule per camera for simplicity
);
