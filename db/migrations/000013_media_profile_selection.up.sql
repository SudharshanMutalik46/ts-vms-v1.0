-- 1. Camera Media Profiles (Normalized)
CREATE TABLE IF NOT EXISTS camera_media_profiles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    camera_id UUID NOT NULL, -- Logical link to inventory (Phase 2.1)
    
    profile_token TEXT NOT NULL,
    profile_name TEXT,
    
    video_codec TEXT NOT NULL, -- H264, H265, MJPEG, UNKNOWN
    width INT NOT NULL,
    height INT NOT NULL,
    fps DECIMAL, 
    bitrate_kbps INT,
    
    rtsp_url_sanitized TEXT NOT NULL, -- Credentials STRIPPED
    
    discovered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    -- Uniqueness per camera+token
    CONSTRAINT uq_camera_media_profile UNIQUE (tenant_id, camera_id, profile_token)
);

CREATE INDEX idx_media_profiles_camera ON camera_media_profiles(tenant_id, camera_id);

-- 2. Camera Stream Selections (Main/Sub)
CREATE TABLE IF NOT EXISTS camera_stream_selections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    camera_id UUID NOT NULL,
    
    main_profile_token TEXT,
    main_rtsp_url_sanitized TEXT,
    main_supported BOOLEAN DEFAULT FALSE,
    
    sub_profile_token TEXT,
    sub_rtsp_url_sanitized TEXT,
    sub_supported BOOLEAN DEFAULT FALSE,
    sub_is_same_as_main BOOLEAN DEFAULT FALSE,
    
    selection_version INT DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    CONSTRAINT uq_stream_selection UNIQUE (tenant_id, camera_id)
);

-- 3. RTSP Validation Results
CREATE TABLE IF NOT EXISTS rtsp_validation_results (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    camera_id UUID NOT NULL,
    variant TEXT NOT NULL, -- 'main', 'sub'
    
    status TEXT NOT NULL, -- valid, invalid, unauthorized, missing_credentials, timeout, rtsp_uri_missing, unsupported_codec, error
    last_error_code TEXT,
    rtt_ms INT,
    attempt_count INT DEFAULT 0,
    
    validated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    CONSTRAINT uq_validation_result UNIQUE (tenant_id, camera_id, variant),
    CONSTRAINT chk_variant CHECK (variant IN ('main', 'sub'))
);

-- RLS
ALTER TABLE camera_media_profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE camera_stream_selections ENABLE ROW LEVEL SECURITY;
ALTER TABLE rtsp_validation_results ENABLE ROW LEVEL SECURITY;

CREATE POLICY media_profiles_isolation ON camera_media_profiles
    USING (tenant_id = current_setting('app.current_tenant')::uuid);

CREATE POLICY stream_selections_isolation ON camera_stream_selections
    USING (tenant_id = current_setting('app.current_tenant')::uuid);

CREATE POLICY validation_results_isolation ON rtsp_validation_results
    USING (tenant_id = current_setting('app.current_tenant')::uuid);
